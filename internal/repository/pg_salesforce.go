package repository

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/warmbly/warmbly/internal/models"
)

// ErrSalesforceSourceRefs means an import source, its connection or its target
// campaign or categories are not in the organization.
var ErrSalesforceSourceRefs = errors.New("import source, connection, campaign or category not found")

// SalesforceConnectionRef is a live Salesforce connection the loops work on.
type SalesforceConnectionRef struct {
	ID                 uuid.UUID
	OrganizationID     uuid.UUID
	ConfigCapabilities json.RawMessage
	ConnectedByUserID  *uuid.UUID
}

// SalesforceContact is the slice of a contact the sync reads and writes.
type SalesforceContact struct {
	ID           uuid.UUID
	Email        string
	FirstName    string
	LastName     string
	Company      string
	Phone        string
	CustomFields map[string]string
	Subscribed   bool
}

// SalesforceEngagement is a contact's campaign activity, for engagement fields.
type SalesforceEngagement struct {
	LastCampaign string
	LastSentAt   *time.Time
	LastReplyAt  *time.Time
	ReplyIntent  string
	Status       string
}

// SalesforceSentContent is the stored copy of one sent campaign email.
type SalesforceSentContent struct {
	Subject   string
	FromEmail string
	Status    string
	Campaign  string
	Step      string
	SentAt    time.Time
}

// SalesforceActivityFilter scopes an activity-log page.
type SalesforceActivityFilter struct {
	Status    string
	ContactID *uuid.UUID
	Before    *time.Time
	BeforeID  *uuid.UUID
	Limit     int
}

// SalesforceRepository is the native Salesforce sync's storage.
type SalesforceRepository interface {
	ActiveConnections(ctx context.Context) ([]SalesforceConnectionRef, error)
	ActiveConnectionsForOrg(ctx context.Context, orgID uuid.UUID) ([]SalesforceConnectionRef, error)

	LinksForContact(ctx context.Context, orgID, contactID uuid.UUID) ([]models.SalesforceRecordLink, error)
	LinksForContacts(ctx context.Context, connID uuid.UUID, contactIDs []uuid.UUID) (map[uuid.UUID]models.SalesforceRecordLink, error)
	LinksForRecords(ctx context.Context, connID uuid.UUID, recordIDs []string) ([]models.SalesforceRecordLink, error)
	UpsertLink(ctx context.Context, l *models.SalesforceRecordLink) error
	SetLinkError(ctx context.Context, id uuid.UUID, msg string) error
	MarkLinksPushed(ctx context.Context, ids []uuid.UUID) error
	DeleteLink(ctx context.Context, orgID, id uuid.UUID) (bool, error)
	LinkedRecordIDs(ctx context.Context, connID uuid.UUID, recordIDs []string) (map[string]bool, error)

	EnqueueActivity(ctx context.Context, a *models.SalesforceActivity) error
	ClaimDueActivities(ctx context.Context, limit int, lease time.Duration) ([]models.SalesforceActivity, error)
	ClaimDueForConnection(ctx context.Context, connID uuid.UUID, limit int, lease time.Duration) ([]models.SalesforceActivity, error)
	MakeDue(ctx context.Context, orgID, connID uuid.UUID) error
	FinishActivity(ctx context.Context, a *models.SalesforceActivity) error
	ListActivities(ctx context.Context, orgID, connID uuid.UUID, f SalesforceActivityFilter) ([]models.SalesforceActivity, error)
	RetryActivities(ctx context.Context, orgID, connID uuid.UUID, ids []uuid.UUID) (int, error)
	ActivityCounts(ctx context.Context, connID uuid.UUID) (models.SalesforceActivityCounts, error)
	ContactActivityCounts(ctx context.Context, orgID, contactID uuid.UUID) (pending, failed, synced int, lastError string, err error)
	PruneActivities(ctx context.Context, before time.Time) (int64, error)

	CreateSource(ctx context.Context, s *models.SalesforceImportSource) error
	GetSource(ctx context.Context, orgID, id uuid.UUID) (*models.SalesforceImportSource, error)
	ListSources(ctx context.Context, orgID, connID uuid.UUID) ([]models.SalesforceImportSource, error)
	UpdateSource(ctx context.Context, s *models.SalesforceImportSource) error
	DeleteSource(ctx context.Context, orgID, id uuid.UUID) (bool, error)
	ClaimSource(ctx context.Context, id uuid.UUID) (bool, error)
	FinishSource(ctx context.Context, id uuid.UUID, result *models.SalesforceRunResult, errMsg string) error
	DueRecurringSources(ctx context.Context, every time.Duration) ([]models.SalesforceImportSource, error)
	SourceMembers(ctx context.Context, sourceID uuid.UUID, recordIDs []string) (map[string]bool, error)
	AddSourceMembers(ctx context.Context, sourceID uuid.UUID, members map[string]uuid.UUID) error
	LinksForAccounts(ctx context.Context, connID uuid.UUID, accountIDs []string) ([]models.SalesforceRecordLink, error)

	GetSyncState(ctx context.Context, connID uuid.UUID) (*models.SalesforceSyncState, error)
	EnsureSyncState(ctx context.Context, connID, orgID uuid.UUID, start time.Time) error
	SetCursors(ctx context.Context, connID uuid.UUID, lead, contact, opp *time.Time, pullErr string) error
	RecordUsage(ctx context.Context, connID uuid.UUID, used, max, calls int) error

	ContactsByEmails(ctx context.Context, orgID uuid.UUID, emails []string) (map[string]SalesforceContact, error)
	ContactsByIDs(ctx context.Context, orgID uuid.UUID, ids []uuid.UUID) (map[uuid.UUID]SalesforceContact, error)
	Engagement(ctx context.Context, orgID uuid.UUID, contactIDs []uuid.UUID) (map[uuid.UUID]SalesforceEngagement, error)
	SentContent(ctx context.Context, orgID, taskID uuid.UUID) (*SalesforceSentContent, error)
	MailboxEmail(ctx context.Context, orgID, accountID uuid.UUID) (string, error)
}

type salesforceRepository struct {
	db *pgxpool.Pool
}

// NewSalesforceRepository builds the Postgres-backed repository.
func NewSalesforceRepository(db *pgxpool.Pool) SalesforceRepository {
	return &salesforceRepository{db: db}
}

func (r *salesforceRepository) activeConnections(ctx context.Context, where string, args ...any) ([]SalesforceConnectionRef, error) {
	rows, err := r.db.Query(ctx, `
		SELECT id, organization_id, COALESCE(config_capabilities, '{}'::jsonb), connected_by_user_id
		FROM integration_connections
		WHERE provider = 'salesforce' AND status IN ('connected', 'degraded')`+where+`
		ORDER BY created_at`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []SalesforceConnectionRef
	for rows.Next() {
		var c SalesforceConnectionRef
		if err := rows.Scan(&c.ID, &c.OrganizationID, &c.ConfigCapabilities, &c.ConnectedByUserID); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

func (r *salesforceRepository) ActiveConnections(ctx context.Context) ([]SalesforceConnectionRef, error) {
	return r.activeConnections(ctx, "")
}

func (r *salesforceRepository) ActiveConnectionsForOrg(ctx context.Context, orgID uuid.UUID) ([]SalesforceConnectionRef, error) {
	return r.activeConnections(ctx, " AND organization_id = $1", orgID)
}

// --- links ------------------------------------------------------------------

const linkColumns = `id, organization_id, connection_id, contact_id, sobject, record_id,
	COALESCE(account_id, ''), account_name, COALESCE(owner_id, ''), owner_name, lead_status,
	is_converted, opted_out, snapshot, linked_by, record_modified_at, last_pushed_at,
	last_pulled_at, last_error, last_error_at, created_at, updated_at`

func scanSFLink(row pgx.Row) (models.SalesforceRecordLink, error) {
	var l models.SalesforceRecordLink
	err := row.Scan(&l.ID, &l.OrganizationID, &l.ConnectionID, &l.ContactID, &l.SObject, &l.RecordID,
		&l.AccountID, &l.AccountName, &l.OwnerID, &l.OwnerName, &l.LeadStatus,
		&l.IsConverted, &l.OptedOut, &l.Snapshot, &l.LinkedBy, &l.RecordModifiedAt, &l.LastPushedAt,
		&l.LastPulledAt, &l.LastError, &l.LastErrorAt, &l.CreatedAt, &l.UpdatedAt)
	return l, err
}

func (r *salesforceRepository) queryLinks(ctx context.Context, q string, args ...any) ([]models.SalesforceRecordLink, error) {
	rows, err := r.db.Query(ctx, `SELECT `+linkColumns+` FROM salesforce_record_links `+q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []models.SalesforceRecordLink
	for rows.Next() {
		l, err := scanSFLink(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, l)
	}
	return out, rows.Err()
}

func (r *salesforceRepository) LinksForContact(ctx context.Context, orgID, contactID uuid.UUID) ([]models.SalesforceRecordLink, error) {
	return r.queryLinks(ctx, `WHERE organization_id = $1 AND contact_id = $2 ORDER BY created_at`, orgID, contactID)
}

func (r *salesforceRepository) LinksForContacts(ctx context.Context, connID uuid.UUID, contactIDs []uuid.UUID) (map[uuid.UUID]models.SalesforceRecordLink, error) {
	out := map[uuid.UUID]models.SalesforceRecordLink{}
	if len(contactIDs) == 0 {
		return out, nil
	}
	links, err := r.queryLinks(ctx, `WHERE connection_id = $1 AND contact_id = ANY($2)`, connID, contactIDs)
	if err != nil {
		return nil, err
	}
	for _, l := range links {
		out[l.ContactID] = l
	}
	return out, nil
}

func (r *salesforceRepository) LinksForRecords(ctx context.Context, connID uuid.UUID, recordIDs []string) ([]models.SalesforceRecordLink, error) {
	if len(recordIDs) == 0 {
		return nil, nil
	}
	return r.queryLinks(ctx, `WHERE connection_id = $1 AND record_id = ANY($2)`, connID, recordIDs)
}

func (r *salesforceRepository) LinkedRecordIDs(ctx context.Context, connID uuid.UUID, recordIDs []string) (map[string]bool, error) {
	out := map[string]bool{}
	if len(recordIDs) == 0 {
		return out, nil
	}
	rows, err := r.db.Query(ctx, `SELECT DISTINCT record_id FROM salesforce_record_links WHERE connection_id = $1 AND record_id = ANY($2)`, connID, recordIDs)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out[id] = true
	}
	return out, rows.Err()
}

// UpsertLink writes a link keyed by (connection, contact). The contact must be
// in the link's organization; a mismatched pair writes nothing.
func (r *salesforceRepository) UpsertLink(ctx context.Context, l *models.SalesforceRecordLink) error {
	snap := l.Snapshot
	if len(snap) == 0 {
		snap = json.RawMessage(`{}`)
	}
	if l.LinkedBy == "" {
		l.LinkedBy = "match"
	}
	row := r.db.QueryRow(ctx, `
		INSERT INTO salesforce_record_links (
			organization_id, connection_id, contact_id, sobject, record_id, account_id, account_name,
			owner_id, owner_name, lead_status, is_converted, opted_out, snapshot, linked_by,
			record_modified_at, last_pulled_at
		)
		SELECT $1, $2, c.id, $4, $5, NULLIF($6, ''), $7, NULLIF($8, ''), $9, $10, $11, $12, $13, $14, $15, NOW()
		FROM contacts c
		WHERE c.id = $3 AND c.organization_id = $1
		ON CONFLICT (connection_id, contact_id) DO UPDATE SET
			sobject = EXCLUDED.sobject,
			record_id = EXCLUDED.record_id,
			account_id = EXCLUDED.account_id,
			account_name = EXCLUDED.account_name,
			owner_id = EXCLUDED.owner_id,
			owner_name = EXCLUDED.owner_name,
			lead_status = EXCLUDED.lead_status,
			is_converted = EXCLUDED.is_converted,
			opted_out = EXCLUDED.opted_out,
			snapshot = EXCLUDED.snapshot,
			record_modified_at = EXCLUDED.record_modified_at,
			last_pulled_at = NOW(),
			last_error = NULL,
			last_error_at = NULL,
			updated_at = NOW()
		RETURNING id, linked_by, created_at, updated_at`,
		l.OrganizationID, l.ConnectionID, l.ContactID, l.SObject, l.RecordID, l.AccountID, l.AccountName,
		l.OwnerID, l.OwnerName, l.LeadStatus, l.IsConverted, l.OptedOut, snap, l.LinkedBy, l.RecordModifiedAt)
	err := row.Scan(&l.ID, &l.LinkedBy, &l.CreatedAt, &l.UpdatedAt)
	if isNoRows(err) {
		return fmt.Errorf("contact %s is not in this organization", l.ContactID)
	}
	return err
}

func (r *salesforceRepository) SetLinkError(ctx context.Context, id uuid.UUID, msg string) error {
	_, err := r.db.Exec(ctx, `UPDATE salesforce_record_links SET last_error = NULLIF($2, ''), last_error_at = CASE WHEN $2 = '' THEN NULL ELSE NOW() END, updated_at = NOW() WHERE id = $1`, id, sfTruncate(msg, 500))
	return err
}

func (r *salesforceRepository) MarkLinksPushed(ctx context.Context, ids []uuid.UUID) error {
	if len(ids) == 0 {
		return nil
	}
	_, err := r.db.Exec(ctx, `UPDATE salesforce_record_links SET last_pushed_at = NOW(), last_error = NULL, last_error_at = NULL WHERE id = ANY($1)`, ids)
	return err
}

func (r *salesforceRepository) DeleteLink(ctx context.Context, orgID, id uuid.UUID) (bool, error) {
	tag, err := r.db.Exec(ctx, `DELETE FROM salesforce_record_links WHERE organization_id = $1 AND id = $2`, orgID, id)
	if err != nil {
		return false, err
	}
	return tag.RowsAffected() > 0, nil
}

// --- activity outbox --------------------------------------------------------

const activityColumns = `id, organization_id, connection_id, contact_id, contact_email, kind, dedupe_key,
	payload, content_encrypted, status, attempts, lease_id, next_attempt_at, COALESCE(sf_record_id, ''),
	COALESCE(sf_task_id, ''), detail, occurred_at, created_at, processed_at`

func scanActivity(row pgx.Row) (models.SalesforceActivity, error) {
	var a models.SalesforceActivity
	var payload []byte
	err := row.Scan(&a.ID, &a.OrganizationID, &a.ConnectionID, &a.ContactID, &a.ContactEmail, &a.Kind, &a.DedupeKey,
		&payload, &a.ContentEncrypted, &a.Status, &a.Attempts, &a.LeaseID, &a.NextAttemptAt, &a.RecordID,
		&a.TaskID, &a.Detail, &a.OccurredAt, &a.CreatedAt, &a.ProcessedAt)
	if err == nil && len(payload) > 0 {
		_ = json.Unmarshal(payload, &a.Payload)
	}
	return a, err
}

// EnqueueActivity records an event once per (connection, dedupe key); a repeat
// of the same event is a no-op. The contact must be in the organization.
func (r *salesforceRepository) EnqueueActivity(ctx context.Context, a *models.SalesforceActivity) error {
	payload, err := json.Marshal(a.Payload)
	if err != nil {
		return err
	}
	_, err = r.db.Exec(ctx, `
		INSERT INTO salesforce_activity_queue (
			organization_id, connection_id, contact_id, contact_email, kind, dedupe_key,
			payload, content_encrypted, occurred_at
		)
		SELECT $1, $2,
		       (SELECT id FROM contacts WHERE id = $3 AND organization_id = $1),
		       $4, $5, $6, $7, $8, $9
		WHERE EXISTS (SELECT 1 FROM integration_connections WHERE id = $2 AND organization_id = $1)
		ON CONFLICT (connection_id, dedupe_key) DO NOTHING`,
		a.OrganizationID, a.ConnectionID, a.ContactID, strings.ToLower(strings.TrimSpace(a.ContactEmail)),
		a.Kind, a.DedupeKey, payload, a.ContentEncrypted, a.OccurredAt)
	return err
}

// ClaimDueActivities leases due rows to one drain pass. A second replica skips
// them, and a pass that outlives its lease cannot write over the one that
// re-claimed them: FinishActivity only lands while the lease is still its own.
func (r *salesforceRepository) ClaimDueActivities(ctx context.Context, limit int, lease time.Duration) ([]models.SalesforceActivity, error) {
	return r.claim(ctx, "", nil, limit, lease)
}

// ClaimDueForConnection leases one connection's due rows.
func (r *salesforceRepository) ClaimDueForConnection(ctx context.Context, connID uuid.UUID, limit int, lease time.Duration) ([]models.SalesforceActivity, error) {
	return r.claim(ctx, " AND connection_id = $4", &connID, limit, lease)
}

func (r *salesforceRepository) claim(ctx context.Context, extra string, connID *uuid.UUID, limit int, lease time.Duration) ([]models.SalesforceActivity, error) {
	args := []any{limit, lease.Seconds(), uuid.New()}
	if connID != nil {
		args = append(args, *connID)
	}
	rows, err := r.db.Query(ctx, `
		WITH due AS (
			SELECT id FROM salesforce_activity_queue
			WHERE status = 'pending' AND next_attempt_at <= NOW()`+extra+`
			ORDER BY next_attempt_at
			LIMIT $1
			FOR UPDATE SKIP LOCKED
		)
		UPDATE salesforce_activity_queue q
		SET next_attempt_at = NOW() + make_interval(secs => $2), lease_id = $3
		FROM due WHERE q.id = due.id
		RETURNING q.id, q.organization_id, q.connection_id, q.contact_id, q.contact_email, q.kind, q.dedupe_key,
			q.payload, q.content_encrypted, q.status, q.attempts, q.lease_id, q.next_attempt_at, COALESCE(q.sf_record_id, ''),
			COALESCE(q.sf_task_id, ''), q.detail, q.occurred_at, q.created_at, q.processed_at`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []models.SalesforceActivity
	for rows.Next() {
		a, err := scanActivity(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// MakeDue brings a connection's waiting rows forward, for "Sync now".
func (r *salesforceRepository) MakeDue(ctx context.Context, orgID, connID uuid.UUID) error {
	_, err := r.db.Exec(ctx, `UPDATE salesforce_activity_queue SET next_attempt_at = NOW()
		WHERE organization_id = $1 AND connection_id = $2 AND status = 'pending' AND next_attempt_at > NOW()`, orgID, connID)
	return err
}

func (r *salesforceRepository) FinishActivity(ctx context.Context, a *models.SalesforceActivity) error {
	_, err := r.db.Exec(ctx, `
		UPDATE salesforce_activity_queue SET
			status = $2, attempts = $3, next_attempt_at = $4, sf_record_id = NULLIF($5, ''),
			sf_task_id = NULLIF($6, ''), detail = $7, lease_id = NULL,
			processed_at = CASE WHEN $2 = 'pending' THEN processed_at ELSE NOW() END
		WHERE id = $1 AND lease_id IS NOT DISTINCT FROM $8`,
		a.ID, a.Status, a.Attempts, a.NextAttemptAt, a.RecordID, a.TaskID, sfTruncate(a.Detail, 1000), a.LeaseID)
	return err
}

func (r *salesforceRepository) ListActivities(ctx context.Context, orgID, connID uuid.UUID, f SalesforceActivityFilter) ([]models.SalesforceActivity, error) {
	if f.Limit <= 0 || f.Limit > 200 {
		f.Limit = 50
	}
	args := []any{orgID, connID}
	where := []string{"organization_id = $1", "connection_id = $2"}
	if f.Status != "" {
		args = append(args, f.Status)
		where = append(where, fmt.Sprintf("status = $%d", len(args)))
	}
	if f.ContactID != nil {
		args = append(args, *f.ContactID)
		where = append(where, fmt.Sprintf("contact_id = $%d", len(args)))
	}
	if f.Before != nil && f.BeforeID != nil {
		args = append(args, *f.Before, *f.BeforeID)
		where = append(where, fmt.Sprintf("(created_at, id) < ($%d, $%d)", len(args)-1, len(args)))
	}
	args = append(args, f.Limit+1)
	rows, err := r.db.Query(ctx, `SELECT `+activityColumns+` FROM salesforce_activity_queue WHERE `+
		strings.Join(where, " AND ")+fmt.Sprintf(` ORDER BY created_at DESC, id DESC LIMIT $%d`, len(args)), args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []models.SalesforceActivity
	for rows.Next() {
		a, err := scanActivity(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// RetryActivities re-queues failed rows: the given ones, or every failed row
// on the connection when ids is empty.
func (r *salesforceRepository) RetryActivities(ctx context.Context, orgID, connID uuid.UUID, ids []uuid.UUID) (int, error) {
	q := `UPDATE salesforce_activity_queue SET status = 'pending', attempts = 0, next_attempt_at = NOW(), detail = '', lease_id = NULL
		WHERE organization_id = $1 AND connection_id = $2 AND status IN ('failed', 'skipped')`
	args := []any{orgID, connID}
	if len(ids) > 0 {
		q += ` AND id = ANY($3)`
		args = append(args, ids)
	} else {
		q += ` AND status = 'failed'`
	}
	tag, err := r.db.Exec(ctx, q, args...)
	if err != nil {
		return 0, err
	}
	return int(tag.RowsAffected()), nil
}

func (r *salesforceRepository) ActivityCounts(ctx context.Context, connID uuid.UUID) (models.SalesforceActivityCounts, error) {
	var c models.SalesforceActivityCounts
	err := r.db.QueryRow(ctx, `
		SELECT
			COUNT(*) FILTER (WHERE status = 'pending'),
			COUNT(*) FILTER (WHERE status = 'synced' AND processed_at > NOW() - INTERVAL '24 hours'),
			COUNT(*) FILTER (WHERE status = 'failed'),
			COUNT(*) FILTER (WHERE status = 'skipped' AND processed_at > NOW() - INTERVAL '24 hours'),
			(SELECT COUNT(*) FROM salesforce_record_links WHERE connection_id = $1)
		FROM salesforce_activity_queue WHERE connection_id = $1`, connID).
		Scan(&c.Pending, &c.Synced24h, &c.Failed, &c.Skipped24h, &c.LinkedTotal)
	return c, err
}

func (r *salesforceRepository) ContactActivityCounts(ctx context.Context, orgID, contactID uuid.UUID) (pending, failed, synced int, lastError string, err error) {
	err = r.db.QueryRow(ctx, `
		SELECT
			COUNT(*) FILTER (WHERE status = 'pending'),
			COUNT(*) FILTER (WHERE status = 'failed'),
			COUNT(*) FILTER (WHERE status = 'synced'),
			COALESCE((SELECT detail FROM salesforce_activity_queue
			          WHERE organization_id = $1 AND contact_id = $2 AND status = 'failed'
			          ORDER BY processed_at DESC NULLS LAST LIMIT 1), '')
		FROM salesforce_activity_queue WHERE organization_id = $1 AND contact_id = $2`, orgID, contactID).
		Scan(&pending, &failed, &synced, &lastError)
	return
}

func (r *salesforceRepository) PruneActivities(ctx context.Context, before time.Time) (int64, error) {
	tag, err := r.db.Exec(ctx, `DELETE FROM salesforce_activity_queue WHERE status <> 'pending' AND created_at < $1`, before)
	if err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
}

// --- import sources ---------------------------------------------------------

const sourceColumns = `id, organization_id, connection_id, created_by_user_id, name, source_kind, sobject,
	source_id, source_label, campaign_id, category_ids, recurring, enabled, status, last_run_at,
	last_result, last_error, total_imported, created_at, updated_at, segment_ids`

func scanSource(row pgx.Row) (models.SalesforceImportSource, error) {
	var s models.SalesforceImportSource
	var result []byte
	err := row.Scan(&s.ID, &s.OrganizationID, &s.ConnectionID, &s.CreatedByUserID, &s.Name, &s.SourceKind, &s.SObject,
		&s.SourceID, &s.SourceLabel, &s.CampaignID, &s.CategoryIDs, &s.Recurring, &s.Enabled, &s.Status, &s.LastRunAt,
		&result, &s.LastError, &s.TotalImported, &s.CreatedAt, &s.UpdatedAt, &s.SegmentIDs)
	if err == nil && len(result) > 0 {
		var rr models.SalesforceRunResult
		if json.Unmarshal(result, &rr) == nil {
			s.LastResult = &rr
		}
	}
	if s.CategoryIDs == nil {
		s.CategoryIDs = []uuid.UUID{}
	}
	if s.SegmentIDs == nil {
		s.SegmentIDs = []uuid.UUID{}
	}
	return s, err
}

func (r *salesforceRepository) CreateSource(ctx context.Context, s *models.SalesforceImportSource) error {
	if s.CategoryIDs == nil {
		s.CategoryIDs = []uuid.UUID{}
	}
	if s.SegmentIDs == nil {
		s.SegmentIDs = []uuid.UUID{}
	}
	row := r.db.QueryRow(ctx, `
		INSERT INTO salesforce_import_sources (
			organization_id, connection_id, created_by_user_id, name, source_kind, sobject, source_id,
			source_label, campaign_id, category_ids, recurring, enabled, segment_ids
		)
		SELECT $1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13
		WHERE EXISTS (SELECT 1 FROM integration_connections WHERE id = $2 AND organization_id = $1)
		  AND ($9::uuid IS NULL OR EXISTS (SELECT 1 FROM campaigns WHERE id = $9 AND organization_id = $1))
		  AND NOT EXISTS (SELECT 1 FROM unnest($10::uuid[]) c(id) WHERE NOT EXISTS (SELECT 1 FROM categories cat WHERE cat.id = c.id AND cat.organization_id = $1))
		  AND NOT EXISTS (SELECT 1 FROM unnest($13::uuid[]) g(id) WHERE NOT EXISTS (SELECT 1 FROM segments seg WHERE seg.id = g.id AND seg.organization_id = $1))
		RETURNING `+sourceColumns,
		s.OrganizationID, s.ConnectionID, s.CreatedByUserID, s.Name, s.SourceKind, s.SObject, s.SourceID,
		s.SourceLabel, s.CampaignID, s.CategoryIDs, s.Recurring, s.Enabled, s.SegmentIDs)
	out, err := scanSource(row)
	if isNoRows(err) {
		return ErrSalesforceSourceRefs
	}
	if err != nil {
		return err
	}
	*s = out
	return nil
}

func (r *salesforceRepository) GetSource(ctx context.Context, orgID, id uuid.UUID) (*models.SalesforceImportSource, error) {
	s, err := scanSource(r.db.QueryRow(ctx, `SELECT `+sourceColumns+` FROM salesforce_import_sources WHERE organization_id = $1 AND id = $2`, orgID, id))
	if isNoRows(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &s, nil
}

func (r *salesforceRepository) ListSources(ctx context.Context, orgID, connID uuid.UUID) ([]models.SalesforceImportSource, error) {
	rows, err := r.db.Query(ctx, `SELECT `+sourceColumns+` FROM salesforce_import_sources WHERE organization_id = $1 AND connection_id = $2 ORDER BY created_at DESC`, orgID, connID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []models.SalesforceImportSource{}
	for rows.Next() {
		s, err := scanSource(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

func (r *salesforceRepository) UpdateSource(ctx context.Context, s *models.SalesforceImportSource) error {
	tag, err := r.db.Exec(ctx, `
		UPDATE salesforce_import_sources SET
			name = $3, campaign_id = $4, category_ids = $5, recurring = $6, enabled = $7, segment_ids = $8,
			updated_at = NOW()
		WHERE organization_id = $1 AND id = $2
		  AND ($4::uuid IS NULL OR EXISTS (SELECT 1 FROM campaigns WHERE id = $4 AND organization_id = $1))
		  AND NOT EXISTS (SELECT 1 FROM unnest($5::uuid[]) c(id) WHERE c.id <> ALL(category_ids) AND NOT EXISTS (SELECT 1 FROM categories cat WHERE cat.id = c.id AND cat.organization_id = $1))
		  AND NOT EXISTS (SELECT 1 FROM unnest($8::uuid[]) g(id) WHERE g.id <> ALL(segment_ids) AND NOT EXISTS (SELECT 1 FROM segments seg WHERE seg.id = g.id AND seg.organization_id = $1))`,
		s.OrganizationID, s.ID, s.Name, s.CampaignID, s.CategoryIDs, s.Recurring, s.Enabled, s.SegmentIDs)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrSalesforceSourceRefs
	}
	return nil
}

func (r *salesforceRepository) DeleteSource(ctx context.Context, orgID, id uuid.UUID) (bool, error) {
	tag, err := r.db.Exec(ctx, `DELETE FROM salesforce_import_sources WHERE organization_id = $1 AND id = $2`, orgID, id)
	if err != nil {
		return false, err
	}
	return tag.RowsAffected() > 0, nil
}

// ClaimSource marks a source running unless it already is; a run that died
// mid-way is reclaimable after 30 minutes.
func (r *salesforceRepository) ClaimSource(ctx context.Context, id uuid.UUID) (bool, error) {
	tag, err := r.db.Exec(ctx, `
		UPDATE salesforce_import_sources SET status = 'running', updated_at = NOW()
		WHERE id = $1 AND (status <> 'running' OR updated_at < NOW() - INTERVAL '30 minutes')`, id)
	if err != nil {
		return false, err
	}
	return tag.RowsAffected() > 0, nil
}

func (r *salesforceRepository) FinishSource(ctx context.Context, id uuid.UUID, result *models.SalesforceRunResult, errMsg string) error {
	var raw []byte
	added := 0
	if result != nil {
		raw, _ = json.Marshal(result)
		added = result.Imported
	}
	status := "idle"
	if errMsg != "" {
		status = "error"
	}
	_, err := r.db.Exec(ctx, `
		UPDATE salesforce_import_sources SET
			status = $2, last_run_at = NOW(), last_result = COALESCE($3, last_result), last_error = $4,
			total_imported = total_imported + $5, updated_at = NOW()
		WHERE id = $1`, id, status, raw, sfTruncate(errMsg, 500), added)
	return err
}

func (r *salesforceRepository) DueRecurringSources(ctx context.Context, every time.Duration) ([]models.SalesforceImportSource, error) {
	rows, err := r.db.Query(ctx, `
		SELECT s.id, s.organization_id, s.connection_id, s.created_by_user_id, s.name, s.source_kind, s.sobject,
			s.source_id, s.source_label, s.campaign_id, s.category_ids, s.recurring, s.enabled, s.status, s.last_run_at,
			s.last_result, s.last_error, s.total_imported, s.created_at, s.updated_at, s.segment_ids
		FROM salesforce_import_sources s
		JOIN integration_connections c ON c.id = s.connection_id
		WHERE s.recurring AND s.enabled AND s.status <> 'running'
		  AND c.status IN ('connected', 'degraded')
		  AND (s.last_run_at IS NULL OR s.last_run_at < NOW() - make_interval(secs => $1))
		ORDER BY s.last_run_at NULLS FIRST
		LIMIT 50`, every.Seconds())
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []models.SalesforceImportSource
	for rows.Next() {
		s, err := scanSource(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

func (r *salesforceRepository) SourceMembers(ctx context.Context, sourceID uuid.UUID, recordIDs []string) (map[string]bool, error) {
	out := map[string]bool{}
	if len(recordIDs) == 0 {
		return out, nil
	}
	rows, err := r.db.Query(ctx, `SELECT record_id FROM salesforce_import_members WHERE source_id = $1 AND record_id = ANY($2)`, sourceID, recordIDs)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out[id] = true
	}
	return out, rows.Err()
}

// AddSourceMembers records records a source imported; contact may be uuid.Nil
// for a record that had no usable address.
func (r *salesforceRepository) AddSourceMembers(ctx context.Context, sourceID uuid.UUID, members map[string]uuid.UUID) error {
	if len(members) == 0 {
		return nil
	}
	ids := make([]string, 0, len(members))
	contacts := make([]string, 0, len(members))
	for rec, c := range members {
		ids = append(ids, rec)
		if c == uuid.Nil {
			contacts = append(contacts, "")
		} else {
			contacts = append(contacts, c.String())
		}
	}
	_, err := r.db.Exec(ctx, `
		INSERT INTO salesforce_import_members (source_id, record_id, contact_id)
		SELECT $1, rec, NULLIF(cid, '')::uuid FROM UNNEST($2::text[], $3::text[]) AS m(rec, cid)
		ON CONFLICT (source_id, record_id) DO UPDATE SET contact_id = COALESCE(EXCLUDED.contact_id, salesforce_import_members.contact_id)`,
		sourceID, ids, contacts)
	return err
}

func (r *salesforceRepository) LinksForAccounts(ctx context.Context, connID uuid.UUID, accountIDs []string) ([]models.SalesforceRecordLink, error) {
	if len(accountIDs) == 0 {
		return nil, nil
	}
	return r.queryLinks(ctx, `WHERE connection_id = $1 AND sobject = 'Contact' AND account_id = ANY($2)`, connID, accountIDs)
}

// --- sync state -------------------------------------------------------------

func (r *salesforceRepository) GetSyncState(ctx context.Context, connID uuid.UUID) (*models.SalesforceSyncState, error) {
	var s models.SalesforceSyncState
	err := r.db.QueryRow(ctx, `
		SELECT connection_id, organization_id, lead_cursor, contact_cursor, opportunity_cursor, last_pull_at, last_pull_error,
		       api_used, api_max, api_seen_at,
		       CASE WHEN calls_day = (NOW() AT TIME ZONE 'UTC')::date THEN calls_today ELSE 0 END
		FROM salesforce_sync_state WHERE connection_id = $1`, connID).
		Scan(&s.ConnectionID, &s.OrganizationID, &s.LeadCursor, &s.ContactCursor, &s.OppCursor, &s.LastPullAt, &s.LastPullError,
			&s.APIUsed, &s.APIMax, &s.APISeenAt, &s.CallsToday)
	if isNoRows(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &s, nil
}

// EnsureSyncState starts a connection's cursors at start, so the first pull
// reads changes from then on rather than the org's whole history.
func (r *salesforceRepository) EnsureSyncState(ctx context.Context, connID, orgID uuid.UUID, start time.Time) error {
	_, err := r.db.Exec(ctx, `
		INSERT INTO salesforce_sync_state (connection_id, organization_id, lead_cursor, contact_cursor, opportunity_cursor)
		SELECT $1, $2, $3, $3, $3
		WHERE EXISTS (SELECT 1 FROM integration_connections WHERE id = $1 AND organization_id = $2)
		ON CONFLICT (connection_id) DO NOTHING`, connID, orgID, start)
	return err
}

func (r *salesforceRepository) SetCursors(ctx context.Context, connID uuid.UUID, lead, contact, opp *time.Time, pullErr string) error {
	_, err := r.db.Exec(ctx, `
		UPDATE salesforce_sync_state SET
			lead_cursor = COALESCE($2, lead_cursor), contact_cursor = COALESCE($3, contact_cursor),
			opportunity_cursor = COALESCE($5, opportunity_cursor, NOW()),
			last_pull_at = NOW(), last_pull_error = $4, updated_at = NOW()
		WHERE connection_id = $1`, connID, lead, contact, sfTruncate(pullErr, 500), opp)
	return err
}

// RecordUsage stores the org's API reading and adds calls to today's count.
func (r *salesforceRepository) RecordUsage(ctx context.Context, connID uuid.UUID, used, max, calls int) error {
	_, err := r.db.Exec(ctx, `
		UPDATE salesforce_sync_state SET
			api_used = CASE WHEN $3 > 0 THEN $2 ELSE api_used END,
			api_max = CASE WHEN $3 > 0 THEN $3 ELSE api_max END,
			api_seen_at = CASE WHEN $3 > 0 THEN NOW() ELSE api_seen_at END,
			calls_today = CASE WHEN calls_day = (NOW() AT TIME ZONE 'UTC')::date THEN calls_today + $4 ELSE $4 END,
			calls_day = (NOW() AT TIME ZONE 'UTC')::date,
			updated_at = NOW()
		WHERE connection_id = $1`, connID, used, max, calls)
	return err
}

// --- contacts and campaign context ------------------------------------------

func (r *salesforceRepository) scanContacts(ctx context.Context, q string, args ...any) ([]SalesforceContact, error) {
	rows, err := r.db.Query(ctx, `
		SELECT id, email, COALESCE(first_name, ''), COALESCE(last_name, ''), COALESCE(company, ''),
		       COALESCE(phone, ''), COALESCE(custom_fields, '{}'::jsonb), subscribed
		FROM contacts `+q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []SalesforceContact
	for rows.Next() {
		var c SalesforceContact
		var custom []byte
		if err := rows.Scan(&c.ID, &c.Email, &c.FirstName, &c.LastName, &c.Company, &c.Phone, &custom, &c.Subscribed); err != nil {
			return nil, err
		}
		c.CustomFields = map[string]string{}
		_ = json.Unmarshal(custom, &c.CustomFields)
		out = append(out, c)
	}
	return out, rows.Err()
}

func (r *salesforceRepository) ContactsByEmails(ctx context.Context, orgID uuid.UUID, emails []string) (map[string]SalesforceContact, error) {
	out := map[string]SalesforceContact{}
	if len(emails) == 0 {
		return out, nil
	}
	lower := make([]string, len(emails))
	for i, e := range emails {
		lower[i] = strings.ToLower(strings.TrimSpace(e))
	}
	cs, err := r.scanContacts(ctx, `WHERE organization_id = $1 AND LOWER(email) = ANY($2)`, orgID, lower)
	if err != nil {
		return nil, err
	}
	for _, c := range cs {
		out[strings.ToLower(c.Email)] = c
	}
	return out, nil
}

func (r *salesforceRepository) ContactsByIDs(ctx context.Context, orgID uuid.UUID, ids []uuid.UUID) (map[uuid.UUID]SalesforceContact, error) {
	out := map[uuid.UUID]SalesforceContact{}
	if len(ids) == 0 {
		return out, nil
	}
	cs, err := r.scanContacts(ctx, `WHERE organization_id = $1 AND id = ANY($2)`, orgID, ids)
	if err != nil {
		return nil, err
	}
	for _, c := range cs {
		out[c.ID] = c
	}
	return out, nil
}

// Engagement summarises each contact's most recent campaign activity in the
// organization.
func (r *salesforceRepository) Engagement(ctx context.Context, orgID uuid.UUID, contactIDs []uuid.UUID) (map[uuid.UUID]SalesforceEngagement, error) {
	out := map[uuid.UUID]SalesforceEngagement{}
	if len(contactIDs) == 0 {
		return out, nil
	}
	rows, err := r.db.Query(ctx, `
		SELECT DISTINCT ON (ccp.contact_id)
			ccp.contact_id, cam.name,
			MAX(ccp.sent_at) OVER (PARTITION BY ccp.contact_id),
			MAX(ccp.replied_at) OVER (PARTITION BY ccp.contact_id),
			CASE
				WHEN BOOL_OR(ccp.replied_at IS NOT NULL) OVER (PARTITION BY ccp.contact_id) THEN 'replied'
				WHEN BOOL_OR(ccp.bounced_at IS NOT NULL) OVER (PARTITION BY ccp.contact_id) THEN 'bounced'
				WHEN cam.status = 'active' THEN 'in_sequence'
				ELSE 'finished'
			END
		FROM campaign_contact_progress ccp
		JOIN campaigns cam ON cam.id = ccp.campaign_id AND cam.organization_id = $1
		WHERE ccp.contact_id = ANY($2)
		ORDER BY ccp.contact_id, ccp.sent_at DESC NULLS LAST`, orgID, contactIDs)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var id uuid.UUID
		var e SalesforceEngagement
		if err := rows.Scan(&id, &e.LastCampaign, &e.LastSentAt, &e.LastReplyAt, &e.Status); err != nil {
			return nil, err
		}
		out[id] = e
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	intents, err := r.db.Query(ctx, `
		SELECT DISTINCT ON (c.id) c.id, ri.intent
		FROM contacts c
		JOIN reply_intents ri ON ri.organization_id = c.organization_id AND LOWER(ri.contact_email) = LOWER(c.email)
		WHERE c.organization_id = $1 AND c.id = ANY($2)
		ORDER BY c.id, ri.created_at DESC`, orgID, contactIDs)
	if err != nil {
		return out, nil
	}
	defer intents.Close()
	for intents.Next() {
		var id uuid.UUID
		var intent string
		if intents.Scan(&id, &intent) == nil {
			e := out[id]
			e.ReplyIntent = intent
			out[id] = e
		}
	}
	return out, nil
}

// SentContent reads what is stored about a campaign send in the organization:
// its rendered subject, where it came from, and whether it went out.
func (r *salesforceRepository) SentContent(ctx context.Context, orgID, taskID uuid.UUID) (*SalesforceSentContent, error) {
	var c SalesforceSentContent
	err := r.db.QueryRow(ctx, `
		SELECT COALESCE(ct.subject, ''), COALESCE(ea.email, ''), COALESCE(cam.name, ''),
		       COALESCE(seq.name, ''), t.status::text, t.created_at
		FROM tasks t
		JOIN campaign_tasks ct ON ct.task_id = t.id
		JOIN campaigns cam ON cam.id = ct.campaign_id AND cam.organization_id = $1
		LEFT JOIN sequences seq ON seq.id = ct.sequence_id
		LEFT JOIN email_accounts ea ON ea.id = t.email_account_id
		WHERE t.id = $2`, orgID, taskID).
		Scan(&c.Subject, &c.FromEmail, &c.Campaign, &c.Step, &c.Status, &c.SentAt)
	if isNoRows(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &c, nil
}

func (r *salesforceRepository) MailboxEmail(ctx context.Context, orgID, accountID uuid.UUID) (string, error) {
	var email string
	err := r.db.QueryRow(ctx, `SELECT email FROM email_accounts WHERE id = $1 AND organization_id = $2`, accountID, orgID).Scan(&email)
	if isNoRows(err) {
		return "", nil
	}
	return email, err
}

// sfTruncate cuts to at most n bytes without splitting a character, so a
// long localized message never becomes invalid UTF-8.
func sfTruncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	for n > 0 && !utf8.RuneStart(s[n]) {
		n--
	}
	return s[:n]
}
