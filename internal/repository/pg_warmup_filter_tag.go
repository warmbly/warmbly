package repository

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// WarmupFilterTagRepository reads and writes a mailbox's opt-in filter tag, scoped to its organization.
type WarmupFilterTagRepository struct {
	db *pgxpool.Pool
}

func NewWarmupFilterTagRepository(db *pgxpool.Pool) *WarmupFilterTagRepository {
	return &WarmupFilterTagRepository{db: db}
}

// Get is the mailbox's tag, nil when off; found is false when the mailbox is not the organization's.
func (r *WarmupFilterTagRepository) Get(ctx context.Context, orgID, accountID uuid.UUID) (tag *string, found bool, err error) {
	err = r.db.QueryRow(ctx, `SELECT warmup_filter_tag FROM email_accounts WHERE id = $1 AND organization_id = $2`, accountID, orgID).Scan(&tag)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, false, nil
	}
	return tag, err == nil, err
}

// Set stores the tag, nil turning it off; found is false when the mailbox is not the organization's.
func (r *WarmupFilterTagRepository) Set(ctx context.Context, orgID, accountID uuid.UUID, tag *string) (bool, error) {
	ct, err := r.db.Exec(ctx, `UPDATE email_accounts SET warmup_filter_tag = $3, updated_at = NOW() WHERE id = $1 AND organization_id = $2`, accountID, orgID, tag)
	if err != nil {
		return false, err
	}
	return ct.RowsAffected() == 1, nil
}

// RecipientWarmupFilterTag is the tag a warmup send to this mailbox carries, empty when off.
func (r *warmupRepository) RecipientWarmupFilterTag(ctx context.Context, accountID uuid.UUID) (string, error) {
	var tag *string
	err := resultDB(ctx, r.db).QueryRow(ctx, `SELECT warmup_filter_tag FROM email_accounts WHERE id = $1`, accountID).Scan(&tag)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	if tag == nil {
		return "", nil
	}
	return *tag, nil
}
