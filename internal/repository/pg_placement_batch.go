package repository

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/warmbly/warmbly/internal/infrastructure/db"
	"github.com/warmbly/warmbly/internal/models"
)

// PlacementBatchCandidate is a mailbox a batch could test from.
type PlacementBatchCandidate struct {
	ID          uuid.UUID
	Email       string
	SendAsEmail string
	Provider    string
	MailHost    string
	Status      string
	WorkerID    *uuid.UUID
}

// SendFrom is the address the mailbox sends as.
func (c PlacementBatchCandidate) SendFrom() string {
	if c.SendAsEmail != "" {
		return c.SendAsEmail
	}
	return c.Email
}

// PlacementCandidateFilter narrows a workspace's mailboxes in SQL. Provider and
// domain filters are applied by the caller, which resolves the host family.
type PlacementCandidateFilter struct {
	// IDs keeps only these mailboxes; nil keeps every one.
	IDs             []uuid.UUID
	TagIDs          []uuid.UUID
	IncludeInactive bool
	// UntestedSince keeps mailboxes with no delivered test since then.
	UntestedSince *time.Time
}

// PlacementBatchSenderRow is a batch sender with where its copies landed.
type PlacementBatchSenderRow struct {
	models.PlacementBatchSender
	Counts  models.PlacementCounts
	TestIDs []uuid.UUID
}

// PlacementBatchSenderFilter narrows and orders a batch's sender listing.
type PlacementBatchSenderFilter struct {
	Status string
	Search string
	// Sort is worst (lowest inbox rate first), best, email or status.
	Sort   string
	Limit  int
	Offset int
}

// PlacementBreakdownRow is one cell of a batch's placement grouped by a
// dimension. Empty dimension fields are the ones the row is not grouped by.
type PlacementBreakdownRow struct {
	Set             string
	Tracked         bool
	SenderDomain    string
	SenderFamily    string
	RecipientFamily string
	Folder          string
	Count           int
}

// PlacementBatchGroupSize is how many senders a domain or provider has in a
// batch, and how many of them finished a test.
type PlacementBatchGroupSize struct {
	// ByDomain is a domain's row; otherwise it is a provider's.
	ByDomain  bool
	Domain    string
	Family    string
	Senders   int
	Completed int
}

// PlacementOrgTest names a test with its workspace.
type PlacementOrgTest struct {
	OrganizationID uuid.UUID
	TestID         uuid.UUID
}

// PlacementCoverage is how much of a workspace's fleet was tested recently.
type PlacementCoverage struct {
	Mailboxes   int `json:"mailboxes"`
	Tested7d    int `json:"tested_7d"`
	Tested30d   int `json:"tested_30d"`
	NeverTested int `json:"never_tested"`
}

// PlacementBatchRepository stores placement batches and their senders.
type PlacementBatchRepository interface {
	// ListBatchCandidates lists a workspace's mailboxes that can send a test:
	// never a seed.
	ListBatchCandidates(ctx context.Context, orgID uuid.UUID, f PlacementCandidateFilter) ([]PlacementBatchCandidate, error)
	// CreateBatch writes a batch and its sender snapshot in one transaction.
	CreateBatch(ctx context.Context, b *models.PlacementBatch, senders []models.PlacementBatchSender) error
	GetBatch(ctx context.Context, orgID, id uuid.UUID) (*models.PlacementBatch, error)
	// CountOpenBatches counts a workspace's batches still running.
	CountOpenBatches(ctx context.Context, orgID uuid.UUID) (int, error)
	ListBatches(ctx context.Context, orgID uuid.UUID, limit, offset int) ([]models.PlacementBatch, int, error)
	BatchProgress(ctx context.Context, ids []uuid.UUID) (map[uuid.UUID]models.PlacementBatchProgress, error)
	// BatchSummaries counts where every batch's copies landed, headline copy
	// only (the tracked half of a tracking comparison).
	BatchSummaries(ctx context.Context, ids []uuid.UUID) (map[uuid.UUID]models.PlacementCounts, error)

	// ClaimBatches leases the active batches no other replica holds and
	// returns each with the moment it was last advanced.
	ClaimBatches(ctx context.Context, now time.Time, lease time.Duration, limit int) ([]models.PlacementBatch, error)
	ReleaseBatch(ctx context.Context, id uuid.UUID) error
	// CloseInactiveBatches ends a batch that is not active yet still open:
	// one imported from another instance.
	CloseInactiveBatches(ctx context.Context) error
	// SyncBatchSenders resolves running senders whose tests all finished, and
	// returns a sender claimed but never started (a crash in between) to the
	// queue after staleAfter.
	SyncBatchSenders(ctx context.Context, batchID uuid.UUID, staleBefore time.Time) error
	// SyncClosedBatchSenders resolves the running senders of batches that are
	// no longer active (cancelled), once their tests finish.
	SyncClosedBatchSenders(ctx context.Context) error
	// RunningTestsOfCancelledBatches lists tests still running in a cancelled
	// batch: one a sender started while the batch was being cancelled.
	RunningTestsOfCancelledBatches(ctx context.Context, limit int) ([]PlacementOrgTest, error)
	// CountSendingBatchSenders counts batch senders still sending probes,
	// across one workspace's batches, or every workspace's when orgID is nil.
	CountSendingBatchSenders(ctx context.Context, orgID *uuid.UUID) (int, error)
	DueBatchSenders(ctx context.Context, batchID uuid.UUID, now time.Time, limit int) ([]models.PlacementBatchSender, error)
	// ClaimBatchSender marks a due sender running before its test is created,
	// so a restart can never start it twice. False when it was not due.
	ClaimBatchSender(ctx context.Context, id uuid.UUID) (bool, error)
	// SetBatchSenderOutcome records a sender that did not start.
	SetBatchSenderOutcome(ctx context.Context, id uuid.UUID, status, reason, detail string, next time.Time) error
	// CloseOpenBatchSenders gives every sender of a batch in one of the from
	// statuses (queued, deferred) one final status.
	CloseOpenBatchSenders(ctx context.Context, batchID uuid.UUID, from []string, status, reason, detail string) error
	AddBatchSpend(ctx context.Context, id uuid.UUID, spent models.Micro) error
	MarkBatchStarted(ctx context.Context, id uuid.UUID) error
	// FinishBatch closes an active batch; false when it was already closed.
	FinishBatch(ctx context.Context, id uuid.UUID, status, errMsg string) (bool, error)
	// CancelBatch closes a batch and its unstarted senders, returning the
	// running tests to cancel. False when it was not open.
	CancelBatch(ctx context.Context, orgID, id uuid.UUID) (bool, []uuid.UUID, error)

	ListBatchSenders(ctx context.Context, orgID, batchID uuid.UUID, f PlacementBatchSenderFilter) ([]PlacementBatchSenderRow, int, error)
	BatchBreakdown(ctx context.Context, batchID uuid.UUID) ([]PlacementBreakdownRow, error)
	BatchGroupSizes(ctx context.Context, batchID uuid.UUID) ([]PlacementBatchGroupSize, error)
	Coverage(ctx context.Context, orgID uuid.UUID, now time.Time) (PlacementCoverage, error)
}

type placementBatchRepository struct {
	db *db.DB
}

// NewPlacementBatchRepository wires the batch store.
func NewPlacementBatchRepository(db *db.DB) PlacementBatchRepository {
	return &placementBatchRepository{db: db}
}

// deliveredFolders are the folders a copy that left can land in.
const deliveredFolders = `('inbox', 'promotions', 'other', 'spam', 'missing')`

// headlineTest keeps the copy a campaign really sends: the tracked half of a
// tracking comparison, every test otherwise. b and t are the batch and test.
const headlineTest = `(b.tracking <> 'compare' OR t.open_tracking OR t.link_tracking)`

func (r *placementBatchRepository) ListBatchCandidates(ctx context.Context, orgID uuid.UUID, f PlacementCandidateFilter) ([]PlacementBatchCandidate, error) {
	rows, err := r.db.Query(ctx, `
		SELECT ea.id, ea.email, ea.send_as_email, ea.provider::text, ea.mail_host, ea.status::text, ea.worker_id
		FROM email_accounts ea
		WHERE ea.organization_id = $1
		  AND ea.seed_scope IS NULL
		  AND ($2::uuid[] IS NULL OR ea.id = ANY($2))
		  AND (cardinality($3::uuid[]) = 0 OR EXISTS (
			SELECT 1 FROM email_tags et WHERE et.email_id = ea.id AND et.tag_id = ANY($3)
		  ))
		  AND ($4 OR ea.status = 'active')
		  AND ($5::timestamptz IS NULL OR NOT EXISTS (
			SELECT 1 FROM placement_tests pt
			WHERE pt.sender_account_id = ea.id AND pt.organization_id = $1
			  AND pt.status = 'completed' AND pt.created_at >= $5
		  ))
		ORDER BY ea.email, ea.id
	`, orgID, f.IDs, nonNilUUIDs(f.TagIDs), f.IncludeInactive, f.UntestedSince)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []PlacementBatchCandidate
	for rows.Next() {
		var c PlacementBatchCandidate
		if err := rows.Scan(&c.ID, &c.Email, &c.SendAsEmail, &c.Provider, &c.MailHost, &c.Status, &c.WorkerID); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

func nonNilUUIDs(ids []uuid.UUID) []uuid.UUID {
	if ids == nil {
		return []uuid.UUID{}
	}
	return ids
}

const placementBatchCols = `id, organization_id, created_by, campaign_id, sequence_id, contact_id, subject,
	body_plain, body_html, tracking, panel, pace, families, seed_ids, on_unavailable, selection, sender_count,
	max_credits, credits_spent, max_micro, spent_micro, status, error, active, last_tick_at, retry_until, created_at, started_at, finished_at`

func scanPlacementBatch(row pgx.Row) (*models.PlacementBatch, error) {
	var b models.PlacementBatch
	var selection []byte
	err := row.Scan(&b.ID, &b.OrganizationID, &b.CreatedBy, &b.CampaignID, &b.SequenceID, &b.ContactID, &b.Subject,
		&b.BodyPlain, &b.BodyHTML, &b.Tracking, &b.Panel, &b.Pace, &b.Families, &b.SeedIDs, &b.OnUnavailable, &selection, &b.SenderCount,
		&b.MaxCredits, &b.CreditsSpent, &b.MaxMicro, &b.SpentMicro, &b.Status, &b.Error, &b.Active, &b.LastTickAt, &b.RetryUntil, &b.CreatedAt, &b.StartedAt, &b.FinishedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	// The previous release writes the credit columns only; this one writes
	// credits rounded down, so the larger figure is the real one.
	b.MaxMicro = max(b.MaxMicro, models.Micro(b.MaxCredits)*models.LegacyCreditMicro)
	b.SpentMicro = max(b.SpentMicro, models.Micro(b.CreditsSpent)*models.LegacyCreditMicro)
	b.MaxUSD, b.SpentUSD = b.MaxMicro.USD(), b.SpentMicro.USD()
	if len(selection) > 0 {
		// A selection that no longer parses only costs the display.
		_ = json.Unmarshal(selection, &b.Selection)
	}
	if b.Families == nil {
		b.Families = []string{}
	}
	if b.SeedIDs == nil {
		b.SeedIDs = []uuid.UUID{}
	}
	return &b, nil
}

func (r *placementBatchRepository) CreateBatch(ctx context.Context, b *models.PlacementBatch, senders []models.PlacementBatchSender) error {
	selection, err := json.Marshal(b.Selection)
	if err != nil {
		return err
	}
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	families := b.Families
	if families == nil {
		families = []string{}
	}
	err = tx.QueryRow(ctx, `
		INSERT INTO placement_batches (id, organization_id, created_by, campaign_id, sequence_id, contact_id, subject,
			body_plain, body_html, tracking, panel, pace, families, seed_ids, on_unavailable, selection, sender_count,
			max_credits, max_micro, status, active, retry_until)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18, $19, $20, true, $21)
		RETURNING created_at
	`, b.ID, b.OrganizationID, b.CreatedBy, b.CampaignID, b.SequenceID, b.ContactID, b.Subject,
		b.BodyPlain, b.BodyHTML, b.Tracking, b.Panel, b.Pace, families, nonNilUUIDs(b.SeedIDs), b.OnUnavailable, selection, b.SenderCount,
		models.CreditsFromMicro(b.MaxMicro), b.MaxMicro, b.Status, b.RetryUntil).Scan(&b.CreatedAt)
	if err != nil {
		return err
	}
	b.Active = true

	rows := make([][]any, len(senders))
	for i, s := range senders {
		rows[i] = []any{s.ID, b.ID, s.EmailAccountID, s.SenderEmail, s.SenderDomain, s.SenderFamily, s.Position, models.PlacementSenderQueued}
	}
	if _, err := tx.CopyFrom(ctx, pgx.Identifier{"placement_batch_senders"},
		[]string{"id", "batch_id", "email_account_id", "sender_email", "sender_domain", "sender_family", "position", "status"},
		pgx.CopyFromRows(rows)); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (r *placementBatchRepository) GetBatch(ctx context.Context, orgID, id uuid.UUID) (*models.PlacementBatch, error) {
	return scanPlacementBatch(r.db.QueryRow(ctx,
		`SELECT `+placementBatchCols+` FROM placement_batches WHERE id = $2 AND organization_id = $1`, orgID, id))
}

func (r *placementBatchRepository) CountOpenBatches(ctx context.Context, orgID uuid.UUID) (int, error) {
	var n int
	err := r.db.QueryRow(ctx, `SELECT COUNT(*) FROM placement_batches WHERE organization_id = $1 AND active`, orgID).Scan(&n)
	return n, err
}

func (r *placementBatchRepository) ListBatches(ctx context.Context, orgID uuid.UUID, limit, offset int) ([]models.PlacementBatch, int, error) {
	if limit <= 0 {
		limit = 25
	}
	var total int
	if err := r.db.QueryRow(ctx, `SELECT COUNT(*) FROM placement_batches WHERE organization_id = $1`, orgID).Scan(&total); err != nil {
		return nil, 0, err
	}
	rows, err := r.db.Query(ctx, `
		SELECT `+placementBatchCols+` FROM placement_batches
		WHERE organization_id = $1
		ORDER BY created_at DESC, id DESC
		LIMIT $2 OFFSET $3
	`, orgID, limit, offset)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	out := make([]models.PlacementBatch, 0, limit)
	for rows.Next() {
		b, err := scanPlacementBatch(rows)
		if err != nil {
			return nil, 0, err
		}
		out = append(out, *b)
	}
	return out, total, rows.Err()
}

func (r *placementBatchRepository) BatchProgress(ctx context.Context, ids []uuid.UUID) (map[uuid.UUID]models.PlacementBatchProgress, error) {
	out := make(map[uuid.UUID]models.PlacementBatchProgress, len(ids))
	if len(ids) == 0 {
		return out, nil
	}
	rows, err := r.db.Query(ctx, `
		SELECT batch_id, status, COUNT(*) FROM placement_batch_senders
		WHERE batch_id = ANY($1)
		GROUP BY batch_id, status
	`, ids)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var id uuid.UUID
		var status string
		var n int
		if err := rows.Scan(&id, &status, &n); err != nil {
			return nil, err
		}
		p := out[id]
		p.Add(status, n)
		out[id] = p
	}
	return out, rows.Err()
}

func (r *placementBatchRepository) BatchSummaries(ctx context.Context, ids []uuid.UUID) (map[uuid.UUID]models.PlacementCounts, error) {
	out := make(map[uuid.UUID]models.PlacementCounts, len(ids))
	if len(ids) == 0 {
		return out, nil
	}
	rows, err := r.db.Query(ctx, `
		SELECT b.id, r.folder, COUNT(*)
		FROM placement_batches b
		JOIN placement_tests t ON t.batch_id = b.id
		JOIN placement_results r ON r.test_id = t.id
		WHERE b.id = ANY($1) AND `+headlineTest+`
		GROUP BY b.id, r.folder
	`, ids)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var id uuid.UUID
		var folder string
		var n int
		if err := rows.Scan(&id, &folder, &n); err != nil {
			return nil, err
		}
		c := out[id]
		c.AddN(folder, n)
		out[id] = c
	}
	for id, c := range out {
		c.Finish()
		out[id] = c
	}
	return out, rows.Err()
}

func (r *placementBatchRepository) ClaimBatches(ctx context.Context, now time.Time, lease time.Duration, limit int) ([]models.PlacementBatch, error) {
	if limit <= 0 {
		limit = 20
	}
	rows, err := r.db.Query(ctx, `
		WITH due AS (
			SELECT id, last_tick_at AS prev FROM placement_batches
			WHERE active AND (lease_until IS NULL OR lease_until < $1)
			ORDER BY created_at
			LIMIT $3
			FOR UPDATE SKIP LOCKED
		)
		UPDATE placement_batches b
		SET lease_until = $1 + make_interval(secs => $2), last_tick_at = $1
		FROM due
		WHERE b.id = due.id
		RETURNING `+strings.Replace(prefixCols("b.", placementBatchCols), "b.last_tick_at", "due.prev", 1),
		now, lease.Seconds(), limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []models.PlacementBatch
	for rows.Next() {
		b, err := scanPlacementBatch(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *b)
	}
	return out, rows.Err()
}

func (r *placementBatchRepository) ReleaseBatch(ctx context.Context, id uuid.UUID) error {
	_, err := r.db.Exec(ctx, `UPDATE placement_batches SET lease_until = NULL WHERE id = $1`, id)
	return err
}

func (r *placementBatchRepository) CloseInactiveBatches(ctx context.Context) error {
	_, err := r.db.Exec(ctx, `
		WITH closed AS (
			UPDATE placement_batches
			SET status = 'cancelled', finished_at = COALESCE(finished_at, NOW()), updated_at = NOW(),
				error = 'The batch was moved from another instance before it finished.'
			WHERE NOT active AND status IN ('queued', 'running')
			RETURNING id
		)
		UPDATE placement_batch_senders SET status = 'cancelled', finished_at = NOW()
		WHERE batch_id IN (SELECT id FROM closed) AND status IN ('queued', 'deferred')
	`)
	return err
}

// resolveFinishedSenders sets a running sender whose tests have all finished
// to what they came to. The caller adds the WHERE condition choosing senders.
const resolveFinishedSenders = `
		UPDATE placement_batch_senders s
		SET status = CASE
				WHEN EXISTS (SELECT 1 FROM placement_tests t WHERE t.batch_sender_id = s.id AND t.status = 'completed') THEN 'completed'
				WHEN EXISTS (SELECT 1 FROM placement_tests t WHERE t.batch_sender_id = s.id AND t.status = 'cancelled') THEN 'cancelled'
				ELSE 'failed'
			END,
			detail = COALESCE((
				SELECT t.error FROM placement_tests t
				WHERE t.batch_sender_id = s.id AND t.status = 'failed' AND t.error <> ''
				LIMIT 1
			), ''),
			finished_at = NOW()
		WHERE s.status = 'running'
		  AND EXISTS (SELECT 1 FROM placement_tests t WHERE t.batch_sender_id = s.id)
		  AND NOT EXISTS (SELECT 1 FROM placement_tests t WHERE t.batch_sender_id = s.id AND t.finished_at IS NULL)
		  AND `

func (r *placementBatchRepository) SyncBatchSenders(ctx context.Context, batchID uuid.UUID, staleBefore time.Time) error {
	if _, err := r.db.Exec(ctx, resolveFinishedSenders+`s.batch_id = $1`, batchID); err != nil {
		return err
	}
	_, err := r.db.Exec(ctx, `
		UPDATE placement_batch_senders s
		SET status = 'deferred', next_attempt_at = NOW()
		WHERE s.batch_id = $1 AND s.status = 'running' AND s.started_at < $2
		  AND NOT EXISTS (SELECT 1 FROM placement_tests t WHERE t.batch_sender_id = s.id)
	`, batchID, staleBefore)
	return err
}

func (r *placementBatchRepository) SyncClosedBatchSenders(ctx context.Context) error {
	if _, err := r.db.Exec(ctx, resolveFinishedSenders+`s.batch_id IN (
		SELECT id FROM placement_batches WHERE NOT active AND status = 'cancelled'
	)`); err != nil {
		return err
	}
	// Claimed but never started before the cancel: nothing will start it now.
	_, err := r.db.Exec(ctx, `
		UPDATE placement_batch_senders s
		SET status = 'cancelled', detail = 'The batch was cancelled before this mailbox started.', finished_at = NOW()
		WHERE s.status = 'running'
		  AND s.batch_id IN (SELECT id FROM placement_batches WHERE NOT active AND status = 'cancelled')
		  AND NOT EXISTS (SELECT 1 FROM placement_tests t WHERE t.batch_sender_id = s.id)
		  AND s.started_at < NOW() - interval '10 minutes'
	`)
	return err
}

func (r *placementBatchRepository) RunningTestsOfCancelledBatches(ctx context.Context, limit int) ([]PlacementOrgTest, error) {
	rows, err := r.db.Query(ctx, `
		SELECT t.organization_id, t.id FROM placement_tests t
		JOIN placement_batches b ON b.id = t.batch_id
		WHERE b.status = 'cancelled' AND NOT b.active AND t.status = 'running' AND t.organization_id = b.organization_id
		ORDER BY t.created_at
		LIMIT $1
	`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []PlacementOrgTest
	for rows.Next() {
		var o PlacementOrgTest
		if err := rows.Scan(&o.OrganizationID, &o.TestID); err != nil {
			return nil, err
		}
		out = append(out, o)
	}
	return out, rows.Err()
}

func (r *placementBatchRepository) CountSendingBatchSenders(ctx context.Context, orgID *uuid.UUID) (int, error) {
	var n int
	err := r.db.QueryRow(ctx, `
		SELECT COUNT(*) FROM placement_batch_senders s
		JOIN placement_batches b ON b.id = s.batch_id
		WHERE ($1::uuid IS NULL OR b.organization_id = $1) AND b.active AND s.status = 'running'
		  AND (
			NOT EXISTS (SELECT 1 FROM placement_tests t WHERE t.batch_sender_id = s.id)
			OR EXISTS (
				SELECT 1 FROM placement_tests t
				JOIN placement_results pr ON pr.test_id = t.id
				WHERE t.batch_sender_id = s.id AND t.status = 'running'
				  AND pr.folder = 'pending' AND pr.sent_at IS NULL
			)
		  )
	`, orgID).Scan(&n)
	return n, err
}

const placementBatchSenderCols = `id, batch_id, email_account_id, sender_email, sender_domain, sender_family, position,
	status, reason, detail, attempts, next_attempt_at, started_at, finished_at`

func scanBatchSender(row pgx.Row, extra ...any) (models.PlacementBatchSender, error) {
	var s models.PlacementBatchSender
	dest := append([]any{&s.ID, &s.BatchID, &s.EmailAccountID, &s.SenderEmail, &s.SenderDomain, &s.SenderFamily, &s.Position,
		&s.Status, &s.Reason, &s.Detail, &s.Attempts, &s.NextAttemptAt, &s.StartedAt, &s.FinishedAt}, extra...)
	err := row.Scan(dest...)
	return s, err
}

func (r *placementBatchRepository) DueBatchSenders(ctx context.Context, batchID uuid.UUID, now time.Time, limit int) ([]models.PlacementBatchSender, error) {
	rows, err := r.db.Query(ctx, `
		SELECT `+placementBatchSenderCols+` FROM placement_batch_senders
		WHERE batch_id = $1 AND status IN ('queued', 'deferred') AND next_attempt_at <= $2
		ORDER BY position
		LIMIT $3
	`, batchID, now, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []models.PlacementBatchSender
	for rows.Next() {
		s, err := scanBatchSender(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

func (r *placementBatchRepository) ClaimBatchSender(ctx context.Context, id uuid.UUID) (bool, error) {
	tag, err := r.db.Exec(ctx, `
		UPDATE placement_batch_senders
		SET status = 'running', attempts = attempts + 1, started_at = NOW(), reason = '', detail = ''
		WHERE id = $1 AND status IN ('queued', 'deferred')
	`, id)
	if err != nil {
		return false, err
	}
	return tag.RowsAffected() == 1, nil
}

func (r *placementBatchRepository) SetBatchSenderOutcome(ctx context.Context, id uuid.UUID, status, reason, detail string, next time.Time) error {
	_, err := r.db.Exec(ctx, `
		UPDATE placement_batch_senders
		SET status = $2, reason = $3, detail = $4, next_attempt_at = $5,
			finished_at = CASE WHEN $2 IN ('skipped', 'failed', 'cancelled') THEN NOW() ELSE NULL END
		WHERE id = $1
	`, id, status, reason, truncateRunes(detail, 500), next)
	return err
}

func (r *placementBatchRepository) CloseOpenBatchSenders(ctx context.Context, batchID uuid.UUID, from []string, status, reason, detail string) error {
	_, err := r.db.Exec(ctx, `
		UPDATE placement_batch_senders
		SET status = $3, reason = $4, detail = $5, finished_at = NOW()
		WHERE batch_id = $1 AND status = ANY($2) AND status IN ('queued', 'deferred')
	`, batchID, from, status, reason, truncateRunes(detail, 500))
	return err
}

func (r *placementBatchRepository) AddBatchSpend(ctx context.Context, id uuid.UUID, spent models.Micro) error {
	_, err := r.db.Exec(ctx, `
		UPDATE placement_batches SET credits_spent = credits_spent + $2, spent_micro = spent_micro + $3, updated_at = NOW() WHERE id = $1
	`, id, models.CreditsFromMicro(spent), spent)
	return err
}

func (r *placementBatchRepository) MarkBatchStarted(ctx context.Context, id uuid.UUID) error {
	_, err := r.db.Exec(ctx, `
		UPDATE placement_batches SET status = 'running', started_at = COALESCE(started_at, NOW()), updated_at = NOW()
		WHERE id = $1 AND status = 'queued'
	`, id)
	return err
}

func (r *placementBatchRepository) FinishBatch(ctx context.Context, id uuid.UUID, status, errMsg string) (bool, error) {
	tag, err := r.db.Exec(ctx, `
		UPDATE placement_batches
		SET status = $2, error = $3, active = false, lease_until = NULL, finished_at = NOW(), updated_at = NOW()
		WHERE id = $1 AND active
	`, id, status, truncateRunes(errMsg, 500))
	if err != nil {
		return false, err
	}
	return tag.RowsAffected() == 1, nil
}

func (r *placementBatchRepository) CancelBatch(ctx context.Context, orgID, id uuid.UUID) (bool, []uuid.UUID, error) {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return false, nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	tag, err := tx.Exec(ctx, `
		UPDATE placement_batches
		SET status = 'cancelled', active = false, lease_until = NULL, finished_at = NOW(), updated_at = NOW()
		WHERE id = $2 AND organization_id = $1 AND active
	`, orgID, id)
	if err != nil {
		return false, nil, err
	}
	if tag.RowsAffected() == 0 {
		return false, nil, nil
	}
	if _, err := tx.Exec(ctx, `
		UPDATE placement_batch_senders
		SET status = 'cancelled', reason = '', detail = 'The batch was cancelled before this mailbox started.', finished_at = NOW()
		WHERE batch_id = $1 AND status IN ('queued', 'deferred')
	`, id); err != nil {
		return false, nil, err
	}
	rows, err := tx.Query(ctx, `
		SELECT id FROM placement_tests WHERE batch_id = $1 AND organization_id = $2 AND status = 'running'
	`, id, orgID)
	if err != nil {
		return false, nil, err
	}
	var running []uuid.UUID
	for rows.Next() {
		var tid uuid.UUID
		if err := rows.Scan(&tid); err != nil {
			rows.Close()
			return false, nil, err
		}
		running = append(running, tid)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return false, nil, err
	}
	return true, running, tx.Commit(ctx)
}

func (r *placementBatchRepository) ListBatchSenders(ctx context.Context, orgID, batchID uuid.UUID, f PlacementBatchSenderFilter) ([]PlacementBatchSenderRow, int, error) {
	if f.Limit <= 0 {
		f.Limit = 50
	}
	order := `(counts.inbox::float / NULLIF(counts.delivered, 0)) ASC NULLS LAST, counts.delivered DESC, s.sender_email`
	switch f.Sort {
	case "best":
		order = `(counts.inbox::float / NULLIF(counts.delivered, 0)) DESC NULLS LAST, counts.delivered DESC, s.sender_email`
	case "email":
		order = `s.sender_email, s.id`
	case "status":
		order = `s.status, s.sender_email`
	}
	where := `s.batch_id = $1 AND EXISTS (SELECT 1 FROM placement_batches ob WHERE ob.id = s.batch_id AND ob.organization_id = $2)
		AND ($3 = '' OR s.status = $3)
		AND ($4 = '' OR s.sender_email ILIKE '%' || $4 || '%')`
	search := strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(strings.TrimSpace(f.Search))

	var total int
	if err := r.db.QueryRow(ctx, `SELECT COUNT(*) FROM placement_batch_senders s WHERE `+where,
		batchID, orgID, f.Status, search).Scan(&total); err != nil {
		return nil, 0, err
	}
	rows, err := r.db.Query(ctx, `
		SELECT `+prefixCols("s.", placementBatchSenderCols)+`,
			counts.inbox, counts.promotions, counts.other, counts.spam, counts.missing, counts.pending, counts.failed, counts.cancelled,
			ARRAY(SELECT t.id FROM placement_tests t WHERE t.batch_sender_id = s.id ORDER BY t.open_tracking OR t.link_tracking, t.created_at)
		FROM placement_batch_senders s
		JOIN placement_batches b ON b.id = s.batch_id
		CROSS JOIN LATERAL (
			SELECT
				COUNT(*) FILTER (WHERE r.folder = 'inbox') AS inbox,
				COUNT(*) FILTER (WHERE r.folder = 'promotions') AS promotions,
				COUNT(*) FILTER (WHERE r.folder = 'other') AS other,
				COUNT(*) FILTER (WHERE r.folder = 'spam') AS spam,
				COUNT(*) FILTER (WHERE r.folder = 'missing') AS missing,
				COUNT(*) FILTER (WHERE r.folder = 'pending') AS pending,
				COUNT(*) FILTER (WHERE r.folder = 'failed') AS failed,
				COUNT(*) FILTER (WHERE r.folder = 'cancelled') AS cancelled,
				COUNT(*) FILTER (WHERE r.folder IN `+deliveredFolders+`) AS delivered
			FROM placement_tests t
			JOIN placement_results r ON r.test_id = t.id
			WHERE t.batch_sender_id = s.id AND `+headlineTest+`
		) counts
		WHERE `+where+`
		ORDER BY `+order+`
		LIMIT $5 OFFSET $6
	`, batchID, orgID, f.Status, search, f.Limit, f.Offset)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	out := make([]PlacementBatchSenderRow, 0, f.Limit)
	for rows.Next() {
		var row PlacementBatchSenderRow
		var inbox, promotions, other, spam, missing, pending, failed, cancelled int
		s, err := scanBatchSender(rows, &inbox, &promotions, &other, &spam, &missing, &pending, &failed, &cancelled, &row.TestIDs)
		if err != nil {
			return nil, 0, err
		}
		row.PlacementBatchSender = s
		for folder, n := range map[string]int{
			models.PlacementFolderInbox: inbox, models.PlacementFolderPromotions: promotions, models.PlacementFolderOther: other,
			models.PlacementFolderSpam: spam, models.PlacementFolderMissing: missing, models.PlacementFolderPending: pending,
			models.PlacementFolderFailed: failed, models.PlacementFolderCancelled: cancelled,
		} {
			row.Counts.AddN(folder, n)
		}
		row.Counts.Finish()
		if row.TestIDs == nil {
			row.TestIDs = []uuid.UUID{}
		}
		out = append(out, row)
	}
	return out, total, rows.Err()
}

func (r *placementBatchRepository) BatchBreakdown(ctx context.Context, batchID uuid.UUID) ([]PlacementBreakdownRow, error) {
	var out []PlacementBreakdownRow
	// Overall, per tracking variant, so a comparison reads both halves.
	rows, err := r.db.Query(ctx, `
		SELECT t.open_tracking OR t.link_tracking, r.folder, COUNT(*)
		FROM placement_tests t
		JOIN placement_results r ON r.test_id = t.id
		WHERE t.batch_id = $1
		GROUP BY 1, 2
	`, batchID)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		b := PlacementBreakdownRow{Set: "overall"}
		if err := rows.Scan(&b.Tracked, &b.Folder, &b.Count); err != nil {
			rows.Close()
			return nil, err
		}
		out = append(out, b)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}

	// The headline copy by sending domain, sending provider, recipient
	// provider, and sending domain by recipient provider, in one pass.
	rows, err = r.db.Query(ctx, `
		SELECT
			CASE GROUPING(s.sender_domain, s.sender_family, r.provider)
				WHEN 3 THEN 'domain'
				WHEN 5 THEN 'provider'
				WHEN 6 THEN 'recipient'
				ELSE 'matrix'
			END,
			COALESCE(s.sender_domain, ''), COALESCE(s.sender_family, ''), COALESCE(r.provider, ''),
			r.folder, COUNT(*)
		FROM placement_tests t
		JOIN placement_batches b ON b.id = t.batch_id
		JOIN placement_batch_senders s ON s.id = t.batch_sender_id
		JOIN placement_results r ON r.test_id = t.id
		WHERE t.batch_id = $1 AND `+headlineTest+`
		GROUP BY GROUPING SETS (
			(s.sender_domain, r.folder),
			(s.sender_family, r.folder),
			(r.provider, r.folder),
			(s.sender_domain, r.provider, r.folder)
		)
	`, batchID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var b PlacementBreakdownRow
		if err := rows.Scan(&b.Set, &b.SenderDomain, &b.SenderFamily, &b.RecipientFamily, &b.Folder, &b.Count); err != nil {
			return nil, err
		}
		out = append(out, b)
	}
	return out, rows.Err()
}

func (r *placementBatchRepository) BatchGroupSizes(ctx context.Context, batchID uuid.UUID) ([]PlacementBatchGroupSize, error) {
	rows, err := r.db.Query(ctx, `
		SELECT GROUPING(sender_domain) = 0, COALESCE(sender_domain, ''), COALESCE(sender_family, ''), COUNT(*),
			COUNT(*) FILTER (WHERE status = 'completed')
		FROM placement_batch_senders
		WHERE batch_id = $1
		GROUP BY GROUPING SETS ((sender_domain), (sender_family))
	`, batchID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []PlacementBatchGroupSize
	for rows.Next() {
		var g PlacementBatchGroupSize
		if err := rows.Scan(&g.ByDomain, &g.Domain, &g.Family, &g.Senders, &g.Completed); err != nil {
			return nil, err
		}
		out = append(out, g)
	}
	return out, rows.Err()
}

func (r *placementBatchRepository) Coverage(ctx context.Context, orgID uuid.UUID, now time.Time) (PlacementCoverage, error) {
	var c PlacementCoverage
	err := r.db.QueryRow(ctx, `
		SELECT COUNT(*),
			COUNT(*) FILTER (WHERE last >= $2),
			COUNT(*) FILTER (WHERE last >= $3),
			COUNT(*) FILTER (WHERE last IS NULL)
		FROM (
			SELECT (
				SELECT MAX(pt.created_at) FROM placement_tests pt
				WHERE pt.sender_account_id = ea.id AND pt.organization_id = $1 AND pt.status = 'completed'
			) AS last
			FROM email_accounts ea
			WHERE ea.organization_id = $1 AND ea.seed_scope IS NULL AND ea.status = 'active'
		) fleet
	`, orgID, now.AddDate(0, 0, -7), now.AddDate(0, 0, -30)).Scan(&c.Mailboxes, &c.Tested7d, &c.Tested30d, &c.NeverTested)
	return c, err
}
