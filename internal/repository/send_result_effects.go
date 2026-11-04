package repository

import (
	"context"
	"encoding/json"
	"slices"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog/log"
	"github.com/warmbly/warmbly/internal/models"
)

type SendResultEffect struct {
	ID             uuid.UUID                   `json:"-"`
	Kind           string                      `json:"kind"`
	OrganizationID uuid.UUID                   `json:"organization_id"`
	EventType      models.WebhookEventType     `json:"event_type,omitempty"`
	Data           map[string]any              `json:"data,omitempty"`
	UserID         uuid.UUID                   `json:"user_id,omitempty"`
	Category       models.NotificationCategory `json:"category,omitempty"`
	Title          string                      `json:"title,omitempty"`
	Body           string                      `json:"body,omitempty"`
	Link           string                      `json:"link,omitempty"`
}

func QueueSendResultEffect(ctx context.Context, key string, effect SendResultEffect) bool {
	state, ok := ctx.Value(sendResultKey{}).(sendResultContext)
	if !ok {
		return false
	}
	raw, err := json.Marshal(effect)
	if err == nil {
		_, err = state.tx.Exec(ctx, `INSERT INTO send_result_effects(task_id,effect_key,organization_id,kind,payload)VALUES($1,$2,$3,$4,$5)ON CONFLICT(task_id,effect_key)DO NOTHING`, state.taskID, key, effect.OrganizationID, effect.Kind, raw)
	}
	if err != nil && state.effectError != nil {
		*state.effectError = err
	}
	return true
}

type SendResultEffectOutbox interface {
	DeliverSendResultEffects(context.Context, func(context.Context, SendResultEffect) error) error
}

type sendResultEffectIDKey struct{}

func SendResultEffectEventID(ctx context.Context) uuid.UUID {
	id, _ := ctx.Value(sendResultEffectIDKey{}).(uuid.UUID)
	return id
}

// One claim statement leases a batch; a pass drains batches until one comes back short.
const (
	sendResultEffectBatch   = 25
	sendResultEffectBatches = 20
)

type claimedSendResultEffect struct {
	id, lease uuid.UUID
	raw       []byte
	created   time.Time
}

func (r *advancedOutreachRepository) DeliverSendResultEffects(ctx context.Context, deliver func(context.Context, SendResultEffect) error) error {
	for range sendResultEffectBatches {
		batch, err := r.claimSendResultEffects(ctx, sendResultEffectBatch)
		if err != nil {
			return err
		}
		for i, c := range batch {
			if err := r.deliverSendResultEffect(ctx, c, deliver); err != nil {
				r.releaseSendResultEffects(batch[i+1:])
				return err
			}
		}
		if len(batch) < sendResultEffectBatch {
			break
		}
	}
	return nil
}

// claimSendResultEffects leases up to limit pending effects, oldest first.
func (r *advancedOutreachRepository) claimSendResultEffects(ctx context.Context, limit int) ([]claimedSendResultEffect, error) {
	rows, err := r.db.Query(ctx, `WITH candidate AS(SELECT id FROM send_result_effects WHERE delivered_at IS NULL AND (locked_until IS NULL OR locked_until<NOW()) ORDER BY created_at LIMIT $1 FOR UPDATE SKIP LOCKED)
		UPDATE send_result_effects e SET lease=gen_random_uuid(),locked_until=NOW()+INTERVAL '1 minute',attempts=attempts+1 FROM candidate c WHERE e.id=c.id RETURNING e.id,e.lease,e.payload,e.created_at`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var claimed []claimedSendResultEffect
	for rows.Next() {
		var c claimedSendResultEffect
		if err := rows.Scan(&c.id, &c.lease, &c.raw, &c.created); err != nil {
			return nil, err
		}
		claimed = append(claimed, c)
	}
	// RETURNING has no order; deliver oldest first, as the claim chose them.
	slices.SortFunc(claimed, func(a, b claimedSendResultEffect) int { return a.created.Compare(b.created) })
	return claimed, rows.Err()
}

func (r *advancedOutreachRepository) deliverSendResultEffect(ctx context.Context, c claimedSendResultEffect, deliver func(context.Context, SendResultEffect) error) error {
	var effect SendResultEffect
	if err := json.Unmarshal(c.raw, &effect); err != nil {
		return err
	}
	effect.ID = c.id
	if err := deliver(context.WithValue(ctx, sendResultEffectIDKey{}, c.id), effect); err != nil {
		return err
	}
	_, err := r.db.Exec(ctx, `UPDATE send_result_effects SET delivered_at=NOW(),locked_until=NULL WHERE id=$1 AND lease=$2`, c.id, c.lease)
	return err
}

// releaseSendResultEffects hands claims a failed pass never attempted back to the next pass, uncounted.
func (r *advancedOutreachRepository) releaseSendResultEffects(batch []claimedSendResultEffect) {
	if len(batch) == 0 {
		return
	}
	ids, leases := make([]uuid.UUID, len(batch)), make([]uuid.UUID, len(batch))
	for i, c := range batch {
		ids[i], leases[i] = c.id, c.lease
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if _, err := r.db.Exec(ctx, `UPDATE send_result_effects e SET locked_until=NULL,attempts=GREATEST(e.attempts-1,0)
		FROM unnest($1::uuid[],$2::uuid[]) AS c(id,lease) WHERE e.id=c.id AND e.lease=c.lease AND e.delivered_at IS NULL`, ids, leases); err != nil {
		log.Warn().Err(err).Msg("send result effects: release unattempted claims")
	}
}
