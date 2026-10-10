package repository

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/mail"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/infrastructure/db"
	"github.com/warmbly/warmbly/internal/infrastructure/kms"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/pkg/displayname"
)

type UserRepository interface {
	// RecordSignupMetadata stores where a signup came from and what its address
	// scored, for later correlation. Never used for authentication.
	RecordSignupMetadata(ctx context.Context, userID uuid.UUID, ip, userAgent string, emailRisk int, normalizedEmail string) error

	CreateUser(ctx context.Context, email *mail.Address, password string) (*models.User, error)
	GetUser(ctx context.Context, id uuid.UUID) (*models.User, error)

	// IsLoginCodeExempt reports whether this account skips the emailed login
	// code. Read on the login path, so it is a single boolean rather than a
	// whole user load.
	IsLoginCodeExempt(ctx context.Context, id uuid.UUID) (bool, error)

	// SetLoginCodeExempt grants or clears the exemption. A reason is required
	// to grant one; the database refuses a blank one too.
	SetLoginCodeExempt(ctx context.Context, id uuid.UUID, exempt bool, reason string, by *uuid.UUID) error

	// ListLoginCodeExempt returns every exempt account, for the instance check
	// that keeps a forgotten exemption visible.
	ListLoginCodeExempt(ctx context.Context) ([]models.LoginCodeExemption, error)

	// CreateExemptUser creates an account and its login-code exemption in one
	// transaction, so a failure cannot leave an account that holds no
	// exemption and therefore appears in no list.
	// The password stops working at passwordExpiresAt.
	CreateExemptUser(ctx context.Context, email *mail.Address, passwordHash, reason string, by *uuid.UUID, passwordExpiresAt time.Time) (*models.User, error)

	// RevokeTester clears the exemption and, on an account whose password was
	// handed out with an expiry, the password too. It reports whether it did.
	RevokeTester(ctx context.Context, id uuid.UUID) (passwordCleared bool, err error)

	// DeleteOrphanExemptUser undoes a tester creation whose workspace step
	// failed. The predicate is the safety: it only matches an account that is
	// exempt AND belongs to no organization, which a real user never is.
	DeleteOrphanExemptUser(ctx context.Context, id uuid.UUID) error
	GetUserByEmail(ctx context.Context, email string) (*models.User, error)
	SetFreeTrialUsed(ctx context.Context, userID uuid.UUID) error
	UpdateOnboarding(ctx context.Context, userID uuid.UUID, firstName, lastName, referralSource, role, teamSize string) error
	MarkOnboarded(ctx context.Context, userID uuid.UUID) (time.Time, error)
	UpdateProfile(ctx context.Context, userID uuid.UUID, firstName, lastName string) error
	UpdateAvatar(ctx context.Context, userID uuid.UUID, avatarURL *string) error

	// GetBanState returns the user's ban_scope bitmask (0 = not
	// banned). Used by middleware to enforce BanScopeLogin etc.
	// without re-fetching the full user row.
	GetBanState(ctx context.Context, userID uuid.UUID) (scope uint32, err error)

	// GetUndoSendSeconds returns the user's undo-send window without
	// fetching the full user row (hot path: every instant send).
	GetUndoSendSeconds(ctx context.Context, userID uuid.UUID) (int, error)
	SetUndoSendSeconds(ctx context.Context, userID uuid.UUID, seconds int) error

	// IsEmpty reports whether the instance has no users at all. Drives the
	// first-launch exemption for DISABLE_REGISTRATION and the bootstrap owner,
	// both of which must only apply to a brand new install.
	IsEmpty(ctx context.Context) (bool, error)

	// CountUsers is the operator-facing count, for boot diagnostics and the
	// CLI. IsEmpty stays the hot-path check.
	CountUsers(ctx context.Context) (int, error)
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

// ErrUserEmailTaken is CreateUser's answer when the address already belongs to an account.
var ErrUserEmailTaken = errors.New("an account with this email address already exists")

// ErrUserNotFound is returned when the account a write names does not exist.
var ErrUserNotFound = errors.New("no such account")

func (r *userRepository) CreateUser(ctx context.Context, email *mail.Address, passwordHash string) (*models.User, error) {
	return createUser(ctx, r.DB, email, passwordHash)
}

type userCreator interface {
	Exec(context.Context, string, ...any) (pgconn.CommandTag, error)
}

func createUser(ctx context.Context, store userCreator, email *mail.Address, passwordHash string) (*models.User, error) {
	id := uuid.New()

	// The stored form is decided here, not by the caller, because the account
	// lookups all fold and `users_email_key` does not: a row written with the
	// case someone typed is a row no sign-in and no reset can find.
	address := normalizeUserEmail(email.Address)

	// Only a local part that passes the display-name rules becomes a name.
	firstName := displayname.FromEmail(address)

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
		id, address, passwordHash,
		firstName, lastName,
		now,
	}

	_, err := store.Exec(
		ctx,
		q,
		params...)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" && pgErr.ConstraintName == "users_email_key" {
			return nil, ErrUserEmailTaken
		}
		db.CaptureError(err, q, nil, "exec")
		return nil, err
	}

	return &models.User{
		ID: id,

		FirstName: firstName,
		LastName:  lastName,
		Email:     address,
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
		   u.deletion_scheduled_at, u.deletion_scheduled_for, u.undo_send_seconds,
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
		&u.DeletionScheduledAt, &u.DeletionScheduledFor, &u.UndoSendSeconds,
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

// normalizeUserEmail is the form `users.email` is written and matched in.
// `users_email_key` is a plain unique index on the raw column, so the case a
// row was written with is the only case that will ever find it again.
func normalizeUserEmail(email string) string {
	return strings.ToLower(strings.TrimSpace(email))
}

func (r *userRepository) GetUser(ctx context.Context, userID uuid.UUID) (*models.User, error) {
	return r.getUser(ctx, "id", userID)
}

func (r *userRepository) GetUserByEmail(ctx context.Context, email string) (*models.User, error) {
	return r.getUser(ctx, "email", normalizeUserEmail(email))
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

// MarkOnboarded skips the first-run wizard for an operator-provisioned account,
// keeping an earlier completion time when there is one.
func (r *userRepository) MarkOnboarded(ctx context.Context, userID uuid.UUID) (time.Time, error) {
	const q = `UPDATE users SET onboarding_completed_at=COALESCE(onboarding_completed_at, NOW()), updated_at=NOW() WHERE id=$1 RETURNING onboarding_completed_at`
	var at time.Time
	err := r.DB.QueryRow(ctx, q, userID).Scan(&at)
	return at, err
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

func (r *userRepository) IsEmpty(ctx context.Context) (bool, error) {
	// EXISTS rather than COUNT: this runs on every signup attempt once the
	// lockdown is on, and it only ever needs the first row.
	const q = `SELECT NOT EXISTS (SELECT 1 FROM users LIMIT 1)`
	var empty bool
	err := r.DB.QueryRow(ctx, q).Scan(&empty)
	return empty, err
}

func (r *userRepository) CountUsers(ctx context.Context) (int, error) {
	const q = `SELECT count(*) FROM users`
	var n int
	err := r.DB.QueryRow(ctx, q).Scan(&n)
	return n, err
}

func (r *userRepository) GetUndoSendSeconds(ctx context.Context, userID uuid.UUID) (int, error) {
	const q = `SELECT undo_send_seconds FROM users WHERE id = $1`
	var seconds int
	err := r.DB.QueryRow(ctx, q, userID).Scan(&seconds)
	return seconds, err
}

func (r *userRepository) SetUndoSendSeconds(ctx context.Context, userID uuid.UUID, seconds int) error {
	const q = `UPDATE users SET undo_send_seconds = $2, updated_at = NOW() WHERE id = $1`
	_, err := r.DB.Exec(ctx, q, userID, seconds)
	return err
}

func (r *userRepository) RecordSignupMetadata(ctx context.Context, userID uuid.UUID, ip, userAgent string, emailRisk int, normalizedEmail string) error {
	// An unparseable or absent address is stored as NULL rather than failing
	// the write: the rest of the evidence is still worth keeping.
	var addr any
	if parsed := net.ParseIP(strings.TrimSpace(ip)); parsed != nil {
		addr = parsed.String()
	}
	_, err := r.DB.Pool.Exec(ctx, `
		UPDATE users
		   SET signup_ip = $2::inet,
		       signup_user_agent = NULLIF($3, ''),
		       signup_email_risk = $4,
		       signup_email_normalized = NULLIF($5, '')
		 WHERE id = $1
	`, userID, addr, truncate(userAgent, 512), emailRisk, normalizedEmail)
	return err
}

// truncate bounds a client-supplied string before it reaches the database.
func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max]
}

// IsLoginCodeExempt reads the one flag the login path needs.
func (r *userRepository) IsLoginCodeExempt(ctx context.Context, id uuid.UUID) (bool, error) {
	var exempt bool
	err := r.DB.QueryRow(ctx, `SELECT login_code_exempt FROM users WHERE id = $1`, id).Scan(&exempt)
	if err == pgx.ErrNoRows {
		return false, nil
	}
	return exempt, err
}

// SetLoginCodeExempt grants or clears the exemption. Clearing wipes the
// reason with it, so a cleared row cannot be mistaken for a live exemption.
func (r *userRepository) SetLoginCodeExempt(ctx context.Context, id uuid.UUID, exempt bool, reason string, by *uuid.UUID) error {
	if !exempt {
		_, err := r.DB.Exec(ctx, `
			UPDATE users
			SET login_code_exempt = false,
			    login_code_exempt_reason = NULL,
			    login_code_exempt_by = NULL,
			    login_code_exempt_at = NULL,
			    updated_at = NOW()
			WHERE id = $1`, id)
		return err
	}
	_, err := r.DB.Exec(ctx, `
		UPDATE users
		SET login_code_exempt = true,
		    login_code_exempt_reason = $2,
		    login_code_exempt_by = $3,
		    login_code_exempt_at = NOW(),
		    updated_at = NOW()
		WHERE id = $1`, id, reason, by)
	return err
}

// ListLoginCodeExempt is ordered oldest first, because the exemption most
// likely to have been forgotten is the one that has been there longest.
func (r *userRepository) ListLoginCodeExempt(ctx context.Context) ([]models.LoginCodeExemption, error) {
	rows, err := r.DB.Query(ctx, `
		SELECT u.id, u.email, u.login_code_exempt_reason, u.login_code_exempt_at, u.password_expires_at,
		       workspace.id, seeded.created_at
		FROM users u
		LEFT JOIN LATERAL (
			SELECT o.id FROM organizations o
			WHERE o.owner_user_id = u.id AND o.category = 'test'
			ORDER BY o.created_at, o.id LIMIT 1
		) workspace ON true
		LEFT JOIN LATERAL (
			SELECT a.created_at FROM admin_audit_logs a
			WHERE a.target_type = 'organization' AND a.target_id = workspace.id
			  AND a.action = 'seed_tester_workspace'
			ORDER BY a.created_at LIMIT 1
		) seeded ON true
		WHERE u.login_code_exempt
		ORDER BY u.login_code_exempt_at NULLS FIRST`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []models.LoginCodeExemption{}
	for rows.Next() {
		var e models.LoginCodeExemption
		if err := rows.Scan(&e.UserID, &e.Email, &e.Reason, &e.GrantedAt, &e.PasswordExpiresAt, &e.TestWorkspaceID, &e.SampleDataSeededAt); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// CreateExemptUser is CreateUser plus the exemption, atomically.
//
// Creating them separately meant a failure between the two left an account
// with no exemption: invisible to the tester list, un-retryable because the
// address was taken, and reachable by whoever held the password.
func (r *userRepository) CreateExemptUser(ctx context.Context, email *mail.Address, passwordHash, reason string, by *uuid.UUID, passwordExpiresAt time.Time) (*models.User, error) {
	tx, err := r.DB.Begin(ctx)
	if err != nil {
		return nil, err
	}
	// Rolls back unless the commit below succeeds; a commit makes this a no-op.
	defer func() { _ = tx.Rollback(ctx) }()

	id := uuid.New()
	address := normalizeUserEmail(email.Address)
	firstName := displayname.FromEmail(address)
	now := time.Now()
	if _, ierr := tx.Exec(ctx, `
		INSERT INTO users (id, email, password_hash, first_name, last_name, created_at, updated_at, onboarding_completed_at)
		VALUES ($1, $2, $3, $4, '', $5, $5, $5)`,
		id, address, passwordHash, firstName, now); ierr != nil {
		return nil, ierr
	}
	created := &models.User{
		OnboardingCompletedAt: &now,
		ID:                    id,
		FirstName:             firstName,
		Email:                 address,
		Roles:                 make([]uuid.UUID, 0),
		CreatedAt:             now,
		UpdatedAt:             now,
	}
	if _, eerr := tx.Exec(ctx, `
		UPDATE users
		SET login_code_exempt = true,
		    login_code_exempt_reason = $2,
		    login_code_exempt_by = $3,
		    login_code_exempt_at = NOW(),
		    password_expires_at = $4
		WHERE id = $1`, created.ID, reason, by, passwordExpiresAt); eerr != nil {
		return nil, eerr
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return created, nil
}

// RevokeTester ends a tester's exemption and its handed-out password in one
// write. An account exempted without an expiring password keeps its password.
func (r *userRepository) RevokeTester(ctx context.Context, id uuid.UUID) (bool, error) {
	var cleared bool
	err := r.DB.QueryRow(ctx, `
		WITH ended_grants AS (
			UPDATE subscriptions s SET managed_until = now(), updated_at = now()
			FROM organizations o
			WHERE s.organization_id = o.id AND o.owner_user_id = $1
			  AND o.category = 'test' AND s.managed_plan_id = $2
		)
		UPDATE users u
		SET login_code_exempt = false,
		    login_code_exempt_reason = NULL,
		    login_code_exempt_by = NULL,
		    login_code_exempt_at = NULL,
		    password_hash = CASE WHEN old.tester THEN NULL ELSE u.password_hash END,
		    password_changed_at = CASE WHEN old.tester THEN now() ELSE u.password_changed_at END,
		    password_expires_at = NULL,
		    updated_at = now()
		FROM (SELECT id, password_expires_at IS NOT NULL AS tester FROM users WHERE id = $1 FOR UPDATE) old
		WHERE u.id = old.id
		RETURNING old.tester`, id, models.TestPlanID).Scan(&cleared)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, ErrUserNotFound
	}
	return cleared, err
}

// DeleteOrphanExemptUser removes a half-created tester.
//
// Without it the address is taken by an account that cannot be used and cannot
// be recreated, so the operator is stuck. The WHERE clause is what makes this
// safe to expose at all: an account that holds an exemption and belongs to no
// organization is one this handler made moments ago and failed to finish. A
// real user always has a workspace, so no predicate match means no delete.
func (r *userRepository) DeleteOrphanExemptUser(ctx context.Context, id uuid.UUID) error {
	_, err := r.DB.Exec(ctx, `
		DELETE FROM users
		WHERE id = $1
		  AND login_code_exempt
		  AND NOT EXISTS (SELECT 1 FROM organization_members m WHERE m.user_id = users.id)`, id)
	return err
}
