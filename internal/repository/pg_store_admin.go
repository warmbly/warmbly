package repository

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/warmbly/warmbly/internal/infrastructure/db"
	"github.com/warmbly/warmbly/internal/models"
)

// StoreAdminRepository is the operator's instance-wide view of the store, and
// the reads an agency's overview makes across its clients. Every customer path
// stays on StoreRepository, whose every query carries an organization predicate.
type StoreAdminRepository interface {
	// OrderOrg, DomainOrg and MailboxOrg name the workspace a row belongs to; ok is false when there is none.
	OrderOrg(ctx context.Context, id uuid.UUID) (uuid.UUID, bool, error)
	DomainOrg(ctx context.Context, id uuid.UUID) (uuid.UUID, bool, error)
	MailboxOrg(ctx context.Context, id uuid.UUID) (uuid.UUID, bool, error)

	AdminListOrders(ctx context.Context, f models.StoreAdminOrderFilter, after *models.StorePage, limit int, now time.Time) ([]models.StoreOrder, error)
	AdminListDomains(ctx context.Context, f models.StoreAdminDomainFilter, after *models.StorePage, limit int) ([]models.StoreDomain, error)
	AdminListMailboxes(ctx context.Context, f models.StoreAdminMailboxFilter, after *models.StorePage, limit int) ([]models.StoreMailbox, error)
	AdminCounts(ctx context.Context, now time.Time) (*models.StoreAdminCounts, error)
	LiveMailboxesByProvider(ctx context.Context) ([]models.StoreLiveMailboxes, error)
	OrgNames(ctx context.Context, ids []uuid.UUID) (map[uuid.UUID]string, error)

	// ResetConnect makes a failed or stalled connect claimable now, with its attempts cleared.
	ResetConnect(ctx context.Context, orgID, id uuid.UUID) (bool, error)

	// OrgSummaries are the listed workspaces' purchased infrastructure, one row per workspace that has any.
	OrgSummaries(ctx context.Context, orgIDs []uuid.UUID, now time.Time) ([]models.StoreOrgSummary, error)
	// RenewalsBefore are the listed workspaces' live purchased domains expiring before horizon, soonest first.
	RenewalsBefore(ctx context.Context, orgIDs []uuid.UUID, horizon time.Time, limit int) ([]models.StoreDomain, error)

	GetMeta(ctx context.Context, key string) (json.RawMessage, error)
	SetMeta(ctx context.Context, key string, value json.RawMessage) error
}

var _ StoreAdminRepository = (*storeRepository)(nil)

// storeStuckOrder is an order that should have moved by now: charged and not placed
// after the reconciler's grace, or still setting up two days after it was bought.
const storeStuckOrder = `((o.status = 'charging' AND o.updated_at < $NOW - interval '30 minutes')
	OR (o.status = 'processing' AND o.created_at < $NOW - interval '72 hours'))`

func (r *storeRepository) orgOf(ctx context.Context, table string, id uuid.UUID) (uuid.UUID, bool, error) {
	var org uuid.UUID
	err := r.db.QueryRow(ctx, `SELECT organization_id FROM `+table+` WHERE id = $1`, id).Scan(&org)
	if errors.Is(err, pgx.ErrNoRows) {
		return uuid.Nil, false, nil
	}
	if err != nil {
		return uuid.Nil, false, err
	}
	return org, true, nil
}

func (r *storeRepository) OrderOrg(ctx context.Context, id uuid.UUID) (uuid.UUID, bool, error) {
	return r.orgOf(ctx, "store_orders", id)
}

func (r *storeRepository) DomainOrg(ctx context.Context, id uuid.UUID) (uuid.UUID, bool, error) {
	return r.orgOf(ctx, "store_domains", id)
}

func (r *storeRepository) MailboxOrg(ctx context.Context, id uuid.UUID) (uuid.UUID, bool, error) {
	return r.orgOf(ctx, "store_mailboxes", id)
}

// where accumulates a filter's predicates with numbered parameters.
type where struct {
	parts []string
	args  []any
}

func (w *where) add(sql string, args ...any) {
	for _, a := range args {
		w.args = append(w.args, a)
		sql = strings.Replace(sql, "?", "$"+strconv.Itoa(len(w.args)), 1)
	}
	w.parts = append(w.parts, sql)
}

func (w *where) page(alias string, after *models.StorePage) {
	if after != nil {
		w.add("("+alias+"created_at, "+alias+"id) < (?, ?::uuid)", after.CreatedAt, after.ID)
	}
}

func (w *where) sql() string {
	if len(w.parts) == 0 {
		return "TRUE"
	}
	return strings.Join(w.parts, " AND ")
}

func (w *where) limit(n int) string {
	w.args = append(w.args, n)
	return " LIMIT $" + strconv.Itoa(len(w.args))
}

func orgNameMatch(alias string) string {
	return alias + "organization_id IN (SELECT id FROM organizations WHERE name ILIKE ?)"
}

func likeArg(q string) string {
	r := strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`)
	return "%" + r.Replace(q) + "%"
}

func (r *storeRepository) AdminListOrders(ctx context.Context, f models.StoreAdminOrderFilter, after *models.StorePage, limit int, now time.Time) ([]models.StoreOrder, error) {
	var w where
	if f.OrgID != nil {
		w.add("o.organization_id = ?", *f.OrgID)
	}
	switch f.Status {
	case "":
	case "open":
		w.add("o.status IN ('charging', 'processing')")
	default:
		w.add("o.status = ?", f.Status)
	}
	if f.FailureCode != "" {
		w.add("o.failure_code = ?", f.FailureCode)
	}
	if f.Stuck {
		w.add(strings.ReplaceAll(storeStuckOrder, "$NOW", "?::timestamptz"), now, now)
	}
	if q := strings.TrimSpace(f.Query); q != "" {
		if id, err := uuid.Parse(q); err == nil {
			w.add("o.id = ?", id)
		} else {
			w.add(orgNameMatch("o."), likeArg(q))
		}
	}
	w.page("o.", after)
	query := `SELECT ` + storeOrderColumns + ` FROM store_orders o WHERE ` + w.sql() + ` ORDER BY o.created_at DESC, o.id DESC` + w.limit(limit)
	return r.orders(ctx, query, w.args...)
}

func (r *storeRepository) AdminListDomains(ctx context.Context, f models.StoreAdminDomainFilter, after *models.StorePage, limit int) ([]models.StoreDomain, error) {
	var w where
	if f.OrgID != nil {
		w.add("d.organization_id = ?", *f.OrgID)
	}
	if f.Status != "" {
		w.add("d.status = ?", f.Status)
	}
	if f.AutoRenew != nil {
		w.add("d.auto_renew = ?", *f.AutoRenew)
	}
	if f.ExpiresBefore != nil {
		w.add("d.expires_at IS NOT NULL AND d.expires_at <= ? AND d.status IN ('active', 'expired')", *f.ExpiresBefore)
	}
	if f.RenewalFailed {
		w.add("d.renewal_failures > 0")
	}
	if q := strings.TrimSpace(f.Query); q != "" {
		w.add("d.name ILIKE ?", likeArg(strings.ToLower(q)))
	}
	w.page("d.", after)
	query := `SELECT ` + storeDomainColumns + ` FROM store_domains d WHERE ` + w.sql() + ` ORDER BY d.created_at DESC, d.id DESC` + w.limit(limit)
	return r.domains(ctx, query, w.args...)
}

func (r *storeRepository) AdminListMailboxes(ctx context.Context, f models.StoreAdminMailboxFilter, after *models.StorePage, limit int) ([]models.StoreMailbox, error) {
	var w where
	if f.OrgID != nil {
		w.add("organization_id = ?", *f.OrgID)
	}
	if f.Status != "" {
		w.add("status = ?", f.Status)
	}
	if f.ConnectState != "" {
		w.add("connect_state = ?", f.ConnectState)
	}
	if f.BillingFailed {
		w.add("billing_failures > 0")
	}
	if q := strings.TrimSpace(f.Query); q != "" {
		w.add("email ILIKE ?", likeArg(strings.ToLower(q)))
	}
	w.page("", after)
	query := `SELECT ` + storeMailboxColumns + ` FROM store_mailboxes WHERE ` + w.sql() + ` ORDER BY created_at DESC, id DESC` + w.limit(limit)
	return r.mailboxes(ctx, query, w.args...)
}

func (r *storeRepository) AdminCounts(ctx context.Context, now time.Time) (*models.StoreAdminCounts, error) {
	var c models.StoreAdminCounts
	stuck := strings.ReplaceAll(storeStuckOrder, "$NOW", "$1::timestamptz")
	query := `SELECT
		(SELECT count(*) FROM store_orders o WHERE o.status IN ('charging', 'processing')),
		(SELECT count(*) FROM store_orders o WHERE ` + stuck + `),
		(SELECT count(*) FROM store_orders o WHERE o.status = 'failed' AND o.updated_at >= $1::timestamptz - interval '7 days'),
		(SELECT count(*) FROM store_mailboxes WHERE connect_state = 'failed' AND email_account_id IS NULL AND status = 'active'),
		(SELECT count(*) FROM store_mailboxes WHERE ` + storeLiveMailbox + `),
		(SELECT count(*) FROM store_mailboxes WHERE billing_failures > 0 AND ` + storeLiveMailbox + `),
		(SELECT count(*) FROM store_domains WHERE status IN ('active', 'expired', 'registering', 'connecting')),
		(SELECT count(*) FROM store_domains WHERE renewal_failures > 0 AND status IN ('active', 'expired')),
		(SELECT count(*) FROM store_domains WHERE status IN ('active', 'expired') AND expires_at IS NOT NULL
			AND expires_at <= $1::timestamptz + interval '30 days')`
	err := r.db.QueryRow(ctx, query, now).Scan(&c.OrdersOpen, &c.OrdersStuck, &c.OrdersFailed7d, &c.ConnectsFailed,
		&c.MailboxesLive, &c.MailboxesBillingFailed, &c.DomainsLive, &c.DomainsRenewalFailed, &c.DomainsExpiring30d)
	if err != nil {
		db.CaptureError(err, "store admin counts", nil, "queryrow")
		return nil, err
	}
	return &c, nil
}

func (r *storeRepository) LiveMailboxesByProvider(ctx context.Context) ([]models.StoreLiveMailboxes, error) {
	query := `SELECT provider, count(*), COALESCE(sum(monthly_retail_micros), 0) FROM store_mailboxes
		WHERE ` + storeLiveMailbox + ` GROUP BY provider ORDER BY provider`
	rows, err := r.db.Query(ctx, query)
	if err != nil {
		db.CaptureError(err, query, nil, "query")
		return nil, err
	}
	defer rows.Close()
	out := []models.StoreLiveMailboxes{}
	for rows.Next() {
		var l models.StoreLiveMailboxes
		if err := rows.Scan(&l.Provider, &l.Count, &l.MonthlyMicros); err != nil {
			return nil, err
		}
		out = append(out, l)
	}
	return out, rows.Err()
}

func (r *storeRepository) OrgNames(ctx context.Context, ids []uuid.UUID) (map[uuid.UUID]string, error) {
	out := map[uuid.UUID]string{}
	if len(ids) == 0 {
		return out, nil
	}
	query := `SELECT id, name FROM organizations WHERE id = ANY($1)`
	rows, err := r.db.Query(ctx, query, ids)
	if err != nil {
		db.CaptureError(err, query, nil, "query")
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var id uuid.UUID
		var name string
		if err := rows.Scan(&id, &name); err != nil {
			return nil, err
		}
		out[id] = name
	}
	return out, rows.Err()
}

func (r *storeRepository) ResetConnect(ctx context.Context, orgID, id uuid.UUID) (bool, error) {
	n, err := r.affected(ctx, `UPDATE store_mailboxes SET connect_state = 'waiting', connect_attempts = 0, connect_error = '',
		connect_attempted_at = NULL, action_required = NULL, action_reason = NULL, updated_at = now()
		WHERE organization_id = $1 AND id = $2 AND email_account_id IS NULL AND status = 'active'`, orgID, id)
	return n > 0, err
}

func (r *storeRepository) OrgSummaries(ctx context.Context, orgIDs []uuid.UUID, now time.Time) ([]models.StoreOrgSummary, error) {
	if len(orgIDs) == 0 {
		return []models.StoreOrgSummary{}, nil
	}
	query := `
		WITH orgs AS (SELECT unnest($1::uuid[]) AS id),
		doms AS (
			SELECT organization_id,
			       count(*) FILTER (WHERE status NOT IN ('failed', 'transferred_out', 'removed')) AS domains,
			       count(*) FILTER (WHERE source = 'purchased' AND status IN ('active', 'expired') AND expires_at IS NOT NULL
			                          AND expires_at <= $2::timestamptz + interval '30 days') AS renewals,
			       count(*) FILTER (WHERE renewal_failures > 0 AND status IN ('active', 'expired')) AS renewal_failed,
			       count(*) FILTER (WHERE source = 'purchased' AND NOT auto_renew AND status IN ('active', 'expired')) AS auto_renew_off
			FROM store_domains WHERE organization_id = ANY($1) GROUP BY organization_id),
		boxes AS (
			SELECT organization_id,
			       count(*) FILTER (WHERE ` + storeLiveMailbox + `) AS live,
			       count(*) FILTER (WHERE ` + storeLiveMailbox + ` AND email_account_id IS NOT NULL) AS connected,
			       COALESCE(sum(monthly_retail_micros) FILTER (WHERE status IN ('active', 'suspended') AND cancels_at IS NULL), 0) AS monthly,
			       count(*) FILTER (WHERE connect_state = 'failed' AND email_account_id IS NULL AND status = 'active') AS connect_failed,
			       count(*) FILTER (WHERE billing_failures > 0 AND ` + storeLiveMailbox + `) AS billing_failed
			FROM store_mailboxes WHERE organization_id = ANY($1) GROUP BY organization_id),
		ords AS (
			SELECT organization_id,
			       count(*) FILTER (WHERE status IN ('charging', 'processing')) AS open,
			       count(*) FILTER (WHERE status IN ('failed', 'partially_failed') AND updated_at >= $2::timestamptz - interval '7 days') AS failed
			FROM store_orders WHERE organization_id = ANY($1) GROUP BY organization_id)
		SELECT o.id, COALESCE(d.domains, 0), COALESCE(b.live, 0), COALESCE(b.connected, 0), COALESCE(b.monthly, 0),
		       COALESCE(d.renewals, 0), COALESCE(x.open, 0), COALESCE(b.connect_failed, 0), COALESCE(b.billing_failed, 0),
		       COALESCE(d.renewal_failed, 0), COALESCE(d.auto_renew_off, 0), COALESCE(x.failed, 0)
		FROM orgs o
		LEFT JOIN doms d ON d.organization_id = o.id
		LEFT JOIN boxes b ON b.organization_id = o.id
		LEFT JOIN ords x ON x.organization_id = o.id
		WHERE d.organization_id IS NOT NULL OR b.organization_id IS NOT NULL OR x.organization_id IS NOT NULL`
	rows, err := r.db.Query(ctx, query, orgIDs, now)
	if err != nil {
		db.CaptureError(err, "store org summaries", nil, "query")
		return nil, err
	}
	defer rows.Close()
	out := []models.StoreOrgSummary{}
	for rows.Next() {
		var s models.StoreOrgSummary
		if err := rows.Scan(&s.OrganizationID, &s.Domains, &s.MailboxesLive, &s.MailboxesConnected, &s.MonthlyMicros,
			&s.Renewals30d, &s.OrdersOpen, &s.ConnectFailed, &s.BillingFailed, &s.RenewalFailed, &s.AutoRenewOff, &s.OrdersFailed); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

func (r *storeRepository) RenewalsBefore(ctx context.Context, orgIDs []uuid.UUID, horizon time.Time, limit int) ([]models.StoreDomain, error) {
	if len(orgIDs) == 0 {
		return []models.StoreDomain{}, nil
	}
	return r.domains(ctx, `SELECT `+storeDomainColumns+` FROM store_domains d
		WHERE d.organization_id = ANY($1) AND d.source = 'purchased' AND d.status IN ('active', 'expired')
		  AND d.expires_at IS NOT NULL AND d.expires_at <= $2
		ORDER BY d.expires_at, d.id LIMIT $3`, orgIDs, horizon, limit)
}

func (r *storeRepository) GetMeta(ctx context.Context, key string) (json.RawMessage, error) {
	var raw []byte
	err := r.db.QueryRow(ctx, `SELECT value FROM admin_settings WHERE key = $1`, key).Scan(&raw)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	return raw, err
}

func (r *storeRepository) SetMeta(ctx context.Context, key string, value json.RawMessage) error {
	return r.exec(ctx, `INSERT INTO admin_settings (key, value, updated_at) VALUES ($1, $2, now())
		ON CONFLICT (key) DO UPDATE SET value = EXCLUDED.value, updated_at = now()`, key, []byte(value))
}
