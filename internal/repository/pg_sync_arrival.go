package repository

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/warmbly/warmbly/internal/app/cipher"
	"github.com/warmbly/warmbly/internal/infrastructure/db"
	"github.com/warmbly/warmbly/internal/models"
)

func NewDurableEmailMessageMapRepository(d *db.DB, c cipher.CipherService) *pgEmailMessageMapRepository {
	return &pgEmailMessageMapRepository{db: d, cipher: c}
}

func validateArrival(data EmailMessageData, pending *PendingArrival) (uuid.UUID, uuid.UUID, uuid.UUID, error) {
	user, uerr := uuid.Parse(data.UserID)
	email, eerr := uuid.Parse(data.EmailID)
	id, ierr := uuid.Parse(data.ID)
	if uerr != nil || eerr != nil || ierr != nil || user == uuid.Nil || email == uuid.Nil || id == uuid.Nil || data.MessageID == "" {
		return user, email, id, errors.New("invalid arrival mapping")
	}
	if pending == nil || pending.Arrival == nil || pending.Arrival.Message == nil ||
		pending.Arrival.UserID != user || pending.Arrival.Message.EmailID != email ||
		pending.Arrival.Message.ID != id || pending.Arrival.Message.ThreadID != data.ThreadID {
		return user, email, id, errors.New("arrival does not match mapping")
	}
	if pending.Bounce != nil && (pending.Bounce.UserID != user || pending.Bounce.EmailID != email) ||
		pending.Complaint != nil && (pending.Complaint.UserID != user || pending.Complaint.EmailID != email) {
		return user, email, id, errors.New("arrival report does not match mailbox")
	}
	return user, email, id, nil
}

func (r *pgEmailMessageMapRepository) AdmitArrival(ctx context.Context, data EmailMessageData, pending *PendingArrival) error {
	user, email, id, err := validateArrival(data, pending)
	if err != nil {
		return err
	}
	if r.cipher == nil {
		return ErrArrivalOutboxUnsupported
	}
	var org uuid.UUID
	err = r.db.QueryRow(ctx, `SELECT organization_id FROM email_accounts WHERE id=$1 AND user_id=$2`, email, user).Scan(&org)
	if err != nil {
		return fmt.Errorf("arrival mailbox ownership: %w", err)
	}
	sealed, err := r.cipher.Cipher(ctx, org)
	if err != nil {
		return err
	}
	raw, err := json.Marshal(pending)
	if err != nil {
		return err
	}
	payload, err := sealed.Encrypt(ctx, string(raw))
	if err != nil {
		return err
	}
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(context.Background())
	// Recheck and lock ownership after crypto, without nesting key-store I/O in the transaction.
	var owned uuid.UUID
	if err = tx.QueryRow(ctx, `SELECT id FROM email_accounts WHERE id=$1 AND user_id=$2 AND organization_id=$3 FOR SHARE`, email, user, org).Scan(&owned); err != nil {
		return fmt.Errorf("arrival mailbox ownership: %w", err)
	}
	// A retry after an ambiguous response adopts the first canonical identity.
	inserted, err := tx.Exec(ctx, `INSERT INTO email_message_map(user_id,email_id,message_id,id,thread_id)
		VALUES($1,$2,$3,$4,$5) ON CONFLICT DO NOTHING`, user, email, data.MessageID, id, data.ThreadID)
	if err != nil {
		return err
	}
	if inserted.RowsAffected() != 0 {
		_, err = tx.Exec(ctx, `INSERT INTO sync_arrival_outbox(user_id,email_id,organization_id,message_id,id,payload)
			VALUES($1,$2,$3,$4,$5,$6)`, user, email, org, data.MessageID, id, payload)
		if err != nil {
			return err
		}
	} else {
		var pending bool
		err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM email_message_map m
			JOIN sync_arrival_outbox o USING(user_id,email_id,message_id,id)
			WHERE m.user_id=$1 AND m.email_id=$2 AND m.message_id=$3 AND o.organization_id=$4)`, user, email, data.MessageID, org).Scan(&pending)
		if err != nil {
			return err
		}
		// Legacy, deleted and already-delivered maps have no durable admission proof.
		if !pending {
			return ErrArrivalAdmissionUnconfirmed
		}
	}
	return tx.Commit(ctx)
}

func (r *pgEmailMessageMapRepository) ArrivalBacklog(ctx context.Context) (ArrivalBacklog, error) {
	var backlog ArrivalBacklog
	err := r.db.QueryRow(ctx, `SELECT count(*),min(created_at) FROM sync_arrival_outbox`).Scan(&backlog.Pending, &backlog.Oldest)
	return backlog, err
}

func (r *pgEmailMessageMapRepository) HasPendingArrival(ctx context.Context, user, email, id uuid.UUID) (bool, error) {
	var pending bool
	err := r.db.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM sync_arrival_outbox o
		JOIN email_accounts a ON a.id=o.email_id AND a.user_id=o.user_id AND a.organization_id=o.organization_id
		WHERE o.user_id=$1 AND o.email_id=$2 AND o.id=$3 AND o.stage=0)`, user, email, id).Scan(&pending)
	return pending, err
}

// DeliverArrivals invokes the existing consumer handlers, retaining every failed stage.
func (r *pgEmailMessageMapRepository) DeliverArrivals(ctx context.Context, deliver func(context.Context, models.JobEventType, any) error) error {
	var first error
	for i := 0; i < 32; i++ {
		found, err := r.deliverArrival(ctx, nil, deliver)
		if err != nil && first == nil {
			first = err
		}
		if !found || ctx.Err() != nil {
			break
		}
	}
	return first
}

type arrivalIdentity struct {
	user, email, id uuid.UUID
}

func (r *pgEmailMessageMapRepository) DeliverPendingArrival(ctx context.Context, user, email, id uuid.UUID, deliver func(context.Context, models.JobEventType, any) error) (bool, error) {
	if user == uuid.Nil || email == uuid.Nil || id == uuid.Nil {
		return false, errors.New("invalid pending arrival identity")
	}
	return r.deliverArrival(ctx, &arrivalIdentity{user: user, email: email, id: id}, deliver)
}

func (r *pgEmailMessageMapRepository) deliverArrival(ctx context.Context, target *arrivalIdentity, deliver func(context.Context, models.JobEventType, any) error) (bool, error) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	var user, email, org, lease uuid.UUID
	var key, payload string
	var stage int
	scope := ""
	var args []any
	if target != nil {
		scope = " AND o.user_id=$1 AND o.email_id=$2 AND o.id=$3"
		args = []any{target.user, target.email, target.id}
	}
	// Claim without retaining a connection while handlers use their repositories.
	err := r.db.QueryRow(ctx, `WITH candidate AS (SELECT o.user_id,o.email_id,o.message_id
		FROM sync_arrival_outbox o JOIN email_accounts a ON a.id=o.email_id AND a.user_id=o.user_id AND a.organization_id=o.organization_id
		WHERE o.retry_at<=now() AND (o.locked_until IS NULL OR o.locked_until<now())`+scope+`
		ORDER BY o.retry_at,o.created_at LIMIT 1 FOR UPDATE OF o SKIP LOCKED)
		UPDATE sync_arrival_outbox o SET lease=gen_random_uuid(),locked_until=now()+interval '1 minute'
		FROM candidate c WHERE o.user_id=c.user_id AND o.email_id=c.email_id AND o.message_id=c.message_id
		RETURNING o.user_id,o.email_id,o.organization_id,o.message_id,o.payload,o.stage,o.lease`, args...).Scan(&user, &email, &org, &key, &payload, &stage, &lease)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	sealed, err := r.cipher.Cipher(ctx, org)
	var raw string
	if err == nil {
		raw, err = sealed.Decrypt(ctx, payload)
	}
	var pending PendingArrival
	if err == nil {
		err = json.Unmarshal([]byte(raw), &pending)
	}
	if err == nil {
		events := []struct {
			kind    models.JobEventType
			body    any
			present bool
		}{
			{models.JobEventTypeNewEmail, pending.Arrival, true},
			{models.JobEventTypeInboundBounce, pending.Bounce, pending.Bounce != nil},
			{models.JobEventTypeInboundComplaint, pending.Complaint, pending.Complaint != nil},
		}
		for stage < len(events) {
			e := events[stage]
			if e.present {
				if err = deliver(ctx, e.kind, e.body); err != nil {
					break
				}
			}
			stage++
			if _, updateErr := r.db.Exec(ctx, `UPDATE sync_arrival_outbox SET stage=$5
				WHERE user_id=$1 AND email_id=$2 AND message_id=$3 AND lease=$4`, user, email, key, lease, stage); updateErr != nil {
				return true, updateErr
			}
		}
	}
	if err == nil {
		_, err = r.db.Exec(ctx, `DELETE FROM sync_arrival_outbox WHERE user_id=$1 AND email_id=$2 AND message_id=$3 AND lease=$4`, user, email, key, lease)
	} else {
		if _, updateErr := r.db.Exec(ctx, `UPDATE sync_arrival_outbox SET locked_until=NULL,retry_at=now()+interval '15 seconds'
			WHERE user_id=$1 AND email_id=$2 AND message_id=$3 AND lease=$4`, user, email, key, lease); updateErr != nil {
			return true, updateErr
		}
	}
	return true, err
}
