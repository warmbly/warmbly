package repository

import (
	"context"
	"errors"
	"net/mail"
	"time"

	"github.com/getsentry/sentry-go"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/infrastructure/db"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/pkg/argon2"
)

type AuthRepository interface {
	IsValidCredentials(ctx context.Context, email, password string) (uuid.UUID, *errx.Error)
	ExternalLogin(ctx context.Context, email string) (*models.User, *errx.Error)
	ResetPassword(ctx context.Context, userID uuid.UUID, password string) *errx.Error
	GetPasswordHash(ctx context.Context, userID uuid.UUID) (string, *errx.Error)
}

type authRepository struct {
	DB *db.DB
}

func NewAuthRepostory(db *db.DB) AuthRepository {
	return &authRepository{
		DB: db,
	}
}

func (r *authRepository) IsValidCredentials(ctx context.Context, email, password string) (uuid.UUID, *errx.Error) {
	var id uuid.UUID
	var pw string

	query := `
		SELECT id, password_hash
		FROM users
		WHERE email = $1
	`

	params := []any{
		email,
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

	val, err := argon2.Verify(password, pw)
	if err != nil {
		sentry.CaptureException(err)
		return uuid.Nil, errx.InternalError()
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
		email,
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
// account is OAuth-only / passwordless).
func (r *authRepository) GetPasswordHash(ctx context.Context, userID uuid.UUID) (string, *errx.Error) {
	var hash *string
	err := r.DB.QueryRow(ctx, `SELECT password_hash FROM users WHERE id = $1`, userID).Scan(&hash)
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

func (r *authRepository) ResetPassword(ctx context.Context, userID uuid.UUID, passwordHash string) *errx.Error {
	query := `
		UPDATE users
		SET password_hash = $1, updated_at = now()
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
