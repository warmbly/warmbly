package repository

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/warmbly/warmbly/internal/infrastructure/db"
	"github.com/warmbly/warmbly/internal/models"
)

// WarmupPlacementDayRow is one sender's placement at one recipient group on one UTC day.
type WarmupPlacementDayRow = models.WarmupPlacementDayRow

// WarmupPlacementHostRow is the window's placement at one mail host.
type WarmupPlacementHostRow = models.WarmupPlacementHostRow

// WarmupSenderDayCount is a per-sender, per-UTC-day count.
type WarmupSenderDayCount = models.WarmupSenderDayCount

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
	Rates(ctx context.Context, orgID uuid.UUID, senderID *uuid.UUID, since time.Time) (map[uuid.UUID]models.WarmupPlacementWindow, error)
	ForAccounts(ctx context.Context, orgID uuid.UUID, senderIDs []uuid.UUID, from, to time.Time) (*models.WarmupPlacementData, error)
}

type warmupPlacementRepository struct {
	db *db.DB
}

// NewWarmupPlacementRepository creates the placement rollup repository.
func NewWarmupPlacementRepository(d *db.DB) WarmupPlacementRepository {
	return &warmupPlacementRepository{db: d}
}

func (r *warmupPlacementRepository) RecordPlacement(ctx context.Context, recipientID, internalID uuid.UUID, group, host, landed string, rescued bool) (*WarmupPlacementSender, error) {
	const query = `
		WITH claimed AS (
			UPDATE warmup_received SET placed = true
			WHERE email_account_id = $1 AND internal_id = $2 AND NOT placed
			RETURNING sender_account_id, created_at, first_landing
		), counted AS (
			INSERT INTO warmup_placement_daily (sender_account_id, date, recipient_group, recipient_host, inbox, tabs, spam, rescued, unknown, archived, custom, instrumented)
			SELECT sender_account_id, (created_at AT TIME ZONE 'UTC')::date, $3, $4,
				(COALESCE(first_landing,$5) = 'inbox')::int,
				(COALESCE(first_landing,$5) = 'tabs')::int,
				(COALESCE(first_landing,$5) = 'spam')::int,
				(COALESCE(first_landing,$5) = 'spam' AND $6)::int,
				(COALESCE(first_landing,$5) NOT IN ('inbox','tabs','spam','archive','custom'))::int,
				(COALESCE(first_landing,$5) = 'archive')::int,
				(COALESCE(first_landing,$5) = 'custom')::int,
				(first_landing IS NOT NULL)::int FROM claimed
			ON CONFLICT (sender_account_id, date, recipient_group, recipient_host) DO UPDATE SET
				inbox = warmup_placement_daily.inbox + EXCLUDED.inbox,
				tabs = warmup_placement_daily.tabs + EXCLUDED.tabs,
				spam = warmup_placement_daily.spam + EXCLUDED.spam,
				rescued = warmup_placement_daily.rescued + EXCLUDED.rescued,
				unknown = warmup_placement_daily.unknown + EXCLUDED.unknown,
				archived = warmup_placement_daily.archived + EXCLUDED.archived,
				custom = warmup_placement_daily.custom + EXCLUDED.custom,
				instrumented = warmup_placement_daily.instrumented + EXCLUDED.instrumented
			RETURNING sender_account_id
		)
		SELECT ea.id, ea.organization_id, ea.user_id::text, ea.email
		FROM counted JOIN email_accounts ea ON ea.id = counted.sender_account_id
	`
	params := []any{recipientID, internalID, group, host, landed, rescued}
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

func (r *warmupPlacementRepository) RecordReceiptObservation(ctx context.Context, recipientID, internalID uuid.UUID, folder string, flags []string, at time.Time) error {
	evidence, err := json.Marshal(models.EvidenceFromFlags(flags))
	if err != nil {
		return err
	}
	_, err = r.db.Exec(ctx, `UPDATE warmup_received SET first_landing=$3, first_folder=$4, first_flags=$5, observed_at=$6, evidence=$7 WHERE email_account_id=$1 AND internal_id=$2 AND first_landing IS NULL AND NOT placed`, recipientID, internalID, models.ClassifyWarmupLanding(folder, flags), folder, models.PlacementObservationFlags(flags), at, evidence)
	return err
}

func (r *warmupPlacementRepository) SweepUnplaced(ctx context.Context, cutoff time.Time, limit int) (int, error) {
	// Recovery uses first observations; legacy receipts without them remain unknown.
	query := `
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
			RETURNING wr.email_account_id, wr.message_id, wr.sender_account_id, wr.created_at, wr.first_landing
		), graded AS (
			SELECT c.sender_account_id,
				(c.created_at AT TIME ZONE 'UTC')::date AS day,
				` + recipientGroupSQL("rec") + ` AS grp,
				rec.mail_host AS host,
				EXISTS (
					SELECT 1 FROM warmup_spam_reports sr
					WHERE sr.reporter_account_id = c.email_account_id AND sr.message_id = c.message_id
					  AND sr.report_type = 'spam_placement' AND c.message_id <> ''
				) AS spam,
				c.first_landing
			FROM claimed c
			JOIN email_accounts rec ON rec.id = c.email_account_id
			JOIN email_accounts snd ON snd.id = c.sender_account_id
		), counted AS (
			INSERT INTO warmup_placement_daily (sender_account_id, date, recipient_group, recipient_host, inbox, tabs, spam, unknown, archived, custom, instrumented)
			SELECT sender_account_id, day, grp, host,
				COUNT(*) FILTER (WHERE first_landing='inbox'),
				COUNT(*) FILTER (WHERE first_landing='tabs'),
				COUNT(*) FILTER (WHERE first_landing='spam' OR (first_landing IS NULL AND spam)),
				COUNT(*) FILTER (WHERE first_landing='unknown' OR (first_landing IS NULL AND NOT spam)),
				COUNT(*) FILTER (WHERE first_landing='archive'),
				COUNT(*) FILTER (WHERE first_landing='custom'),
				COUNT(*) FILTER (WHERE first_landing IS NOT NULL)
			FROM graded
			GROUP BY 1, 2, 3, 4
			ON CONFLICT (sender_account_id, date, recipient_group, recipient_host) DO UPDATE SET
				inbox = warmup_placement_daily.inbox + EXCLUDED.inbox,
				spam = warmup_placement_daily.spam + EXCLUDED.spam,
				tabs = warmup_placement_daily.tabs + EXCLUDED.tabs,
				unknown = warmup_placement_daily.unknown + EXCLUDED.unknown,
				archived = warmup_placement_daily.archived + EXCLUDED.archived,
				custom = warmup_placement_daily.custom + EXCLUDED.custom,
				instrumented = warmup_placement_daily.instrumented + EXCLUDED.instrumented
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
	return r.daily(ctx, orgID, singleWarmupAccount(senderID), from, to)
}

func (r *warmupPlacementRepository) daily(ctx context.Context, orgID uuid.UUID, senderIDs []uuid.UUID, from, to time.Time) ([]WarmupPlacementDayRow, error) {
	const query = `
		SELECT p.sender_account_id, ea.email, p.date::text, p.recipient_group,
			SUM(p.inbox)::int, SUM(p.tabs)::int, SUM(p.spam)::int, SUM(p.rescued)::int,
			SUM(p.unknown)::int, SUM(p.archived)::int, SUM(p.custom)::int, SUM(p.instrumented)::int
		FROM warmup_placement_daily p
		JOIN email_accounts ea ON ea.id = p.sender_account_id
		WHERE ea.organization_id = $1
		  AND p.date >= $2 AND p.date <= $3
		  AND ($4::uuid[] IS NULL OR p.sender_account_id = ANY($4))
		GROUP BY 1, 2, 3, 4
	`
	params := []any{orgID, from, to, senderIDs}
	rows, err := r.db.Query(ctx, query, params...)
	if err != nil {
		db.CaptureError(err, query, params, "query")
		return nil, err
	}
	defer rows.Close()
	out := make([]WarmupPlacementDayRow, 0)
	for rows.Next() {
		var d WarmupPlacementDayRow
		if err := rows.Scan(&d.SenderID, &d.Email, &d.Date, &d.Group, &d.Inbox, &d.Tabs, &d.Spam, &d.Rescued, &d.Unknown, &d.Archived, &d.Custom, &d.Instrumented); err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

func (r *warmupPlacementRepository) Hosts(ctx context.Context, orgID uuid.UUID, senderID *uuid.UUID, from, to time.Time) ([]WarmupPlacementHostRow, error) {
	return r.hosts(ctx, orgID, singleWarmupAccount(senderID), from, to)
}

func (r *warmupPlacementRepository) hosts(ctx context.Context, orgID uuid.UUID, senderIDs []uuid.UUID, from, to time.Time) ([]WarmupPlacementHostRow, error) {
	const query = `
		SELECT p.recipient_group, p.recipient_host,
			SUM(p.inbox)::int, SUM(p.tabs)::int, SUM(p.spam)::int, SUM(p.rescued)::int,
			SUM(p.unknown)::int, SUM(p.archived)::int, SUM(p.custom)::int, SUM(p.instrumented)::int
		FROM warmup_placement_daily p
		JOIN email_accounts ea ON ea.id = p.sender_account_id
		WHERE ea.organization_id = $1
		  AND p.date >= $2 AND p.date <= $3
		  AND ($4::uuid[] IS NULL OR p.sender_account_id = ANY($4))
		GROUP BY 1, 2
	`
	params := []any{orgID, from, to, senderIDs}
	rows, err := r.db.Query(ctx, query, params...)
	if err != nil {
		db.CaptureError(err, query, params, "query")
		return nil, err
	}
	defer rows.Close()
	out := make([]WarmupPlacementHostRow, 0)
	for rows.Next() {
		var h WarmupPlacementHostRow
		if err := rows.Scan(&h.Group, &h.Host, &h.Inbox, &h.Tabs, &h.Spam, &h.Rescued, &h.Unknown, &h.Archived, &h.Custom, &h.Instrumented); err != nil {
			return nil, err
		}
		out = append(out, h)
	}
	return out, rows.Err()
}

func (r *warmupPlacementRepository) Sent(ctx context.Context, orgID uuid.UUID, senderID *uuid.UUID, from, to time.Time) ([]WarmupSenderDayCount, error) {
	return r.sent(ctx, orgID, singleWarmupAccount(senderID), from, to)
}

func (r *warmupPlacementRepository) sent(ctx context.Context, orgID uuid.UUID, senderIDs []uuid.UUID, from, to time.Time) ([]WarmupSenderDayCount, error) {
	const query = `
		SELECT ws.email_account_id, ea.email, ws.date::text, SUM(ws.emails_sent)::int
		FROM warmup_statistics ws
		JOIN email_accounts ea ON ea.id = ws.email_account_id
		WHERE ea.organization_id = $1
		  AND ws.date >= $2 AND ws.date <= $3
		  AND ($4::uuid[] IS NULL OR ws.email_account_id = ANY($4))
		GROUP BY 1, 2, 3
	`
	return r.senderDayCounts(ctx, query, []any{orgID, from, to, senderIDs})
}

func (r *warmupPlacementRepository) Unconfirmed(ctx context.Context, orgID uuid.UUID, senderID *uuid.UUID, from, to, cutoff time.Time) ([]WarmupSenderDayCount, error) {
	return r.unconfirmed(ctx, orgID, singleWarmupAccount(senderID), from, to, cutoff)
}

func (r *warmupPlacementRepository) unconfirmed(ctx context.Context, orgID uuid.UUID, senderIDs []uuid.UUID, from, to, cutoff time.Time) ([]WarmupSenderDayCount, error) {
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
		  AND ($4::uuid[] IS NULL OR wt.sender_account_id = ANY($4))
		GROUP BY 1, 2, 3
	`
	return r.senderDayCounts(ctx, query, []any{orgID, from, to, senderIDs, cutoff})
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

func (r *warmupPlacementRepository) Rates(ctx context.Context, orgID uuid.UUID, senderID *uuid.UUID, since time.Time) (map[uuid.UUID]models.WarmupPlacementWindow, error) {
	return r.rates(ctx, orgID, singleWarmupAccount(senderID), since)
}

func (r *warmupPlacementRepository) rates(ctx context.Context, orgID uuid.UUID, senderIDs []uuid.UUID, since time.Time) (map[uuid.UUID]models.WarmupPlacementWindow, error) {
	const query = `
		SELECT p.sender_account_id,
			SUM(p.inbox) FILTER (WHERE p.recipient_group <> 'other')::int,
			SUM(p.tabs) FILTER (WHERE p.recipient_group <> 'other')::int,
			SUM(p.spam) FILTER (WHERE p.recipient_group <> 'other')::int,
			SUM(p.inbox)::int, SUM(p.tabs)::int, SUM(p.spam)::int
		FROM warmup_placement_daily p
		JOIN email_accounts ea ON ea.id = p.sender_account_id
		WHERE ea.organization_id = $1
		  AND p.date >= $2
		  AND ($3::uuid[] IS NULL OR p.sender_account_id = ANY($3))
		GROUP BY 1
	`
	params := []any{orgID, since, senderIDs}
	rows, err := r.db.Query(ctx, query, params...)
	if err != nil {
		db.CaptureError(err, query, params, "query")
		return nil, err
	}
	defer rows.Close()
	out := make(map[uuid.UUID]models.WarmupPlacementWindow)
	for rows.Next() {
		var id uuid.UUID
		var major [3]*int
		var w models.WarmupPlacementWindow
		if err := rows.Scan(&id, &major[0], &major[1], &major[2], &w.All.Inbox, &w.All.Tabs, &w.All.Spam); err != nil {
			return nil, err
		}
		// A sum over no major rows is NULL.
		for i, dst := range []*int{&w.Major.Inbox, &w.Major.Tabs, &w.Major.Spam} {
			if major[i] != nil {
				*dst = *major[i]
			}
		}
		out[id] = w
	}
	return out, rows.Err()
}
