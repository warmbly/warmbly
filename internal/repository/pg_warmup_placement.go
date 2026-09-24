package repository

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
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
	Email    string
	Date     string
	Count    int
}

// WarmupPlacementSender is the sending mailbox a counted placement belongs to.
type WarmupPlacementSender struct {
	ID     uuid.UUID
	OrgID  *uuid.UUID
	UserID string
	Email  string
}

// WarmupPlacementRepository keeps the daily rollup of where each sender's
// warmup mail landed. Every read is scoped to one organization's senders.
type WarmupPlacementRepository interface {
	// RecordPlacement counts one verified receipt exactly once and returns its
	// sender; nil when the receipt was already counted or does not exist.
	RecordPlacement(ctx context.Context, recipientID, internalID uuid.UUID, group, host, landed string, rescued bool) (*WarmupPlacementSender, error)
	// SweepUnplaced counts up to limit receipts written before cutoff that no
	// live arrival counted, reading spam from the spam reports. It returns how
	// many it claimed.
	SweepUnplaced(ctx context.Context, cutoff time.Time, limit int) (int, error)
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

func (r *warmupPlacementRepository) RecordPlacement(ctx context.Context, recipientID, internalID uuid.UUID, group, host, landed string, rescued bool) (*WarmupPlacementSender, error) {
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
		), counted AS (
			INSERT INTO warmup_placement_daily (sender_account_id, date, recipient_group, recipient_host, inbox, tabs, spam, rescued)
			SELECT sender_account_id, (created_at AT TIME ZONE 'UTC')::date, $3, $4, $5, $6, $7, $8 FROM claimed
			ON CONFLICT (sender_account_id, date, recipient_group, recipient_host) DO UPDATE SET
				inbox = warmup_placement_daily.inbox + EXCLUDED.inbox,
				tabs = warmup_placement_daily.tabs + EXCLUDED.tabs,
				spam = warmup_placement_daily.spam + EXCLUDED.spam,
				rescued = warmup_placement_daily.rescued + EXCLUDED.rescued
			RETURNING sender_account_id
		)
		SELECT ea.id, ea.organization_id, ea.user_id::text, ea.email
		FROM counted JOIN email_accounts ea ON ea.id = counted.sender_account_id
	`
	params := []any{recipientID, internalID, group, host, inbox, tabs, spam, rescue}
	var sender WarmupPlacementSender
	err := r.db.QueryRow(ctx, query, params...).Scan(&sender.ID, &sender.OrgID, &sender.UserID, &sender.Email)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		db.CaptureError(err, query, params, "query")
		return nil, err
	}
	return &sender, nil
}

func (r *warmupPlacementRepository) SweepUnplaced(ctx context.Context, cutoff time.Time, limit int) (int, error) {
	// The group CASE mirrors models.WarmupRecipientGroup. Category tabs and
	// rescues are only known on the live path, so a swept receipt reads as
	// inbox or spam.
	const query = `
		WITH batch AS (
			SELECT email_account_id, internal_id FROM warmup_received
			WHERE NOT placed AND created_at < $1
			ORDER BY created_at
			LIMIT $2
			FOR UPDATE SKIP LOCKED
		), claimed AS (
			UPDATE warmup_received wr SET placed = true
			FROM batch b
			WHERE wr.email_account_id = b.email_account_id AND wr.internal_id = b.internal_id AND NOT wr.placed
			RETURNING wr.email_account_id, wr.message_id, wr.sender_account_id, wr.created_at
		), graded AS (
			SELECT c.sender_account_id,
				(c.created_at AT TIME ZONE 'UTC')::date AS day,
				CASE
					WHEN rec.mail_host IN ('google_workspace', 'gmail') THEN 'google'
					WHEN rec.mail_host IN ('microsoft365', 'outlook') THEN 'microsoft'
					WHEN rec.mail_host IN ('yahoo', 'aol') THEN 'yahoo'
					WHEN rec.mail_host = '' AND rec.provider = 'gmail' THEN 'google'
					WHEN rec.mail_host = '' AND rec.provider = 'outlook' THEN 'microsoft'
					ELSE 'other'
				END AS grp,
				rec.mail_host AS host,
				EXISTS (
					SELECT 1 FROM warmup_spam_reports sr
					WHERE sr.reporter_account_id = c.email_account_id AND sr.message_id = c.message_id
					  AND sr.report_type = 'spam_placement' AND c.message_id <> ''
				) AS spam
			FROM claimed c
			JOIN email_accounts rec ON rec.id = c.email_account_id
			JOIN email_accounts snd ON snd.id = c.sender_account_id
		), counted AS (
			INSERT INTO warmup_placement_daily (sender_account_id, date, recipient_group, recipient_host, inbox, spam)
			SELECT sender_account_id, day, grp, host, COUNT(*) FILTER (WHERE NOT spam), COUNT(*) FILTER (WHERE spam)
			FROM graded
			GROUP BY 1, 2, 3, 4
			ON CONFLICT (sender_account_id, date, recipient_group, recipient_host) DO UPDATE SET
				inbox = warmup_placement_daily.inbox + EXCLUDED.inbox,
				spam = warmup_placement_daily.spam + EXCLUDED.spam
			RETURNING 1
		)
		SELECT (SELECT COUNT(*) FROM claimed)::int, (SELECT COUNT(*) FROM counted)::int
	`
	params := []any{cutoff, limit}
	var claimed, groups int
	if err := r.db.QueryRow(ctx, query, params...).Scan(&claimed, &groups); err != nil {
		db.CaptureError(err, query, params, "query")
		return 0, err
	}
	return claimed, nil
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
		SELECT ws.email_account_id, ea.email, ws.date::text, SUM(ws.emails_sent)::int
		FROM warmup_statistics ws
		JOIN email_accounts ea ON ea.id = ws.email_account_id
		WHERE ea.organization_id = $1
		  AND ws.date >= $2 AND ws.date <= $3
		  AND ($4::uuid IS NULL OR ws.email_account_id = $4)
		GROUP BY 1, 2, 3
	`
	return r.senderDayCounts(ctx, query, []any{orgID, from, to, senderID})
}

func (r *warmupPlacementRepository) Unconfirmed(ctx context.Context, orgID uuid.UUID, senderID *uuid.UUID, from, to, cutoff time.Time) ([]WarmupSenderDayCount, error) {
	// A token is written before the send, so only completed sends count.
	const query = `
		SELECT wt.sender_account_id, ea.email, (wt.created_at AT TIME ZONE 'UTC')::date::text, COUNT(*)::int
		FROM warmup_tokens wt
		JOIN email_accounts ea ON ea.id = wt.sender_account_id
		JOIN tasks t ON t.id = wt.task_id AND t.status = 'completed'
		WHERE ea.organization_id = $1
		  AND wt.created_at >= $2::timestamptz
		  AND wt.created_at < ($3::timestamptz + interval '1 day')
		  AND wt.created_at < $5
		  AND wt.consumed_at IS NULL
		  AND ($4::uuid IS NULL OR wt.sender_account_id = $4)
		GROUP BY 1, 2, 3
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
		if err := rows.Scan(&c.SenderID, &c.Email, &c.Date, &c.Count); err != nil {
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
