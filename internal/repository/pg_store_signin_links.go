package repository

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// StoreSignInLink is a client connect link the store minted for one purchased mailbox that connects by sign-in.
type StoreSignInLink struct {
	LinkID         uuid.UUID
	OrganizationID uuid.UUID
	StoreMailboxID uuid.UUID
	Email          string
	ExpiresAt      time.Time
}

// StoreSignInLinkRepository keeps which purchased mailbox each sign-in link is for.
type StoreSignInLinkRepository interface {
	Insert(ctx context.Context, l StoreSignInLink) error
	// Get is the link's row, nil for a link the store did not mint.
	Get(ctx context.Context, linkID uuid.UUID) (*StoreSignInLink, error)
}

type storeSignInLinkRepository struct{ db *pgxpool.Pool }

// NewStoreSignInLinkRepository builds the Postgres repository.
func NewStoreSignInLinkRepository(db *pgxpool.Pool) StoreSignInLinkRepository {
	return &storeSignInLinkRepository{db: db}
}

func (r *storeSignInLinkRepository) Insert(ctx context.Context, l StoreSignInLink) error {
	_, err := r.db.Exec(ctx, `INSERT INTO store_signin_links (link_id, organization_id, store_mailbox_id, email, expires_at)
		VALUES ($1, $2, $3, $4, $5)`, l.LinkID, l.OrganizationID, l.StoreMailboxID, l.Email, l.ExpiresAt)
	return err
}

func (r *storeSignInLinkRepository) Get(ctx context.Context, linkID uuid.UUID) (*StoreSignInLink, error) {
	var l StoreSignInLink
	err := r.db.QueryRow(ctx, `SELECT link_id, organization_id, store_mailbox_id, email, expires_at FROM store_signin_links WHERE link_id = $1`, linkID).
		Scan(&l.LinkID, &l.OrganizationID, &l.StoreMailboxID, &l.Email, &l.ExpiresAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &l, nil
}
