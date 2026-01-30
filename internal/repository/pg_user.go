package repository

import (
	"context"
	"errors"
	"fmt"
	"net/mail"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/infrastructure/db"
	"github.com/warmbly/warmbly/internal/infrastructure/kms"
	"github.com/warmbly/warmbly/internal/models"
)

type UserRepository interface {
	CreateUser(ctx context.Context, email *mail.Address, password string) (*models.User, error)
	GetUser(ctx context.Context, id uuid.UUID) (*models.User, error)
	GetUserByEmail(ctx context.Context, email string) (*models.User, error)
	SetFreeTrialUsed(ctx context.Context, userID uuid.UUID) error
}

type userRepository struct {
	DB  *db.DB
	kms *kms.KMS
}

func NewUserRepostory(db *db.DB, kms *kms.KMS) UserRepository {
	return &userRepository{
		DB:  db,
		kms: kms,
	}
}

func (r *userRepository) CreateUser(ctx context.Context, email *mail.Address, passwordHash string) (*models.User, error) {
	id := uuid.New()

	var firstName string

	nameSplit := strings.SplitN(email.Address, "@", 2)
	if len(nameSplit) < 2 {
		firstName = "Unknown"
	} else {
		firstName = nameSplit[0]
	}

	var lastName string
	now := time.Now()

	const q = `
		INSERT INTO users (
			id, email, password_hash,
			first_name, last_name,
			encrypted_data_key,
			created_at, updated_at
		)
		VALUES (
			$1, $2, $3,
			$4, $5,
			$6, $6
		)
	`

	var params = []any{
		id, email, passwordHash,
		firstName, lastName,
		now,
	}

	_, err := r.DB.Exec(
		ctx,
		q,
		params...)
	if err != nil {
		db.CaptureError(err, q, nil, "exec")
		return nil, err
	}

	return &models.User{
		ID: id,

		FirstName: firstName,
		LastName:  lastName,
		Email:     email.Address,
		Roles:     make([]uuid.UUID, 0),

		CreatedAt: now,
		UpdatedAt: now,
	}, nil
}

func (r *userRepository) getUser(ctx context.Context, key string, value any) (*models.User, error) {
	var u models.User

	q := fmt.Sprintf(
		`SELECT u.email, u.max_organizations, u.free_trial_used, u.updated_at, u.created_at,
		   COALESCE(array_agg(ur.role_id) FILTER (WHERE ur.role_id IS NOT NULL), '{}') AS role_ids
		  FROM users u
		  LEFT JOIN user_roles ur ON ur.user_id = u.id
		  WHERE u.%s = $1
		  GROUP BY u.id`,
		key,
	)

	var params = []any{
		value,
	}

	err := r.DB.QueryRow(
		ctx,
		q,
		params...,
	).Scan(&u.Email, &u.MaxOrganizations, &u.FreeTrialUsed, &u.UpdatedAt, &u.CreatedAt, &u.Roles)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, errx.ErrUser
		}
		db.CaptureError(err, q, params, "queryrow")
		return nil, err
	}
	return &u, nil
}

func (r *userRepository) GetUser(ctx context.Context, userID uuid.UUID) (*models.User, error) {
	return r.getUser(ctx, "id", userID)
}

func (r *userRepository) GetUserByEmail(ctx context.Context, email string) (*models.User, error) {
	return r.getUser(ctx, "email", email)
}

func (r *userRepository) SetFreeTrialUsed(ctx context.Context, userID uuid.UUID) error {
	const q = `UPDATE users SET free_trial_used = TRUE, updated_at = NOW() WHERE id = $1`
	_, err := r.DB.Exec(ctx, q, userID)
	return err
}
