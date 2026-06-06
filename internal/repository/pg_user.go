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
	UpdateOnboarding(ctx context.Context, userID uuid.UUID, firstName, lastName, referralSource, role, teamSize string) error
	UpdateProfile(ctx context.Context, userID uuid.UUID, firstName, lastName string) error
	UpdateAvatar(ctx context.Context, userID uuid.UUID, avatarURL *string) error

	// GetBanState returns the user's ban_scope bitmask (0 = not
	// banned). Used by middleware to enforce BanScopeLogin etc.
	// without re-fetching the full user row.
	GetBanState(ctx context.Context, userID uuid.UUID) (scope uint32, err error)
}

type userRepository struct {
	DB  *db.DB
	kms kms.Provider
}

func NewUserRepostory(db *db.DB, kms kms.Provider) UserRepository {
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
			created_at, updated_at
		)
		VALUES (
			$1, $2, $3,
			$4, $5,
			$6, $6
		)
	`

	var params = []any{
		id, email.Address, passwordHash,
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
		`SELECT u.id, u.email, u.first_name, u.last_name, u.avatar_url, u.referral_source, u.onboarding_completed_at,
		   u.max_organizations, u.free_trial_used, u.admin_permissions,
		   u.deletion_scheduled_at, u.deletion_scheduled_for,
		   u.updated_at, u.created_at,
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

	var adminPerm uint32
	err := r.DB.QueryRow(
		ctx,
		q,
		params...,
	).Scan(&u.ID, &u.Email, &u.FirstName, &u.LastName, &u.AvatarURL, &u.ReferralSource, &u.OnboardingCompletedAt,
		&u.MaxOrganizations, &u.FreeTrialUsed, &adminPerm,
		&u.DeletionScheduledAt, &u.DeletionScheduledFor,
		&u.UpdatedAt, &u.CreatedAt, &u.Roles)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, errx.ErrUser
		}
		db.CaptureError(err, q, params, "queryrow")
		return nil, err
	}

	// Expose platform-admin status so the admin app's auth guard can gate on it.
	u.AdminPermissions = models.AdminPermission(adminPerm)
	u.IsAdmin = adminPerm != 0

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

func (r *userRepository) UpdateOnboarding(ctx context.Context, userID uuid.UUID, firstName, lastName, referralSource, role, teamSize string) error {
	const q = `UPDATE users SET first_name=$2, last_name=$3, referral_source=$4, job_role=NULLIF($5,''), team_size=NULLIF($6,''), onboarding_completed_at=NOW(), updated_at=NOW() WHERE id=$1`
	_, err := r.DB.Exec(ctx, q, userID, firstName, lastName, referralSource, role, teamSize)
	return err
}

func (r *userRepository) UpdateProfile(ctx context.Context, userID uuid.UUID, firstName, lastName string) error {
	const q = `UPDATE users SET first_name=$2, last_name=$3, updated_at=NOW() WHERE id=$1`
	_, err := r.DB.Exec(ctx, q, userID, firstName, lastName)
	return err
}

func (r *userRepository) UpdateAvatar(ctx context.Context, userID uuid.UUID, avatarURL *string) error {
	const q = `UPDATE users SET avatar_url=$2, updated_at=NOW() WHERE id=$1`
	_, err := r.DB.Exec(ctx, q, userID, avatarURL)
	return err
}

// GetBanState reads only ban_scope — banned_at is implied by
// scope > 0 since unban sets both back to zero. Returns 0 for unbanned
// users and for users that don't exist (the latter is fine because
// those callers fail elsewhere on the auth check).
func (r *userRepository) GetBanState(ctx context.Context, userID uuid.UUID) (uint32, error) {
	const q = `SELECT ban_scope FROM users WHERE id = $1`
	var scope uint32
	err := r.DB.QueryRow(ctx, q, userID).Scan(&scope)
	return scope, err
}
