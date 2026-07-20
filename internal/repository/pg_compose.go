package repository

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/infrastructure/db"
)

// ComposeAffinity summarizes one mailbox's history with a recipient address:
// how many messages have been exchanged and when the last one happened.
type ComposeAffinity struct {
	Messages int
	LastAt   *time.Time
}

// ComposeDraft is an autosaved, per-user working copy of an unsent email
// from the compose window.
type ComposeDraft struct {
	ID             uuid.UUID  `json:"id"`
	EmailAccountID *uuid.UUID `json:"email_account_id,omitempty"`
	To             []string   `json:"to"`
	CC             []string   `json:"cc"`
	BCC            []string   `json:"bcc"`
	Subject        string     `json:"subject"`
	Body           string     `json:"body"`
	UpdatedAt      time.Time  `json:"updated_at"`
	CreatedAt      time.Time  `json:"created_at"`
}

// ComposeRepository backs the compose mailbox picker: which of the org's
// mailboxes already talk to a recipient, and how much of each mailbox's
// daily budget is spent. It also owns the per-user compose drafts.
type ComposeRepository interface {
	// AddressHistoryByAccount returns, per org mailbox, the message count and
	// most recent timestamp of traffic with the address. It merges synced
	// unibox conversations (both directions) with queued/sent email tasks so
	// a compose sent minutes ago already counts before the Sent folder syncs.
	AddressHistoryByAccount(ctx context.Context, orgID uuid.UUID, address string) (map[uuid.UUID]ComposeAffinity, error)
	// TodaySentCounts returns today's campaign-send count per account
	// (accounts with no sends today are absent from the map).
	TodaySentCounts(ctx context.Context, accountIDs []uuid.UUID) (map[uuid.UUID]int, error)

	// UpsertDraft creates or overwrites the draft (id is client-generated so
	// autosave is idempotent). Ownership is enforced on the (id, user) pair.
	UpsertDraft(ctx context.Context, userID, orgID uuid.UUID, d *ComposeDraft) error
	// ListDrafts returns the user's drafts in this org, newest first.
	ListDrafts(ctx context.Context, userID, orgID uuid.UUID) ([]ComposeDraft, error)
	// DeleteDraft removes the user's draft; deleting a missing one is a no-op.
	DeleteDraft(ctx context.Context, userID, id uuid.UUID) error
}

type composeRepository struct {
	db *db.DB
}

func NewComposeRepository(db *db.DB) ComposeRepository {
	return &composeRepository{db: db}
}

func (r *composeRepository) AddressHistoryByAccount(ctx context.Context, orgID uuid.UUID, address string) (map[uuid.UUID]ComposeAffinity, error) {
	query := `
		SELECT account_id, SUM(messages)::int, MAX(last_at)
		FROM (
			SELECT ue.email_id AS account_id, COUNT(*) AS messages, MAX(ue.internal_date) AS last_at
			FROM unibox_emails ue
			WHERE ue.email_id IN (SELECT id FROM email_accounts WHERE organization_id = $1)
			  AND EXISTS (
					SELECT 1 FROM unnest(ue.from_addr || ue.to_addr) AS f(addr)
					WHERE f.addr ILIKE '%' || $2 || '%'
			  )
			GROUP BY ue.email_id

			UNION ALL

			SELECT t.email_account_id, COUNT(*), MAX(t.created_at)
			FROM tasks t
			JOIN email_tasks et ON et.task_id = t.id
			WHERE t.email_account_id IN (SELECT id FROM email_accounts WHERE organization_id = $1)
			  AND t.status <> 'cancelled'
			  AND EXISTS (
					SELECT 1 FROM unnest(et.to_addrs) AS a(addr)
					WHERE a.addr ILIKE '%' || $2 || '%'
			  )
			GROUP BY t.email_account_id
		) x
		GROUP BY account_id
	`

	rows, err := r.db.Query(ctx, query, orgID, address)
	if err != nil {
		db.CaptureError(err, query, []any{orgID, address}, "query")
		return nil, err
	}
	defer rows.Close()

	out := make(map[uuid.UUID]ComposeAffinity)
	for rows.Next() {
		var id uuid.UUID
		var messages int
		var lastAt *time.Time
		if err := rows.Scan(&id, &messages, &lastAt); err != nil {
			continue
		}
		out[id] = ComposeAffinity{Messages: messages, LastAt: lastAt}
	}
	return out, rows.Err()
}

func (r *composeRepository) UpsertDraft(ctx context.Context, userID, orgID uuid.UUID, d *ComposeDraft) error {
	query := `
		INSERT INTO compose_drafts (id, user_id, organization_id, email_account_id, to_addrs, cc, bcc, subject, body, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, NOW(), NOW())
		ON CONFLICT (id) DO UPDATE SET
			email_account_id = EXCLUDED.email_account_id,
			to_addrs = EXCLUDED.to_addrs,
			cc = EXCLUDED.cc,
			bcc = EXCLUDED.bcc,
			subject = EXCLUDED.subject,
			body = EXCLUDED.body,
			updated_at = NOW()
		WHERE compose_drafts.user_id = $2
	`
	args := []any{d.ID, userID, orgID, d.EmailAccountID, d.To, d.CC, d.BCC, d.Subject, d.Body}
	if _, err := r.db.Exec(ctx, query, args...); err != nil {
		db.CaptureError(err, query, args, "exec")
		return err
	}
	return nil
}

func (r *composeRepository) ListDrafts(ctx context.Context, userID, orgID uuid.UUID) ([]ComposeDraft, error) {
	query := `
		SELECT id, email_account_id, to_addrs, cc, bcc, subject, body, updated_at, created_at
		FROM compose_drafts
		WHERE user_id = $1 AND organization_id = $2
		ORDER BY updated_at DESC
		LIMIT 100
	`
	rows, err := r.db.Query(ctx, query, userID, orgID)
	if err != nil {
		db.CaptureError(err, query, []any{userID, orgID}, "query")
		return nil, err
	}
	defer rows.Close()

	out := []ComposeDraft{}
	for rows.Next() {
		var d ComposeDraft
		if err := rows.Scan(&d.ID, &d.EmailAccountID, &d.To, &d.CC, &d.BCC, &d.Subject, &d.Body, &d.UpdatedAt, &d.CreatedAt); err != nil {
			continue
		}
		if d.To == nil {
			d.To = []string{}
		}
		if d.CC == nil {
			d.CC = []string{}
		}
		if d.BCC == nil {
			d.BCC = []string{}
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

func (r *composeRepository) DeleteDraft(ctx context.Context, userID, id uuid.UUID) error {
	query := `DELETE FROM compose_drafts WHERE id = $1 AND user_id = $2`
	if _, err := r.db.Exec(ctx, query, id, userID); err != nil {
		db.CaptureError(err, query, []any{id, userID}, "exec")
		return err
	}
	return nil
}

func (r *composeRepository) TodaySentCounts(ctx context.Context, accountIDs []uuid.UUID) (map[uuid.UUID]int, error) {
	out := make(map[uuid.UUID]int)
	if len(accountIDs) == 0 {
		return out, nil
	}

	query := `
		SELECT email_account_id, count
		FROM daily_email_counts
		WHERE email_account_id = ANY($1) AND date = CURRENT_DATE
	`
	rows, err := r.db.Query(ctx, query, accountIDs)
	if err != nil {
		db.CaptureError(err, query, []any{accountIDs}, "query")
		return nil, err
	}
	defer rows.Close()

	for rows.Next() {
		var id uuid.UUID
		var count int
		if err := rows.Scan(&id, &count); err != nil {
			continue
		}
		out[id] = count
	}
	return out, rows.Err()
}
