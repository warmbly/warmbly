package repository

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/warmbly/warmbly/internal/app/instancesettings"
	"github.com/warmbly/warmbly/internal/app/monitoring"
	"github.com/warmbly/warmbly/internal/models"
)

func (r *MonitoringRepository) domainSource() monitoring.Source {
	return monitoring.Source{ID: "domain_auth", Permission: models.AdminPermViewUsers, Collect: func(ctx context.Context, at time.Time) (models.MonitoringSource, error) {
		if r == nil || r.pool == nil {
			return models.MonitoringSource{Availability: models.MonitoringUnavailable, Coverage: "unavailable", Reason: "dependency_missing"}, nil
		}
		policy, err := instancesettings.NewStore(r.pool).Get(ctx)
		if err != nil {
			return models.MonitoringSource{}, err
		}
		grace := policy.Deliverability.AuthGrace()
		queries := []monitoringQuery{
			{id: "domain_unknown", title: "Unobserved domain authentication", condition: models.MonitoringUnknown, severity: "warning", query: `auth_state='unknown' OR auth_checked_at IS NULL`},
			{id: "domain_stale", title: "Domain checks older than 26h", condition: models.MonitoringTelemetryStale, severity: "warning", query: `auth_checked_at<$1-INTERVAL '26 hours'`},
			{id: "domain_failing", title: "Stored failing domain observations", condition: models.MonitoringRecoveryUnverified, severity: "warning", query: `auth_state='failing'`},
			{id: "domain_passing_fresh", title: "Recently observed passing domain checks", condition: models.MonitoringHealthy, severity: "info", query: `auth_state='passing' AND auth_checked_at BETWEEN $1-INTERVAL '26 hours' AND $1`},
			{id: "domain_future_clock", title: "Future-dated domain checks", condition: models.MonitoringUnknown, severity: "warning", query: `auth_checked_at>$1`},
			{id: "domain_blocked", title: "Enforced domain safety blocks", condition: models.MonitoringSafetyHold, severity: "warning", query: `$2 AND auth_state='failing' AND auth_failing_since+make_interval(secs=>$3)<=$1`},
		}
		out := models.MonitoringSource{Metrics: []models.MonitoringMetric{}}
		for _, q := range queries {
			q.unit, q.note = "mailboxes", "DNS observation, separate from mailbox credentials; 26h freshness is monitoring policy. Missing DKIM at probed selectors is not proof a record is absent."
			m := monitoringMetric(q, at, "/mailboxes")
			threshold := int64((26 * time.Hour).Seconds())
			m.ThresholdSeconds = &threshold
			if q.id == "domain_blocked" {
				threshold = int64(grace.Seconds())
				m.ThresholdSeconds = &threshold
				m.Note = "Current policy and stored failing-since enforce this restriction independently of check freshness; no holds are changed."
			}
			err := r.pool.QueryRow(ctx, `SELECT COUNT(*),COUNT(DISTINCT id),COUNT(DISTINCT organization_id),COUNT(*) FILTER(WHERE organization_id IS NULL),MIN(auth_checked_at),MAX(auth_checked_at),NULL::timestamptz FROM email_accounts WHERE (`+strings.ReplaceAll(q.query, "$1", "$1::timestamptz")+`) AND ($2 OR NOT $2) AND $3::double precision>=0 AND $1::timestamptz IS NOT NULL`, at, policy.Deliverability.EnforceDomainAuth, grace.Seconds()).Scan(&m.Count, &m.AffectedMailboxes, &m.AffectedOrganizations, &m.UnknownOrganizationRows, &m.EvidenceAt, &m.LatestEvidenceAt, &m.NextEligibleAt)
			if err != nil {
				return out, err
			}
			classifyMonitoringMetric(&m)
			out.Metrics = append(out.Metrics, m)
		}
		return out, nil
	}}
}

func (r *MonitoringRepository) loadingSource() monitoring.Source {
	return monitoring.Source{ID: "warmup_loading", Permission: models.AdminPermViewCampaigns, Collect: func(ctx context.Context, at time.Time) (models.MonitoringSource, error) {
		out := models.MonitoringSource{Metrics: []models.MonitoringMetric{}}
		if r == nil || r.pool == nil {
			out.Availability = models.MonitoringUnavailable
			out.Coverage = "unavailable"
			out.Reason = "dependency_missing"
			return out, nil
		}
		var expected int64
		if err := r.pool.QueryRow(ctx, `SELECT COUNT(*) FROM email_accounts WHERE status='active' AND warmup IS NOT NULL AND warmup_paused_at IS NULL AND organization_id IS NOT NULL AND (test_mode IS NULL OR test_mode='legacy' OR test_mode='diagnostic' AND test_send_enabled)`).Scan(&expected); err != nil {
			return out, err
		}
		measured := min(expected, 200)
		out.MeasuredScopes, out.ExpectedScopes = &measured, &expected
		if measured < expected {
			out.Coverage = "partial"
			out.Reason = "scope_limit"
		}
		queries := []monitoringQuery{
			{id: "loading_recent", title: "Recent unrecovered mailbox-loading reports", condition: models.MonitoringRecentFailure, severity: "warning"},
			{id: "loading_persistent", title: "Persistent recent mailbox-loading incidents", condition: models.MonitoringPersistentFailure, severity: "warning"},
			{id: "loading_old", title: "Old mailbox-loading report, recovery unverified", condition: models.MonitoringRecoveryUnverified, severity: "warning"},
		}
		for _, q := range queries {
			q.unit, q.note = "mailboxes", "Bounded eligible-mailbox inspection; counts are lower bounds when coverage is partial. Native task updated/completed clocks are proxies; only scoped nonempty warmup token evidence closes the incident."
			m := monitoringMetric(q, at, "/sends?tab=failures")
			threshold := int64(3600)
			m.ThresholdSeconds = &threshold
			zero := int64(0)
			m.Count, m.AffectedMailboxes, m.AffectedOrganizations, m.UnknownOrganizationRows = &zero, &zero, &zero, &zero
			out.Metrics = append(out.Metrics, m)
		}
		query := `WITH eligible AS (SELECT id,organization_id,worker_id,status,warmup,warmup_paused_at,test_mode,test_send_enabled FROM email_accounts WHERE status='active' AND warmup IS NOT NULL AND warmup_paused_at IS NULL AND organization_id IS NOT NULL AND (test_mode IS NULL OR test_mode='legacy' OR test_mode='diagnostic' AND test_send_enabled) ORDER BY id LIMIT 200), failures AS (` + strings.Replace(warmupSendFailuresSQL, "FROM email_accounts e", "FROM eligible e", 1) + `), classified AS (
			SELECT id,organization_id,first_at,at,
			CASE WHEN at>$3-INTERVAL '1 hour' AND first_at>$3-INTERVAL '1 hour' THEN 'loading_recent'
			     WHEN first_at<=$3-INTERVAL '1 hour' AND at>$3-INTERVAL '1 hour' THEN 'loading_persistent'
			     WHEN at<=$3-INTERVAL '1 hour' THEN 'loading_old' END AS bucket
			FROM failures WHERE message LIKE $2
		)
		SELECT bucket,COUNT(*),COUNT(DISTINCT id),COUNT(DISTINCT organization_id),COUNT(*) FILTER(WHERE organization_id IS NULL),MIN(first_at),MAX(at),NULL::timestamptz
		FROM classified WHERE bucket IS NOT NULL GROUP BY bucket`
		rows, err := r.pool.Query(ctx, query, nil, mailboxLoadingPattern, at)
		if err != nil {
			return out, err
		}
		defer rows.Close()
		for rows.Next() {
			var bucket string
			var count, mailboxes, organizations, unknown int64
			var first, last, next *time.Time
			if err := rows.Scan(&bucket, &count, &mailboxes, &organizations, &unknown, &first, &last, &next); err != nil {
				return out, err
			}
			matched := false
			for i := range out.Metrics {
				if out.Metrics[i].ID == bucket {
					m := &out.Metrics[i]
					m.Count, m.AffectedMailboxes, m.AffectedOrganizations, m.UnknownOrganizationRows = &count, &mailboxes, &organizations, &unknown
					m.EvidenceAt, m.LatestEvidenceAt, m.NextEligibleAt = first, last, next
					matched = true
					break
				}
			}
			if !matched {
				return out, errors.New("unexpected mailbox-loading monitoring bucket")
			}
		}
		if err := rows.Err(); err != nil {
			return out, err
		}
		for i := range out.Metrics {
			classifyMonitoringMetric(&out.Metrics[i])
		}
		return out, nil
	}}
}

const workerHeartbeatMissingPredicate = `w.last_seen_at IS NULL OR w.last_seen_at<=observation.at-INTERVAL '5 minutes' OR w.last_seen_at>observation.at`

func (r *MonitoringRepository) workerSource() monitoring.Source {
	return monitoring.Source{ID: "workers", Permission: models.AdminPermViewWorkers, Collect: func(ctx context.Context, _ time.Time) (models.MonitoringSource, error) {
		out := models.MonitoringSource{Metrics: []models.MonitoringMetric{}}
		if r == nil || r.pool == nil {
			out.Availability = models.MonitoringUnavailable
			out.Coverage = "unavailable"
			out.Reason = "dependency_missing"
			return out, nil
		}
		q := monitoringQuery{id: "workers_missing_heartbeat", title: "Active workers without recent heartbeat", unit: "workers", condition: models.MonitoringTelemetryStale, severity: "warning", note: "Native node liveness window is 5m. Missing heartbeat is registration evidence, not proof of power failure or lost throughput. Future heartbeat stamps are clock uncertainty relative to the database observation, not proof of a stopped worker."}
		var observed time.Time
		var future int64
		m := monitoringMetric(q, observed, "/fleet")
		m.ObservedAt = &observed
		threshold := int64(models.NodeLivenessWindow.Seconds())
		m.ThresholdSeconds = &threshold
		// Capture the worker snapshot before taking its single database observation clock.
		rows, err := r.pool.Query(ctx, `WITH workers AS MATERIALIZED (
			SELECT id,last_seen_at FROM fleet_nodes WHERE role='worker' AND active
		), observation AS MATERIALIZED (
			SELECT clock_timestamp() AS at,COUNT(*) FROM workers
		), stale AS MATERIALIZED (
			SELECT w.id,w.last_seen_at FROM workers w CROSS JOIN observation WHERE `+workerHeartbeatMissingPredicate+`
		), assigned AS MATERIALIZED (
			SELECT e.worker_id,e.organization_id FROM email_accounts e JOIN stale s ON s.id=e.worker_id
		), summary AS (
			SELECT (SELECT COUNT(*) FROM stale) AS count,
				(SELECT COUNT(*) FROM stale CROSS JOIN observation WHERE last_seen_at>observation.at) AS future_count,
				(SELECT COUNT(*) FROM assigned) AS mailboxes,
				(SELECT COUNT(DISTINCT organization_id) FROM assigned) AS organizations,
				(SELECT COUNT(*) FROM assigned WHERE organization_id IS NULL) AS unknown_organizations,
				(SELECT MIN(last_seen_at) FROM stale) AS first_seen,
				(SELECT MAX(last_seen_at) FROM stale) AS last_seen
		), sample AS MATERIALIZED (
			SELECT id,last_seen_at FROM stale ORDER BY last_seen_at NULLS FIRST,id LIMIT 20
		), details AS (
			SELECT a.worker_id,COUNT(*) AS mailboxes,COUNT(DISTINCT a.organization_id) AS organizations,
				COUNT(*) FILTER(WHERE a.organization_id IS NULL) AS unknown_organizations
			FROM assigned a JOIN sample s ON s.id=a.worker_id GROUP BY a.worker_id
		)
		SELECT observation.at,summary.count,summary.future_count,summary.mailboxes,summary.organizations,summary.unknown_organizations,summary.first_seen,summary.last_seen,
			sample.id::text,sample.last_seen_at,COALESCE(details.mailboxes,0),COALESCE(details.organizations,0),COALESCE(details.unknown_organizations,0)
		FROM summary CROSS JOIN observation LEFT JOIN sample ON true
		LEFT JOIN details ON details.worker_id=sample.id
		ORDER BY sample.last_seen_at NULLS FIRST,sample.id`)
		if err != nil {
			return out, err
		}
		defer rows.Close()
		for rows.Next() {
			var scopeID *string
			v := monitoringMetric(q, observed, "")
			one := int64(1)
			v.Count = &one
			v.ThresholdSeconds = &threshold
			if err := rows.Scan(&observed, &m.Count, &future, &m.AffectedMailboxes, &m.AffectedOrganizations, &m.UnknownOrganizationRows, &m.EvidenceAt, &m.LatestEvidenceAt, &scopeID, &v.EvidenceAt, &v.AffectedMailboxes, &v.AffectedOrganizations, &v.UnknownOrganizationRows); err != nil {
				return out, err
			}
			if scopeID == nil {
				continue
			}
			v.ScopeID, v.ObservedAt = *scopeID, &observed
			v.ID = "worker_missing_heartbeat"
			v.Investigate = "/workers/" + v.ScopeID
			if v.EvidenceAt != nil && v.EvidenceAt.After(observed) {
				v.Condition = models.MonitoringUnknown
			}
			out.Metrics = append(out.Metrics, v)
		}
		if err := rows.Err(); err != nil {
			return out, err
		}
		if m.Count == nil {
			return out, errors.New("incomplete worker monitoring rows")
		}
		if *m.Count > 0 && future == *m.Count {
			m.Condition = models.MonitoringUnknown
		}
		classifyMonitoringMetric(&m)
		expected := *m.Count
		measured := int64(len(out.Metrics))
		out.Metrics = append([]models.MonitoringMetric{m}, out.Metrics...)
		out.ObservedAt = &observed
		out.ExpectedScopes = &expected
		out.MeasuredScopes = &measured
		if measured < expected {
			out.Coverage = "partial"
			out.Reason = "sample_limit"
		}
		return out, nil
	}}
}

func (r *MonitoringRepository) MonitoringWorkerIDs(ctx context.Context) ([]string, int64, error) {
	if r == nil || r.pool == nil {
		return nil, 0, context.Canceled
	}
	var expected int64
	if err := r.pool.QueryRow(ctx, `SELECT COUNT(*) FROM fleet_nodes WHERE role='worker' AND active`).Scan(&expected); err != nil {
		return nil, 0, err
	}
	rows, err := r.pool.Query(ctx, `SELECT id::text FROM fleet_nodes WHERE role='worker' AND active ORDER BY id LIMIT 20`)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	ids := []string{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, 0, err
		}
		ids = append(ids, id)
	}
	return ids, expected, rows.Err()
}
