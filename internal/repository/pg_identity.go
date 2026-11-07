package repository

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/warmbly/warmbly/internal/models"
)

// IdentityRepository stores the (provider, issuer, subject) bindings that
// federated sign-in resolves accounts by. Matching on the subject rather than
// the email is what stops a self-registration IdP from being able to claim an
// existing account.
type IdentityRepository interface {
	// FindUserByIdentity returns the user bound to this issuer and subject, or
	// uuid.Nil when the identity is unknown.
	FindUserByIdentity(ctx context.Context, issuer, subject string) (uuid.UUID, error)

	// Link binds an identity to a user. The unique index on (issuer, subject)
	// makes a second claim on the same identity fail rather than overwrite.
	Link(ctx context.Context, userID uuid.UUID, identity models.UserIdentity) error

	// HasIdentityForIssuer reports whether the user already has a different
	// subject from this issuer. Used to refuse the email fallback for a user
	// who is already federated: a second subject claiming the same address is
	// an impersonation attempt, not a re-login.
	HasIdentityForIssuer(ctx context.Context, userID uuid.UUID, issuer string) (bool, error)

	// TouchLogin records a successful federated sign-in.
	TouchLogin(ctx context.Context, issuer, subject string) error

	// ListForUser returns every identity linked to a user, for the account
	// security screen.
	ListForUser(ctx context.Context, userID uuid.UUID) ([]models.UserIdentity, error)

	// Unlink removes the user's identity from provider unless it is the
	// account's last way to sign in.
	Unlink(ctx context.Context, userID uuid.UUID, provider string) error
}

type identityRepository struct {
	db *pgxpool.Pool
}

func NewIdentityRepository(db *pgxpool.Pool) IdentityRepository {
	return &identityRepository{db: db}
}

func (r *identityRepository) FindUserByIdentity(ctx context.Context, issuer, subject string) (uuid.UUID, error) {
	const q = `SELECT user_id FROM user_identities WHERE issuer = $1 AND subject = $2`
	var id uuid.UUID
	err := r.db.QueryRow(ctx, q, issuer, subject).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return uuid.Nil, nil
	}
	return id, err
}

func (r *identityRepository) Link(ctx context.Context, userID uuid.UUID, identity models.UserIdentity) error {
	const q = `
		INSERT INTO user_identities (user_id, provider, issuer, subject, email, last_login_at)
		VALUES ($1, $2, $3, $4, $5, NOW())
		ON CONFLICT (issuer, subject) DO UPDATE
			SET last_login_at = NOW(), email = EXCLUDED.email
			WHERE user_identities.user_id = EXCLUDED.user_id
	`
	tag, err := r.db.Exec(ctx, q, userID, identity.Provider, identity.Issuer, identity.Subject, identity.Email)
	if err != nil {
		return err
	}
	// The conflict branch updates only the owner's row, so zero rows means
	// another account holds this identity.
	if tag.RowsAffected() == 0 {
		return ErrIdentityTaken
	}
	return nil
}

// ErrIdentityTaken is Link's answer when the (issuer, subject) pair already
// belongs to a different account.
var ErrIdentityTaken = errors.New("identity is linked to another account")

func (r *identityRepository) HasIdentityForIssuer(ctx context.Context, userID uuid.UUID, issuer string) (bool, error) {
	const q = `SELECT EXISTS (SELECT 1 FROM user_identities WHERE user_id = $1 AND issuer = $2)`
	var exists bool
	err := r.db.QueryRow(ctx, q, userID, issuer).Scan(&exists)
	return exists, err
}

func (r *identityRepository) TouchLogin(ctx context.Context, issuer, subject string) error {
	const q = `UPDATE user_identities SET last_login_at = NOW() WHERE issuer = $1 AND subject = $2`
	_, err := r.db.Exec(ctx, q, issuer, subject)
	return err
}

func (r *identityRepository) ListForUser(ctx context.Context, userID uuid.UUID) ([]models.UserIdentity, error) {
	const q = `
		SELECT provider, issuer, subject, COALESCE(email, ''), created_at, last_login_at
		FROM user_identities WHERE user_id = $1 ORDER BY created_at
	`
	rows, err := r.db.Query(ctx, q, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []models.UserIdentity{}
	for rows.Next() {
		var identity models.UserIdentity
		var lastLogin *time.Time
		if err := rows.Scan(&identity.Provider, &identity.Issuer, &identity.Subject,
			&identity.Email, &identity.CreatedAt, &lastLogin); err != nil {
			return nil, err
		}
		identity.LastLoginAt = lastLogin
		out = append(out, identity)
	}
	return out, rows.Err()
}

// ErrIdentityNotLinked is Unlink's answer when the user has no identity from
// that provider.
var ErrIdentityNotLinked = errors.New("identity is not linked to this account")

// ErrLastSignInMethod is Unlink's answer when the identity is the account's
// only way to sign in: no password, no passkey and no other identity.
var ErrLastSignInMethod = errors.New("identity is the last sign-in method")

func (r *identityRepository) Unlink(ctx context.Context, userID uuid.UUID, provider string) error {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	remains, err := signInMethodRemains(ctx, tx, userID, provider, uuid.Nil)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrIdentityNotLinked
	}
	if err != nil {
		return err
	}
	tag, err := tx.Exec(ctx, `DELETE FROM user_identities WHERE user_id = $1 AND provider = $2`, userID, provider)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrIdentityNotLinked
	}
	if !remains {
		return ErrLastSignInMethod
	}
	return tx.Commit(ctx)
}

// signInMethodRemains locks the user row, which every removal of a way in
// takes first, and reports whether one remains besides the given provider or
// passkey. pgx.ErrNoRows means the user does not exist.
func signInMethodRemains(ctx context.Context, tx pgx.Tx, userID uuid.UUID, provider string, passkeyID uuid.UUID) (bool, error) {
	var hasPassword bool
	err := tx.QueryRow(ctx, `SELECT COALESCE(`+livePasswordHash+`, '') <> '' FROM users WHERE id = $1 FOR UPDATE`, userID).Scan(&hasPassword)
	if err != nil || hasPassword {
		return hasPassword, err
	}
	var remains bool
	err = tx.QueryRow(ctx, `
		SELECT EXISTS (SELECT 1 FROM user_identities WHERE user_id = $1 AND provider <> $2)
		    OR EXISTS (SELECT 1 FROM webauthn_credentials WHERE user_id = $1 AND id <> $3)
	`, userID, provider, passkeyID).Scan(&remains)
	return remains, err
}
