package repository

import (
	"context"
	"encoding/json"
	"errors"

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
	err := r.db.QueryRow(ctx, `
		INSERT INTO notifications (id, user_id, organization_id, category, title, body, link, metadata)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		RETURNING created_at`,
		n.ID, n.UserID, n.OrganizationID, n.Category, n.Title, n.Body, n.Link, meta).Scan(&n.CreatedAt)
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

func (r *notificationRepository) MarkRead(ctx context.Context, userID, notifID uuid.UUID) error {
	_, err := r.db.Exec(ctx, `UPDATE notifications SET read_at = now() WHERE id = $1 AND user_id = $2 AND read_at IS NULL`, notifID, userID)
	return err
}

func (r *notificationRepository) MarkAllRead(ctx context.Context, userID uuid.UUID) error {
	_, err := r.db.Exec(ctx, `UPDATE notifications SET read_at = now() WHERE user_id = $1 AND read_at IS NULL`, userID)
	return err
}
