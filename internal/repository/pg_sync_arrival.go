package repository

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/warmbly/warmbly/internal/app/cipher"
	"github.com/warmbly/warmbly/internal/infrastructure/db"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/observability/arrivaldiag"
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
		return arrivalFailure("validation", err)
	}
	if r.cipher == nil {
		return ErrArrivalOutboxUnsupported
	}
	var org uuid.UUID
	err = r.db.QueryRow(ctx, `SELECT organization_id FROM email_accounts WHERE id=$1 AND user_id=$2`, email, user).Scan(&org)
	if err != nil {
		return arrivalOwnershipFailure("mailbox_ownership", err)
	}
	sealed, err := r.cipher.Cipher(ctx, org)
	if err != nil {
		return arrivalFailure("organization_key", err)
	}
	raw, err := json.Marshal(pending)
	if err != nil {
		return arrivalFailure("payload_encoding", err)
	}
	payload, err := sealed.Encrypt(ctx, string(raw))
	if err != nil {
		return arrivalFailure("payload_encryption", err)
	}
	// Recheck and lock ownership after crypto, and write the map and the outbox row as one statement.
	// A retry after an ambiguous response adopts the first canonical identity.
	var owned, queued bool
	err = r.db.QueryRow(ctx, `WITH owner AS (
			SELECT id FROM email_accounts WHERE id=$2 AND user_id=$1 AND organization_id=$3 FOR SHARE
		), mapped AS (
			INSERT INTO email_message_map(user_id,email_id,message_id,id,thread_id)
			SELECT $1::uuid,$2::uuid,$4::text,$5::uuid,$6::text FROM owner ON CONFLICT DO NOTHING RETURNING 1
		), queued AS (
			INSERT INTO sync_arrival_outbox(user_id,email_id,organization_id,message_id,id,payload)
			SELECT $1::uuid,$2::uuid,$3::uuid,$4::text,$5::uuid,$7::text FROM mapped RETURNING 1
		)
		SELECT EXISTS(SELECT 1 FROM owner), EXISTS(SELECT 1 FROM queued)`,
		user, email, org, data.MessageID, id, data.ThreadID, payload).Scan(&owned, &queued)
	if err != nil {
		return arrivalFailure("mapping_write", err)
	}
	if !owned {
		return arrivalOwnershipFailure("mailbox_ownership_recheck", pgx.ErrNoRows)
	}
	if queued {
		return nil
	}
	var durable bool
	err = r.db.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM email_message_map m
		JOIN sync_arrival_outbox o USING(user_id,email_id,message_id,id)
		WHERE m.user_id=$1 AND m.email_id=$2 AND m.message_id=$3 AND o.organization_id=$4)`, user, email, data.MessageID, org).Scan(&durable)
	if err != nil {
		return arrivalFailure("durability_check", err)
	}
	// Legacy, deleted and already-delivered maps have no durable admission proof.
	if !durable {
		return ErrArrivalAdmissionUnconfirmed
	}
	return nil
}

func arrivalOwnershipFailure(stage string, err error) error {
	if errors.Is(err, pgx.ErrNoRows) {
		err = errors.Join(ErrArrivalMailboxOwnershipLost, err)
	}
	return arrivalFailure(stage, err)
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

// ArrivalBatchSize is how many stored arrivals one claim leases, and the most one DeliverArrivals call delivers; the lease outlives the 30 second pass.
const ArrivalBatchSize = 32

// DeliverArrivals invokes the existing consumer handlers, retaining every failed stage.
func (r *pgEmailMessageMapRepository) DeliverArrivals(ctx context.Context, deliver func(context.Context, models.JobEventType, any) error) error {
	claimed, err := r.claimArrivals(ctx, nil, ArrivalBatchSize)
	if err != nil {
		return err
	}
	next := 0
	err = deliverArrivalBatch(ctx, func() (bool, error) {
		if next == len(claimed) {
			return false, nil
		}
		next++
		return true, r.deliverClaimed(ctx, claimed[next-1], deliver)
	})
	r.releaseArrivals(claimed[next:])
	return err
}

func deliverArrivalBatch(ctx context.Context, deliverOne func() (bool, error)) error {
	var first error
	for i := 0; i < ArrivalBatchSize; i++ {
		found, err := deliverOne()
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
	claimed, err := r.claimArrivals(ctx, &arrivalIdentity{user: user, email: email, id: id}, 1)
	if err != nil || len(claimed) == 0 {
		return false, err
	}
	return true, r.deliverClaimed(ctx, claimed[0], deliver)
}

type claimedArrival struct {
	user, email, org, lease uuid.UUID
	key, payload            string
	stage                   int
}

// claimArrivals leases up to limit due arrivals without holding a connection while handlers run.
func (r *pgEmailMessageMapRepository) claimArrivals(ctx context.Context, target *arrivalIdentity, limit int) ([]claimedArrival, error) {
	scope := ""
	args := []any{limit}
	if target != nil {
		scope = " AND o.user_id=$2 AND o.email_id=$3 AND o.id=$4"
		args = append(args, target.user, target.email, target.id)
	}
	finish := arrivaldiag.Begin(ctx, arrivaldiag.Claim)
	defer finish()
	rows, err := r.db.Query(ctx, `WITH candidate AS (SELECT o.user_id,o.email_id,o.message_id
		FROM sync_arrival_outbox o JOIN email_accounts a ON a.id=o.email_id AND a.user_id=o.user_id AND a.organization_id=o.organization_id
		WHERE o.retry_at<=now() AND (o.locked_until IS NULL OR o.locked_until<now())`+scope+`
		ORDER BY o.retry_at,o.created_at LIMIT $1 FOR UPDATE OF o SKIP LOCKED)
		UPDATE sync_arrival_outbox o SET lease=gen_random_uuid(),locked_until=now()+interval '1 minute'
		FROM candidate c WHERE o.user_id=c.user_id AND o.email_id=c.email_id AND o.message_id=c.message_id
		RETURNING o.user_id,o.email_id,o.organization_id,o.message_id,o.payload,o.stage,o.lease,o.retry_at,o.created_at`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	type ordered struct {
		claimedArrival
		retryAt, createdAt time.Time
	}
	var out []ordered
	for rows.Next() {
		var c ordered
		if err := rows.Scan(&c.user, &c.email, &c.org, &c.key, &c.payload, &c.stage, &c.lease, &c.retryAt, &c.createdAt); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	// RETURNING carries no order; deliver oldest first as the claim chose them.
	slices.SortFunc(out, func(a, b ordered) int {
		if c := a.retryAt.Compare(b.retryAt); c != 0 {
			return c
		}
		return a.createdAt.Compare(b.createdAt)
	})
	claimed := make([]claimedArrival, len(out))
	for i := range out {
		claimed[i] = out[i].claimedArrival
	}
	return claimed, nil
}

// releaseArrivals hands back leases a pass ran out of time to use.
func (r *pgEmailMessageMapRepository) releaseArrivals(claimed []claimedArrival) {
	if len(claimed) == 0 {
		return
	}
	users, emails, keys, leases := make([]uuid.UUID, len(claimed)), make([]uuid.UUID, len(claimed)), make([]string, len(claimed)), make([]uuid.UUID, len(claimed))
	for i, c := range claimed {
		users[i], emails[i], keys[i], leases[i] = c.user, c.email, c.key, c.lease
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, _ = r.db.Exec(ctx, `UPDATE sync_arrival_outbox o SET locked_until=NULL
		FROM unnest($1::uuid[],$2::uuid[],$3::text[],$4::uuid[]) AS c(user_id,email_id,message_id,lease)
		WHERE o.user_id=c.user_id AND o.email_id=c.email_id AND o.message_id=c.message_id AND o.lease=c.lease`, users, emails, keys, leases)
}

func (r *pgEmailMessageMapRepository) deliverClaimed(ctx context.Context, c claimedArrival, deliver func(context.Context, models.JobEventType, any) error) error {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	user, email, org, lease, key, payload, stage := c.user, c.email, c.org, c.lease, c.key, c.payload, c.stage
	finish := arrivaldiag.Begin(ctx, arrivaldiag.KeyLookup)
	sealed, err := r.cipher.Cipher(ctx, org)
	finish()
	var raw string
	if err == nil {
		finish = arrivaldiag.Begin(ctx, arrivaldiag.Decrypt)
		raw, err = sealed.Decrypt(ctx, payload)
		finish()
	}
	var pending PendingArrival
	if err == nil {
		finish = arrivaldiag.Begin(ctx, arrivaldiag.Decode)
		err = json.Unmarshal([]byte(raw), &pending)
		finish()
	}
	if err == nil {
		events := []struct {
			kind    models.JobEventType
			body    any
			present bool
			measure arrivaldiag.Stage
		}{
			{models.JobEventTypeNewEmail, pending.Arrival, true, arrivaldiag.NewEmail},
			{models.JobEventTypeInboundBounce, pending.Bounce, pending.Bounce != nil, arrivaldiag.Bounce},
			{models.JobEventTypeInboundComplaint, pending.Complaint, pending.Complaint != nil, arrivaldiag.Complaint},
		}
		last := 0
		for i, e := range events {
			if e.present {
				last = i
			}
		}
		for stage < len(events) {
			e := events[stage]
			if e.present {
				finish = arrivaldiag.Begin(ctx, e.measure)
				err = deliver(ctx, e.kind, e.body)
				finish()
				if err != nil {
					break
				}
			}
			stage++
			// Only a delivered stage with a later one still to run needs a checkpoint; the delete retires the rest.
			if !e.present || stage > last {
				continue
			}
			finish = arrivaldiag.Begin(ctx, arrivaldiag.Checkpoint)
			_, updateErr := r.db.Exec(ctx, `UPDATE sync_arrival_outbox SET stage=$5
				WHERE user_id=$1 AND email_id=$2 AND message_id=$3 AND lease=$4`, user, email, key, lease, stage)
			finish()
			if updateErr != nil {
				return updateErr
			}
		}
	}
	if err == nil {
		finish = arrivaldiag.Begin(ctx, arrivaldiag.Retire)
		_, err = r.db.Exec(ctx, `DELETE FROM sync_arrival_outbox WHERE user_id=$1 AND email_id=$2 AND message_id=$3 AND lease=$4`, user, email, key, lease)
		finish()
	} else {
		finish = arrivaldiag.Begin(ctx, arrivaldiag.Retry)
		_, updateErr := r.db.Exec(ctx, `UPDATE sync_arrival_outbox SET locked_until=NULL,retry_at=now()+interval '15 seconds'
			WHERE user_id=$1 AND email_id=$2 AND message_id=$3 AND lease=$4`, user, email, key, lease)
		finish()
		if updateErr != nil {
			return updateErr
		}
	}
	return err
}
