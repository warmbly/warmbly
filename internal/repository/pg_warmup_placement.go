package repository

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/infrastructure/db"
	"github.com/warmbly/warmbly/internal/models"
)

// WarmupPlacementDayRow is one sender's placement at one recipient group on one UTC day.
type WarmupPlacementDayRow struct {
	SenderID uuid.UUID
	Email    string
	Date     string
	Group    string
	Inbox    int
	Tabs     int
	Spam     int
	Rescued  int
}

// WarmupPlacementHostRow is the window's placement at one mail host.
type WarmupPlacementHostRow struct {
	Group   string
	Host    string
	Inbox   int
	Tabs    int
	Spam    int
	Rescued int
}

// WarmupSenderDayCount is a per-sender, per-UTC-day count.
type WarmupSenderDayCount struct {
	SenderID uuid.UUID
	Date     string
	Count    int
}

// WarmupPlacementRepository keeps the daily rollup of where each sender's
// warmup mail landed. Every read is scoped to one organization's senders.
type WarmupPlacementRepository interface {
	// RecordPlacement counts one verified receipt exactly once: a redelivered
	// arrival finds the receipt already placed and adds nothing.
	RecordPlacement(ctx context.Context, recipientID, internalID uuid.UUID, group, host, landed string, rescued bool) error
	Daily(ctx context.Context, orgID uuid.UUID, senderID *uuid.UUID, from, to time.Time) ([]WarmupPlacementDayRow, error)
	Hosts(ctx context.Context, orgID uuid.UUID, senderID *uuid.UUID, from, to time.Time) ([]WarmupPlacementHostRow, error)
	Sent(ctx context.Context, orgID uuid.UUID, senderID *uuid.UUID, from, to time.Time) ([]WarmupSenderDayCount, error)
	// Unconfirmed counts completed sends no recipient has reported, among
	// those sent before cutoff.
	Unconfirmed(ctx context.Context, orgID uuid.UUID, senderID *uuid.UUID, from, to, cutoff time.Time) ([]WarmupSenderDayCount, error)
	// Rates is each sender's trailing-window placement since the given day.
	Rates(ctx context.Context, orgID uuid.UUID, senderID *uuid.UUID, since time.Time) (map[uuid.UUID]models.WarmupPlacementRate, error)
}

type warmupPlacementRepository struct {
	db *db.DB
}

// NewWarmupPlacementRepository creates the placement rollup repository.
func NewWarmupPlacementRepository(d *db.DB) WarmupPlacementRepository {
	return &warmupPlacementRepository{db: d}
}

func (r *warmupPlacementRepository) RecordPlacement(ctx context.Context, recipientID, internalID uuid.UUID, group, host, landed string, rescued bool) error {
	var inbox, tabs, spam, rescue int
	switch landed {
	case models.WarmupLandedSpam:
		spam = 1
		if rescued {
			rescue = 1
		}
	case models.WarmupLandedTabs:
		tabs = 1
	default:
		inbox = 1
	}
	const query = `
		WITH claimed AS (
			UPDATE warmup_received SET placed = true
			WHERE email_account_id = $1 AND internal_id = $2 AND NOT placed
			RETURNING sender_account_id, created_at
		)
		INSERT INTO warmup_placement_daily (sender_account_id, date, recipient_group, recipient_host, inbox, tabs, spam, rescued)
		SELECT sender_account_id, (created_at AT TIME ZONE 'UTC')::date, $3, $4, $5, $6, $7, $8 FROM claimed
		ON CONFLICT (sender_account_id, date, recipient_group, recipient_host) DO UPDATE SET
			inbox = warmup_placement_daily.inbox + EXCLUDED.inbox,
			tabs = warmup_placement_daily.tabs + EXCLUDED.tabs,
			spam = warmup_placement_daily.spam + EXCLUDED.spam,
			rescued = warmup_placement_daily.rescued + EXCLUDED.rescued
	`
	params := []any{recipientID, internalID, group, host, inbox, tabs, spam, rescue}
	if _, err := r.db.Exec(ctx, query, params...); err != nil {
		db.CaptureError(err, query, params, "exec")
		return err
	}
	return nil
}

func (r *warmupPlacementRepository) Daily(ctx context.Context, orgID uuid.UUID, senderID *uuid.UUID, from, to time.Time) ([]WarmupPlacementDayRow, error) {
	const query = `
		SELECT p.sender_account_id, ea.email, p.date::text, p.recipient_group,
			SUM(p.inbox)::int, SUM(p.tabs)::int, SUM(p.spam)::int, SUM(p.rescued)::int
		FROM warmup_placement_daily p
		JOIN email_accounts ea ON ea.id = p.sender_account_id
		WHERE ea.organization_id = $1
		  AND p.date >= $2 AND p.date <= $3
		  AND ($4::uuid IS NULL OR p.sender_account_id = $4)
		GROUP BY 1, 2, 3, 4
	`
	params := []any{orgID, from, to, senderID}
	rows, err := r.db.Query(ctx, query, params...)
	if err != nil {
		db.CaptureError(err, query, params, "query")
		return nil, err
	}
	defer rows.Close()
	out := make([]WarmupPlacementDayRow, 0)
	for rows.Next() {
		var d WarmupPlacementDayRow
		if err := rows.Scan(&d.SenderID, &d.Email, &d.Date, &d.Group, &d.Inbox, &d.Tabs, &d.Spam, &d.Rescued); err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

func (r *warmupPlacementRepository) Hosts(ctx context.Context, orgID uuid.UUID, senderID *uuid.UUID, from, to time.Time) ([]WarmupPlacementHostRow, error) {
	const query = `
		SELECT p.recipient_group, p.recipient_host,
			SUM(p.inbox)::int, SUM(p.tabs)::int, SUM(p.spam)::int, SUM(p.rescued)::int
		FROM warmup_placement_daily p
		JOIN email_accounts ea ON ea.id = p.sender_account_id
		WHERE ea.organization_id = $1
		  AND p.date >= $2 AND p.date <= $3
		  AND ($4::uuid IS NULL OR p.sender_account_id = $4)
		GROUP BY 1, 2
	`
	params := []any{orgID, from, to, senderID}
	rows, err := r.db.Query(ctx, query, params...)
	if err != nil {
		db.CaptureError(err, query, params, "query")
		return nil, err
	}
	defer rows.Close()
	out := make([]WarmupPlacementHostRow, 0)
	for rows.Next() {
		var h WarmupPlacementHostRow
		if err := rows.Scan(&h.Group, &h.Host, &h.Inbox, &h.Tabs, &h.Spam, &h.Rescued); err != nil {
			return nil, err
		}
		out = append(out, h)
	}
	return out, rows.Err()
}

func (r *warmupPlacementRepository) Sent(ctx context.Context, orgID uuid.UUID, senderID *uuid.UUID, from, to time.Time) ([]WarmupSenderDayCount, error) {
	const query = `
		SELECT ws.email_account_id, ws.date::text, SUM(ws.emails_sent)::int
		FROM warmup_statistics ws
		JOIN email_accounts ea ON ea.id = ws.email_account_id
		WHERE ea.organization_id = $1
		  AND ws.date >= $2 AND ws.date <= $3
		  AND ($4::uuid IS NULL OR ws.email_account_id = $4)
		GROUP BY 1, 2
	`
	return r.senderDayCounts(ctx, query, []any{orgID, from, to, senderID})
}

func (r *warmupPlacementRepository) Unconfirmed(ctx context.Context, orgID uuid.UUID, senderID *uuid.UUID, from, to, cutoff time.Time) ([]WarmupSenderDayCount, error) {
	// A token is written before the send, so only completed sends count.
	const query = `
		SELECT wt.sender_account_id, (wt.created_at AT TIME ZONE 'UTC')::date::text, COUNT(*)::int
		FROM warmup_tokens wt
		JOIN email_accounts ea ON ea.id = wt.sender_account_id
		JOIN tasks t ON t.id = wt.task_id AND t.status = 'completed'
		WHERE ea.organization_id = $1
		  AND wt.created_at >= $2::timestamptz
		  AND wt.created_at < ($3::timestamptz + interval '1 day')
		  AND wt.created_at < $5
		  AND wt.consumed_at IS NULL
		  AND ($4::uuid IS NULL OR wt.sender_account_id = $4)
		GROUP BY 1, 2
	`
	return r.senderDayCounts(ctx, query, []any{orgID, from, to, senderID, cutoff})
}

func (r *warmupPlacementRepository) senderDayCounts(ctx context.Context, query string, params []any) ([]WarmupSenderDayCount, error) {
	rows, err := r.db.Query(ctx, query, params...)
	if err != nil {
		db.CaptureError(err, query, params, "query")
		return nil, err
	}
	defer rows.Close()
	out := make([]WarmupSenderDayCount, 0)
	for rows.Next() {
		var c WarmupSenderDayCount
		if err := rows.Scan(&c.SenderID, &c.Date, &c.Count); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

func (r *warmupPlacementRepository) Rates(ctx context.Context, orgID uuid.UUID, senderID *uuid.UUID, since time.Time) (map[uuid.UUID]models.WarmupPlacementRate, error) {
	const query = `
		SELECT p.sender_account_id, SUM(p.inbox)::int, SUM(p.tabs)::int, SUM(p.spam)::int
		FROM warmup_placement_daily p
		JOIN email_accounts ea ON ea.id = p.sender_account_id
		WHERE ea.organization_id = $1
		  AND p.date >= $2
		  AND ($3::uuid IS NULL OR p.sender_account_id = $3)
		GROUP BY 1
	`
	params := []any{orgID, since, senderID}
	rows, err := r.db.Query(ctx, query, params...)
	if err != nil {
		db.CaptureError(err, query, params, "query")
		return nil, err
	}
	defer rows.Close()
	out := make(map[uuid.UUID]models.WarmupPlacementRate)
	for rows.Next() {
		var id uuid.UUID
		var inbox, tabs, spam int
		if err := rows.Scan(&id, &inbox, &tabs, &spam); err != nil {
			return nil, err
		}
		out[id] = models.NewWarmupPlacementRate(inbox, tabs, spam)
	}
	return out, rows.Err()
}
