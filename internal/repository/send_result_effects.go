package repository

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
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

func (r *advancedOutreachRepository) DeliverSendResultEffects(ctx context.Context, deliver func(context.Context, SendResultEffect) error) error {
	for range 10 {
		var id, lease uuid.UUID
		var raw []byte
		err := r.db.QueryRow(ctx, `WITH candidate AS(SELECT id FROM send_result_effects WHERE delivered_at IS NULL AND (locked_until IS NULL OR locked_until<NOW()) ORDER BY created_at LIMIT 1 FOR UPDATE SKIP LOCKED)
		UPDATE send_result_effects e SET lease=gen_random_uuid(),locked_until=NOW()+INTERVAL '1 minute',attempts=attempts+1 FROM candidate c WHERE e.id=c.id RETURNING e.id,e.lease,e.payload`).Scan(&id, &lease, &raw)
		if errors.Is(err, pgx.ErrNoRows) {
			break
		}
		if err != nil {
			return err
		}
		var effect SendResultEffect
		if err = json.Unmarshal(raw, &effect); err != nil {
			return err
		}
		effect.ID = id
		if err = deliver(context.WithValue(ctx, sendResultEffectIDKey{}, id), effect); err != nil {
			return err
		}
		if _, err = r.db.Exec(ctx, `UPDATE send_result_effects SET delivered_at=NOW(),locked_until=NULL WHERE id=$1 AND lease=$2`, id, lease); err != nil {
			return err
		}
	}
	_, err := r.db.Exec(ctx, `DELETE FROM send_result_effects WHERE delivered_at<NOW()-INTERVAL '7 days'`)
	return err
}
