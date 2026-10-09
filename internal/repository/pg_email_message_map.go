package repository

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/warmbly/warmbly/internal/app/cipher"
	"github.com/warmbly/warmbly/internal/infrastructure/db"
)

// pgEmailMessageMapRepository is the Postgres-backed implementation used by the
// backend, which serves it to workers over the internal API. The composite key
// (user_id, email_id, message_id) is canonical — see the 000003 migration.
type pgEmailMessageMapRepository struct {
	db     *db.DB
	cipher cipher.CipherService
}

// NewEmailMessageMapRepository returns the Postgres-backed implementation.
func NewEmailMessageMapRepository(d *db.DB) EmailMessageMapRepository {
	return &pgEmailMessageMapRepository{db: d}
}

func (r *pgEmailMessageMapRepository) Add(ctx context.Context, data EmailMessageData) error {
	userID, err := uuid.Parse(data.UserID)
	if err != nil {
		return fmt.Errorf("email_message_map: invalid user_id: %w", err)
	}
	emailID, err := uuid.Parse(data.EmailID)
	if err != nil {
		return fmt.Errorf("email_message_map: invalid email_id: %w", err)
	}
	id, err := uuid.Parse(data.ID)
	if err != nil {
		return fmt.Errorf("email_message_map: invalid id: %w", err)
	}

	// Upsert: the old DynamoDB PutItem overwrote unconditionally.
	const q = `
		INSERT INTO email_message_map (user_id, email_id, message_id, id, thread_id)
		VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT (user_id, email_id, message_id)
		DO UPDATE SET id = EXCLUDED.id, thread_id = EXCLUDED.thread_id
		WHERE NOT EXISTS (SELECT 1 FROM sync_arrival_outbox o
		 WHERE o.user_id = EXCLUDED.user_id AND o.email_id = EXCLUDED.email_id AND o.message_id = EXCLUDED.message_id)
	`
	if _, err := r.db.Exec(ctx, q, userID, emailID, data.MessageID, id, data.ThreadID); err != nil {
		return fmt.Errorf("email_message_map: add: %w", err)
	}
	return nil
}

func (r *pgEmailMessageMapRepository) Get(ctx context.Context, userID, emailID uuid.UUID, messageID string) (*EmailMessageData, error) {
	const q = `
		SELECT id, thread_id
		FROM email_message_map
		WHERE user_id = $1 AND email_id = $2 AND message_id = $3
	`
	var id uuid.UUID
	var threadID string
	err := r.db.QueryRow(ctx, q, userID, emailID, messageID).Scan(&id, &threadID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("email_message_map: get: %w", err)
	}
	return &EmailMessageData{
		UserID:    userID.String(),
		EmailID:   emailID.String(),
		MessageID: messageID,
		ID:        id.String(),
		ThreadID:  threadID,
	}, nil
}

func (r *pgEmailMessageMapRepository) Del(ctx context.Context, userID, emailID uuid.UUID, messageID string, id uuid.UUID) error {
	// Legacy publication rollback must not remove an already admitted arrival.
	const q = `DELETE FROM email_message_map m WHERE user_id=$1 AND email_id=$2 AND message_id=$3
		AND ($4::uuid='00000000-0000-0000-0000-000000000000' OR id=$4)
		AND NOT EXISTS (SELECT 1 FROM sync_arrival_outbox o
		 WHERE o.user_id=m.user_id AND o.email_id=m.email_id AND o.message_id=m.message_id)`
	if _, err := r.db.Exec(ctx, q, userID, emailID, messageID, id); err != nil {
		return fmt.Errorf("email_message_map: del: %w", err)
	}
	return nil
}
