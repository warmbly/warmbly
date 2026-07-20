package repository

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/warmbly/warmbly/internal/models"
)

type NotificationRepository interface {
	GetPreferences(ctx context.Context, userID uuid.UUID) (*models.NotificationPreferences, error)
	UpdatePreferences(ctx context.Context, userID uuid.UUID, prefs *models.NotificationPreferences) error
	Create(ctx context.Context, n *models.Notification) (*models.Notification, error)
	List(ctx context.Context, userID uuid.UUID, limit int, unreadOnly bool) ([]models.Notification, error)
	CountUnread(ctx context.Context, userID uuid.UUID) (int, error)
	MarkRead(ctx context.Context, userID, notifID uuid.UUID) error
	MarkAllRead(ctx context.Context, userID uuid.UUID) error

	// ClaimDueEmails atomically claims every pending email row belonging to a
	// user with at least one due row (so all their pending mail bundles into
	// one digest) or sharing an org group with a due row (so a shared event
	// coalesces into one email even when its recipients digest on different
	// cadences). Safe across replicas: the claim is an UPDATE guarded by
	// FOR UPDATE SKIP LOCKED, and stale claims from crashed flushes recover
	// back to pending first.
	ClaimDueEmails(ctx context.Context) ([]models.Notification, error)
	// MarkEmailed settles claimed rows as sent.
	MarkEmailed(ctx context.Context, ids []uuid.UUID) error
	// RequeueEmails returns claimed rows to pending after a send failure,
	// with a retry delay; rows that already burned their attempts are
	// dropped to skipped instead of retrying forever.
	RequeueEmails(ctx context.Context, ids []uuid.UUID, delay time.Duration, maxAttempts int) error
}

type notificationRepository struct {
	db *pgxpool.Pool
}

func NewNotificationRepository(db *pgxpool.Pool) NotificationRepository {
	return &notificationRepository{db: db}
}

// GetPreferences returns the user's prefs merged over the defaults (stored keys
// override defaults; absent keys keep their default — forward-compatible).
func (r *notificationRepository) GetPreferences(ctx context.Context, userID uuid.UUID) (*models.NotificationPreferences, error) {
	prefs := models.DefaultNotificationPreferences()
	var raw []byte
	err := r.db.QueryRow(ctx, `SELECT notification_preferences FROM users WHERE id = $1`, userID).Scan(&raw)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return &prefs, nil
		}
		return nil, err
	}
	if len(raw) > 0 {
		_ = json.Unmarshal(raw, &prefs)
	}
	if !models.ValidEmailDigest(prefs.EmailDigest) {
		prefs.EmailDigest = models.EmailDigestSmart
	}
	return &prefs, nil
}

func (r *notificationRepository) UpdatePreferences(ctx context.Context, userID uuid.UUID, prefs *models.NotificationPreferences) error {
	raw, err := json.Marshal(prefs)
	if err != nil {
		return err
	}
	_, err = r.db.Exec(ctx, `UPDATE users SET notification_preferences = $2 WHERE id = $1`, userID, raw)
	return err
}

func (r *notificationRepository) Create(ctx context.Context, n *models.Notification) (*models.Notification, error) {
	if n.ID == uuid.Nil {
		n.ID = uuid.New()
	}
	meta, _ := json.Marshal(n.Metadata)
	if len(meta) == 0 {
		meta = []byte("{}")
	}
	if n.EmailState == "" {
		n.EmailState = "none"
	}
	var groupKey *string
	if n.GroupKey != "" {
		groupKey = &n.GroupKey
	}
	err := r.db.QueryRow(ctx, `
		INSERT INTO notifications (id, user_id, organization_id, category, title, body, link, metadata,
			group_key, email_state, email_due_at, read_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, CASE WHEN $12 THEN now() END)
		RETURNING created_at`,
		n.ID, n.UserID, n.OrganizationID, n.Category, n.Title, n.Body, n.Link, meta,
		groupKey, n.EmailState, n.EmailDueAt, n.PreRead).Scan(&n.CreatedAt)
	if err != nil {
		return nil, err
	}
	return n, nil
}

func (r *notificationRepository) List(ctx context.Context, userID uuid.UUID, limit int, unreadOnly bool) ([]models.Notification, error) {
	if limit <= 0 || limit > 100 {
		limit = 50
	}
	q := `SELECT id, user_id, organization_id, category, title, body, link, metadata, read_at, created_at
		FROM notifications WHERE user_id = $1`
	if unreadOnly {
		q += ` AND read_at IS NULL`
	}
	q += ` ORDER BY created_at DESC LIMIT $2`
	rows, err := r.db.Query(ctx, q, userID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []models.Notification{}
	for rows.Next() {
		var n models.Notification
		var meta []byte
		if err := rows.Scan(&n.ID, &n.UserID, &n.OrganizationID, &n.Category, &n.Title, &n.Body, &n.Link, &meta, &n.ReadAt, &n.CreatedAt); err != nil {
			return nil, err
		}
		if len(meta) > 0 {
			_ = json.Unmarshal(meta, &n.Metadata)
		}
		out = append(out, n)
	}
	return out, rows.Err()
}

func (r *notificationRepository) CountUnread(ctx context.Context, userID uuid.UUID) (int, error) {
	var c int
	err := r.db.QueryRow(ctx, `SELECT COUNT(*) FROM notifications WHERE user_id = $1 AND read_at IS NULL`, userID).Scan(&c)
	return c, err
}

// MarkRead also cancels the row's pending email: a notification the user has
// already seen in-app must never email them later.
func (r *notificationRepository) MarkRead(ctx context.Context, userID, notifID uuid.UUID) error {
	_, err := r.db.Exec(ctx, `
		UPDATE notifications
		SET read_at = now(),
			email_state = CASE WHEN email_state = 'pending' THEN 'skipped' ELSE email_state END
		WHERE id = $1 AND user_id = $2 AND read_at IS NULL`, notifID, userID)
	return err
}

func (r *notificationRepository) MarkAllRead(ctx context.Context, userID uuid.UUID) error {
	_, err := r.db.Exec(ctx, `
		UPDATE notifications
		SET read_at = now(),
			email_state = CASE WHEN email_state = 'pending' THEN 'skipped' ELSE email_state END
		WHERE user_id = $1 AND read_at IS NULL`, userID)
	return err
}

// ClaimDueEmails flips claimable rows to 'sending' and returns them. While a
// row is 'sending', email_due_at doubles as the claim timestamp so crashed
// claims can be recovered back to pending.
func (r *notificationRepository) ClaimDueEmails(ctx context.Context) ([]models.Notification, error) {
	_, _ = r.db.Exec(ctx, `
		UPDATE notifications SET email_state = 'pending', email_due_at = now()
		WHERE email_state = 'sending' AND email_due_at < now() - interval '10 minutes'`)

	rows, err := r.db.Query(ctx, `
		UPDATE notifications SET email_state = 'sending', email_due_at = now()
		WHERE id IN (
			SELECT n.id FROM notifications n
			WHERE n.email_state = 'pending' AND (
				n.user_id IN (
					SELECT user_id FROM notifications
					WHERE email_state = 'pending' AND email_due_at <= now())
				OR (n.group_key IS NOT NULL AND n.organization_id IS NOT NULL AND (n.organization_id, n.group_key) IN (
					SELECT organization_id, group_key FROM notifications
					WHERE email_state = 'pending' AND email_due_at <= now()
						AND group_key IS NOT NULL AND organization_id IS NOT NULL))
			)
			FOR UPDATE SKIP LOCKED
		)
		RETURNING id, user_id, organization_id, category, title, body, link,
			COALESCE(group_key, ''), email_attempts, created_at`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []models.Notification{}
	for rows.Next() {
		var n models.Notification
		if err := rows.Scan(&n.ID, &n.UserID, &n.OrganizationID, &n.Category, &n.Title, &n.Body,
			&n.Link, &n.GroupKey, &n.EmailAttempts, &n.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, n)
	}
	return out, rows.Err()
}

func (r *notificationRepository) MarkEmailed(ctx context.Context, ids []uuid.UUID) error {
	if len(ids) == 0 {
		return nil
	}
	_, err := r.db.Exec(ctx, `
		UPDATE notifications SET email_state = 'sent', email_due_at = NULL
		WHERE id = ANY($1) AND email_state = 'sending'`, ids)
	return err
}

func (r *notificationRepository) RequeueEmails(ctx context.Context, ids []uuid.UUID, delay time.Duration, maxAttempts int) error {
	if len(ids) == 0 {
		return nil
	}
	_, err := r.db.Exec(ctx, `
		UPDATE notifications SET
			email_attempts = email_attempts + 1,
			email_state = CASE WHEN email_attempts + 1 >= $3 THEN 'skipped' ELSE 'pending' END,
			email_due_at = CASE WHEN email_attempts + 1 >= $3 THEN NULL ELSE now() + make_interval(secs => $2) END
		WHERE id = ANY($1) AND email_state = 'sending'`, ids, delay.Seconds(), maxAttempts)
	return err
}
