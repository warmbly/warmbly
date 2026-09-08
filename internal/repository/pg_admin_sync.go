package repository

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/infrastructure/db"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/utils/paging"
)

// AdminSyncRepository is the operator's cross-workspace view of mailbox sync:
// every email_sync_state row joined to its mailbox, owner and workspace.
type AdminSyncRepository interface {
	// Search lists sync rows newest-updated first, filtered by state and a
	// free-text match on the mailbox address or workspace name, and returns
	// the instance-wide summary alongside (the summary ignores the filter).
	Search(ctx context.Context, search *models.AdminSyncSearch) (*models.AdminSyncResult, error)
	// ClearThrottle lifts a fair-use throttle on the platform copy. False when
	// the mailbox has no sync row or was not throttled.
	ClearThrottle(ctx context.Context, emailID uuid.UUID) (bool, error)
	// ResetBackfill puts the backfill back to pending with an empty cursor and
	// zero progress, so the next load re-imports history from scratch. False
	// when the mailbox has no sync row.
	ResetBackfill(ctx context.Context, emailID uuid.UUID) (bool, error)
}

type adminSyncRepository struct {
	db *db.DB
}

func NewAdminSyncRepository(d *db.DB) AdminSyncRepository {
	return &adminSyncRepository{db: d}
}

// Sentinel errors the handler maps to 400.
var (
	ErrAdminSyncBadCursor = errors.New("admin sync: invalid cursor")
	ErrAdminSyncBadState  = errors.New("admin sync: invalid state filter")
)

const (
	adminSyncDefaultLimit = 50
	adminSyncMaxLimit     = 200
)

// adminSyncStateWhere is the per-row predicate for each state filter. Expired
// throttles are not throttles, mirroring EmailSyncStateRepository.Get.
func adminSyncStateWhere(state string) (string, error) {
	switch state {
	case "", "all":
		return "", nil
	case "throttled":
		return "s.throttled_until > now()", nil
	case "backfilling":
		return "s.backfill_status = 'running'", nil
	case "stalled":
		return "s.backfill_status = 'running' AND s.updated_at < now() - interval '1 hour'", nil
	case "pending":
		return "s.backfill_status = 'pending'", nil
	case "complete":
		return "s.backfill_status = 'complete'", nil
	}
	return "", ErrAdminSyncBadState
}

func (r *adminSyncRepository) Search(ctx context.Context, search *models.AdminSyncSearch) (*models.AdminSyncResult, error) {
	if search == nil {
		search = &models.AdminSyncSearch{}
	}
	limit := search.Limit
	if limit <= 0 {
		limit = adminSyncDefaultLimit
	}
	if limit > adminSyncMaxLimit {
		limit = adminSyncMaxLimit
	}

	where := "WHERE TRUE"
	args := []any{}
	if cond, err := adminSyncStateWhere(search.State); err != nil {
		return nil, err
	} else if cond != "" {
		where += " AND " + cond
	}
	if q := strings.TrimSpace(search.Q); q != "" {
		args = append(args, "%"+q+"%")
		n := itoa(len(args))
		where += " AND (ea.email ILIKE $" + n + " OR o.name ILIKE $" + n + ")"
	}
	if search.Cursor != "" {
		cursor, err := uuid.Parse(search.Cursor)
		if err != nil {
			return nil, ErrAdminSyncBadCursor
		}
		args = append(args, cursor)
		n := itoa(len(args))
		// Keyset on (updated_at, email_id); the cursor is the last row's id and
		// its updated_at is looked up so the token stays a plain uuid.
		where += " AND (s.updated_at, s.email_id) < ((SELECT c.updated_at FROM email_sync_state c WHERE c.email_id = $" + n + "), $" + n + "::uuid)"
	}
	args = append(args, limit+1)

	query := `
		SELECT s.email_id, s.user_id, ea.email, ea.provider::text, ea.status::text,
		       ea.organization_id, COALESCE(o.name, ''), ea.worker_id,
		       s.backfill_status, s.backfill_synced, s.backfill_since,
		       s.backfill_started_at, s.backfill_completed_at,
		       CASE WHEN s.throttled_until > now() THEN s.throttled_until END,
		       CASE WHEN s.throttled_until > now() THEN s.throttle_reason ELSE '' END,
		       s.deferred,
		       (s.backfill_status = 'running' AND s.updated_at < now() - interval '1 hour'),
		       s.last_synced_at, s.updated_at
		FROM email_sync_state s
		JOIN email_accounts ea ON ea.id = s.email_id
		LEFT JOIN organizations o ON o.id = ea.organization_id
		` + where + `
		ORDER BY s.updated_at DESC, s.email_id DESC
		LIMIT $` + itoa(len(args))

	rows, err := r.db.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("admin sync: search: %w", err)
	}
	defer rows.Close()

	items := []models.AdminSyncRow{}
	for rows.Next() {
		var row models.AdminSyncRow
		if err := rows.Scan(
			&row.EmailID, &row.UserID, &row.Email, &row.Provider, &row.AccountStatus,
			&row.OrganizationID, &row.OrganizationName, &row.WorkerID,
			&row.BackfillStatus, &row.BackfillSynced, &row.BackfillSince,
			&row.BackfillStartedAt, &row.BackfillCompletedAt,
			&row.ThrottledUntil, &row.ThrottleReason,
			&row.Deferred, &row.Stalled,
			&row.LastSyncedAt, &row.UpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("admin sync: scan: %w", err)
		}
		items = append(items, row)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("admin sync: rows: %w", err)
	}

	result := &models.AdminSyncResult{
		Data:       items,
		Pagination: &models.Pagination{HasMore: len(items) > limit},
	}
	if len(items) > limit {
		result.Data = items[:limit]
		result.Pagination.NextCursor = paging.UUIDString(items[limit-1].EmailID)
	}

	const summary = `
		SELECT COUNT(*),
		       COUNT(*) FILTER (WHERE throttled_until > now()),
		       COUNT(*) FILTER (WHERE backfill_status = 'running'),
		       COUNT(*) FILTER (WHERE backfill_status = 'running' AND updated_at < now() - interval '1 hour'),
		       COUNT(*) FILTER (WHERE backfill_status = 'pending'),
		       COUNT(*) FILTER (WHERE backfill_status = 'complete'),
		       COALESCE(SUM(deferred), 0)
		FROM email_sync_state
	`
	var total, throttled, backfilling, stalled, pending, complete, deferred int64
	if err := r.db.QueryRow(ctx, summary).Scan(&total, &throttled, &backfilling, &stalled, &pending, &complete, &deferred); err != nil {
		return nil, fmt.Errorf("admin sync: summary: %w", err)
	}
	result.Summary = models.AdminSyncSummary{
		Total:       int(total),
		Throttled:   int(throttled),
		Backfilling: int(backfilling),
		Stalled:     int(stalled),
		Pending:     int(pending),
		Complete:    int(complete),
		Deferred:    int(deferred),
	}
	return result, nil
}

func (r *adminSyncRepository) ClearThrottle(ctx context.Context, emailID uuid.UUID) (bool, error) {
	const q = `
		UPDATE email_sync_state
		SET throttled_until = NULL, throttle_reason = '', updated_at = now()
		WHERE email_id = $1 AND throttled_until IS NOT NULL
	`
	tag, err := r.db.Exec(ctx, q, emailID)
	if err != nil {
		return false, fmt.Errorf("admin sync: clear throttle: %w", err)
	}
	return tag.RowsAffected() > 0, nil
}

func (r *adminSyncRepository) ResetBackfill(ctx context.Context, emailID uuid.UUID) (bool, error) {
	const q = `
		UPDATE email_sync_state
		SET backfill_status = 'pending', backfill_cursor = '{}'::jsonb, backfill_synced = 0,
		    backfill_since = NULL, backfill_started_at = NULL, backfill_completed_at = NULL,
		    updated_at = now()
		WHERE email_id = $1
	`
	tag, err := r.db.Exec(ctx, q, emailID)
	if err != nil {
		return false, fmt.Errorf("admin sync: reset backfill: %w", err)
	}
	return tag.RowsAffected() > 0, nil
}
