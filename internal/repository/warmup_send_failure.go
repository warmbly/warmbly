package repository

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/models"
)

const mailboxLoadingPattern = models.MailboxNotLoadedPrefix + "%"

// Only provider-confirmed delivery closes a loading incident, never sync or dispatch.
const warmupSendFailuresSQL = `
	SELECT e.id, e.organization_id, e.worker_id, e.status,
	       e.warmup IS NOT NULL AND e.warmup_paused_at IS NULL AS warming,
	       e.test_mode IS NULL OR e.test_mode = 'legacy'
	         OR e.test_mode = 'diagnostic' AND e.test_send_enabled AS sending,
	       latest.message, latest.at, COALESCE(first.at, latest.at) AS first_at
	FROM email_accounts e
	JOIN LATERAL (
		SELECT tf.message, t.updated_at AS at
		FROM tasks t JOIN task_failures tf ON tf.task_id = t.id
		WHERE t.email_account_id = e.id AND t.task_type = 'warmup' AND t.status = 'failed'
		  AND t.updated_at <= $3
		ORDER BY t.updated_at DESC, t.id DESC LIMIT 1
	) latest ON true
	LEFT JOIN LATERAL (
		SELECT c.completed_at AS at
		FROM (
			-- Sort task history before probing confirmation tokens.
			SELECT id, completed_at FROM tasks
			WHERE email_account_id = e.id AND task_type = 'warmup' AND status = 'completed' AND completed_at <= $3
			ORDER BY completed_at DESC
		) c JOIN warmup_tokens wt ON wt.task_id = c.id AND wt.sent_message_id <> ''
		ORDER BY c.completed_at DESC LIMIT 1
	) confirmed ON true
	LEFT JOIN LATERAL (
		SELECT p.updated_at AS at
		FROM tasks p JOIN task_failures pf ON pf.task_id = p.id
		WHERE p.email_account_id = e.id AND p.task_type = 'warmup' AND p.status = 'failed'
		  AND pf.message NOT LIKE $2 AND p.updated_at <= $3
		ORDER BY p.updated_at DESC LIMIT 1
	) non_loading ON latest.message LIKE $2
	LEFT JOIN LATERAL (
		WITH loading AS (
			SELECT t.updated_at AS at,
			       LEAD(t.updated_at) OVER(ORDER BY t.updated_at DESC,t.id DESC) AS previous_at
			FROM tasks t JOIN task_failures tf ON tf.task_id = t.id
			WHERE t.email_account_id = e.id AND t.task_type = 'warmup' AND t.status = 'failed'
			  AND tf.message LIKE $2
			  AND t.updated_at <= $3
			  AND (confirmed.at IS NULL OR t.updated_at > confirmed.at)
			  AND (non_loading.at IS NULL OR t.updated_at > non_loading.at)
		)
		SELECT MIN(at) AS at FROM loading
		WHERE at >= COALESCE((SELECT MAX(at) FROM loading
		    WHERE previous_at <= at - INTERVAL '1 hour'),'-infinity'::timestamptz)
	) first ON latest.message LIKE $2
	WHERE ($1::uuid IS NULL OR e.id = $1)
	  AND e.organization_id IS NOT NULL
	  AND (confirmed.at IS NULL OR latest.at > confirmed.at)
`

type MailboxLoadingIncident struct {
	AccountID      uuid.UUID
	OrganizationID uuid.UUID
	WorkerID       *uuid.UUID
	FirstFailureAt time.Time
	LastFailureAt  time.Time
}

func (r *taskRepository) ListOverdueWarmupDispatches(ctx context.Context, now time.Time) ([]MailboxLoadingIncident, error) {
	rows, err := r.db.Query(ctx, `SELECT e.id,e.organization_id,e.worker_id,MIN(t.scheduled_at),$1::timestamptz
	    FROM tasks t JOIN email_accounts e ON e.id=t.email_account_id
	    WHERE t.task_type='warmup' AND t.status='pending' AND t.scheduled_at <= $2
	      AND e.status='active' AND e.warmup IS NOT NULL AND e.warmup_paused_at IS NULL
	      AND (e.test_mode IS NULL OR e.test_mode='legacy' OR e.test_mode='diagnostic' AND e.test_send_enabled)
	    GROUP BY e.id`, now, now.Add(-models.WarmupLoadingGracePeriod))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var incidents []MailboxLoadingIncident
	for rows.Next() {
		var v MailboxLoadingIncident
		if err := rows.Scan(&v.AccountID, &v.OrganizationID, &v.WorkerID, &v.FirstFailureAt, &v.LastFailureAt); err != nil {
			return nil, err
		}
		incidents = append(incidents, v)
	}
	return incidents, rows.Err()
}

func (r *warmupRepository) ListMailboxLoadingIncidents(ctx context.Context, now time.Time) ([]MailboxLoadingIncident, error) {
	rows, err := r.db.Query(ctx, `
		SELECT id, organization_id, worker_id, first_at, at FROM (`+warmupSendFailuresSQL+`) f
		WHERE message LIKE $2 AND first_at <= $3 - INTERVAL '1 hour' AND at > $3 - INTERVAL '1 hour'
		  AND status = 'active' AND warming AND sending
		ORDER BY worker_id, id
	`, nil, mailboxLoadingPattern, now)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var incidents []MailboxLoadingIncident
	for rows.Next() {
		var v MailboxLoadingIncident
		if err := rows.Scan(&v.AccountID, &v.OrganizationID, &v.WorkerID, &v.FirstFailureAt, &v.LastFailureAt); err != nil {
			return nil, err
		}
		incidents = append(incidents, v)
	}
	return incidents, rows.Err()
}
