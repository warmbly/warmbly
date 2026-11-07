package repository

import (
	"context"
	"errors"
	"net/mail"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/infrastructure/db"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/observability/errs"
	"github.com/warmbly/warmbly/internal/pkg/argon2"
)

type AuthRepository interface {
	IsValidCredentials(ctx context.Context, email, password string) (uuid.UUID, *errx.Error)
	ExternalLogin(ctx context.Context, email string) (*models.User, *errx.Error)
	ResetPassword(ctx context.Context, userID uuid.UUID, password string) *errx.Error
	GetPasswordHash(ctx context.Context, userID uuid.UUID) (string, *errx.Error)
	// PasswordChangedAt is when the password was last written, nil when it
	// has not been since the column existed.
	PasswordChangedAt(ctx context.Context, userID uuid.UUID) (*time.Time, *errx.Error)
	// InitialPasswordPending reports whether the live password is an issued
	// initial one its holder still has to replace.
	InitialPasswordPending(ctx context.Context, userID uuid.UUID) (bool, *errx.Error)
	// ReplaceInitialPassword stores the holder's own password over the live
	// initial password whose hash is oldHash, and reports whether it did.
	ReplaceInitialPassword(ctx context.Context, userID uuid.UUID, oldHash, newHash string) (bool, *errx.Error)
}

type authRepository struct {
	DB *db.DB
}

func NewAuthRepostory(db *db.DB) AuthRepository {
	return &authRepository{
		DB: db,
	}
}

// livePasswordHash reads an expired password as no password at all.
const livePasswordHash = `CASE WHEN password_expires_at <= now() THEN NULL ELSE password_hash END`

func (r *authRepository) IsValidCredentials(ctx context.Context, email, password string) (uuid.UUID, *errx.Error) {
	var id uuid.UUID
	var pw *string

	query := `
		SELECT id, ` + livePasswordHash + `
		FROM users
		WHERE email = $1
	`

	params := []any{
		normalizeUserEmail(email),
	}

	err := r.DB.QueryRow(
		ctx,
		query,
		params...,
	).Scan(&id, &pw)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return uuid.Nil, errx.ErrCredentials
		}
		db.CaptureError(err, query, params, "queryrow")
		return uuid.Nil, errx.InternalError()
	}

	// External sign-in accounts have no password until one is set through a reset.
	if pw == nil || *pw == "" {
		return uuid.Nil, errx.ErrCredentials
	}

	val, err := argon2.Verify(password, *pw)
	if err != nil {
		// A stored hash this cannot parse is an operator problem, so it is
		// still reported. It is not the caller's, though: answering 500 told
		// someone with a correct password that the site was down, and they
		// retried into it. The credential cannot be verified, which is what
		// ErrCredentials says.
		errs.CaptureException(err)
		return uuid.Nil, errx.ErrCredentials
	}

	if !val {
		return uuid.Nil, errx.ErrCredentials
	}

	return id, nil
}

func (r *authRepository) ExternalLogin(ctx context.Context, email string) (*models.User, *errx.Error) {
	id := uuid.NewString()
	vMail, err := mail.ParseAddress(email)
	if err != nil {
		return nil, errx.ErrEmail
	}

	firstName := vMail.Name
	lastName := ""
	now := time.Now()

	query := `
        INSERT INTO users (id, email, password_hash, first_name, last_name, created_at, updated_at)
        VALUES ($1, $2, $3, $4, $5, $6, $6)
        ON CONFLICT (email) DO UPDATE
        SET updated_at = EXCLUDED.updated_at
        RETURNING id, email, first_name, last_name, created_at, updated_at;
    `

	var params = []any{
		id,
		normalizeUserEmail(email),
		"",
		firstName,
		lastName,
		now,
	}

	var u models.User
	err = r.DB.QueryRow(
		ctx,
		query,
		params...,
	).Scan(&u.ID, &u.Email, &u.FirstName, &u.LastName, &u.CreatedAt, &u.UpdatedAt)
	if err != nil {
		db.CaptureError(err, query, nil, "queryrow")
		return nil, errx.InternalError()
	}

	return &u, nil
}

// GetPasswordHash returns the stored argon2 hash for a user (empty when the
// account is OAuth-only / passwordless or its password has expired).
func (r *authRepository) GetPasswordHash(ctx context.Context, userID uuid.UUID) (string, *errx.Error) {
	var hash *string
	err := r.DB.QueryRow(ctx, `SELECT `+livePasswordHash+` FROM users WHERE id = $1`, userID).Scan(&hash)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return "", errx.ErrNotFound
		}
		db.CaptureError(err, "get password hash", []any{userID}, "queryrow")
		return "", errx.InternalError()
	}
	if hash == nil {
		return "", nil
	}
	return *hash, nil
}

// PasswordChangedAt returns the last password write, which is the floor a
// reset link's issue time must clear.
func (r *authRepository) PasswordChangedAt(ctx context.Context, userID uuid.UUID) (*time.Time, *errx.Error) {
	var at *time.Time
	err := r.DB.QueryRow(ctx, `SELECT password_changed_at FROM users WHERE id = $1`, userID).Scan(&at)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, errx.ErrNotFound
		}
		db.CaptureError(err, "get password changed at", []any{userID}, "queryrow")
		return nil, errx.InternalError()
	}
	return at, nil
}

// ResetPassword is the one write of a password hash, so it is also the one
// place the change is stamped: every reset link issued before this instant is
// refused from here on, whichever path (reset, change, operator CLI) wrote it.
// A password written here is chosen rather than issued, so it never expires.
func (r *authRepository) ResetPassword(ctx context.Context, userID uuid.UUID, passwordHash string) *errx.Error {
	query := `
		UPDATE users
		SET password_hash = $1, password_expires_at = NULL, password_changed_at = now(), updated_at = now()
		WHERE id = $2
	`
	params := []any{
		passwordHash,
		userID,
	}

	if _, err := r.DB.Exec(ctx, query, params...); err != nil {
		db.CaptureError(err, query, params, "exec")
		return errx.InternalError()
	}

	return nil
}

func (r *authRepository) InitialPasswordPending(ctx context.Context, userID uuid.UUID) (bool, *errx.Error) {
	var pending bool
	err := r.DB.QueryRow(ctx, `SELECT COALESCE(password_expires_at > now(), false) FROM users WHERE id = $1`, userID).Scan(&pending)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return false, errx.ErrNotFound
		}
		db.CaptureError(err, "initial password pending", []any{userID}, "queryrow")
		return false, errx.InternalError()
	}
	return pending, nil
}

func (r *authRepository) ReplaceInitialPassword(ctx context.Context, userID uuid.UUID, oldHash, newHash string) (bool, *errx.Error) {
	tag, err := r.DB.Exec(ctx, `
		UPDATE users
		SET password_hash = $3, password_expires_at = NULL, password_changed_at = now(), updated_at = now()
		WHERE id = $1 AND password_hash = $2 AND password_expires_at > now()`, userID, oldHash, newHash)
	if err != nil {
		db.CaptureError(err, "replace initial password", []any{userID}, "exec")
		return false, errx.InternalError()
	}
	return tag.RowsAffected() == 1, nil
}
