package repository

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/warmbly/warmbly/internal/infrastructure/db"
	"github.com/warmbly/warmbly/internal/models"
)

// SeedAccount is a mailbox as the seed panel sees it.
type SeedAccount struct {
	ID             uuid.UUID
	OrganizationID *uuid.UUID
	UserID         uuid.UUID
	Email          string
	Name           string
	Provider       string
	MailHost       string
	Status         string
	WorkerID       *uuid.UUID
	SeedScope      string
}

// PlacementProbe is one scheduled send, with the test it belongs to.
type PlacementProbe struct {
	Test   models.PlacementTest
	Result models.PlacementResult
}

// PlacementLanding is a delivered probe found in its seed's synced mail.
type PlacementLanding struct {
	ResultID uuid.UUID
	TestID   uuid.UUID
	Folder   string
	Flags    []string
}

// PlacementFinished is a test that just resolved its last probe.
type PlacementFinished struct {
	ID             uuid.UUID
	OrganizationID *uuid.UUID
	CreatedBy      *uuid.UUID
	Origin         string
	Status         string
	MonitorID      *uuid.UUID
	CampaignID     *uuid.UUID
	CompareGroupID *uuid.UUID
}

// PlacementTestFilter narrows a test listing. A nil OrganizationID lists every
// workspace's tests (admin only).
type PlacementTestFilter struct {
	OrganizationID *uuid.UUID
	CampaignID     *uuid.UUID
	Limit          int
	Offset         int
}

// PlacementRemoteReport is a delivered cloud probe the instance still has to
// tell the cloud about.
type PlacementRemoteReport struct {
	ResultID     uuid.UUID
	RemoteTestID uuid.UUID
	RemoteSeedID uuid.UUID
	MessageID    string
	SentAt       *time.Time
	Folder       string
	Error        string
}

// PlacementRepository stores placement tests, their probes, the seed panels
// and the monitors. Control plane only.
type PlacementRepository interface {
	// CreateTest writes a test, one pending result per probe and, for probes
	// sent from this instance, the pending task that sends each one, in one
	// transaction. tasks may be nil for a test with nothing to send here.
	CreateTest(ctx context.Context, t *models.PlacementTest, results []models.PlacementResult, tasks []Task) error
	GetTest(ctx context.Context, id uuid.UUID) (*models.PlacementTest, error)
	// GetOrgTest is GetTest scoped to one workspace; nil when it is another's.
	GetOrgTest(ctx context.Context, orgID, id uuid.UUID) (*models.PlacementTest, error)
	ListTests(ctx context.Context, f PlacementTestFilter) ([]models.PlacementTest, int, error)
	ListResults(ctx context.Context, testIDs []uuid.UUID) (map[uuid.UUID][]models.PlacementResult, error)
	ListGroup(ctx context.Context, orgID, groupID uuid.UUID) ([]models.PlacementTest, error)
	// CancelTest stops the probes that have not left yet. Delivered probes
	// keep being classified. False when the test is not running.
	CancelTest(ctx context.Context, orgID, id uuid.UUID) (bool, error)

	// GetProbeByTask loads the probe a placement task sends.
	GetProbeByTask(ctx context.Context, taskID uuid.UUID) (*PlacementProbe, error)
	MarkProbeSent(ctx context.Context, resultID uuid.UUID, messageID string, sentAt time.Time) error
	// FailProbe records a probe that never left. No-op once it has a verdict.
	FailProbe(ctx context.Context, resultID uuid.UUID, reason string) error
	// SetProbeMessageIDByTask stores the Message-ID the provider actually put
	// on the wire, which Gmail and Graph restamp.
	SetProbeMessageIDByTask(ctx context.Context, taskID uuid.UUID, messageID string) error
	// FailProbeByTask walks a sent probe back when the worker reports the
	// send failed.
	FailProbeByTask(ctx context.Context, taskID uuid.UUID, reason string) error

	// FindLandings looks every delivered, unresolved probe on a local seed up
	// in that seed's synced mail by Message-ID.
	FindLandings(ctx context.Context, limit int) ([]PlacementLanding, error)
	RecordLanding(ctx context.Context, resultID uuid.UUID, folder, rawFlags string, at time.Time) error
	// ExpireProbes marks delivered probes not seen since sentBefore missing,
	// and probes never sent by scheduledBefore failed. On the cloud, a copy
	// its instance never reported by remoteBefore fails too.
	ExpireProbes(ctx context.Context, sentBefore, scheduledBefore, remoteBefore time.Time) error
	// FinishTests closes every running or cancelled test with nothing pending
	// and returns the ones it closed.
	FinishTests(ctx context.Context) ([]PlacementFinished, error)

	// ListSeeds lists seed mailboxes. scope "" lists both kinds; orgID limits
	// to one workspace's mailboxes.
	ListSeeds(ctx context.Context, scope string, orgID *uuid.UUID, activeOnly bool) ([]SeedAccount, error)
	GetSeedAccount(ctx context.Context, id uuid.UUID) (*SeedAccount, error)
	// SetSeedScope makes a mailbox a seed of the given scope, or an ordinary
	// mailbox again with "". The sender pools skip a seed on read; the caller
	// takes it out of warmup.
	SetSeedScope(ctx context.Context, accountID uuid.UUID, scope string) error
	// SeedScope is a mailbox's seed scope, "" for an ordinary mailbox.
	SeedScope(ctx context.Context, accountID uuid.UUID) (string, error)
	ListSeedCandidates(ctx context.Context, search string, limit int) ([]SeedAccount, error)
	// ListOrgMailboxes lists a workspace's active and inactive mailboxes with
	// what would stop each from becoming a seed.
	ListOrgMailboxes(ctx context.Context, orgID uuid.UUID) ([]SeedAccount, map[uuid.UUID]string, error)

	// CountMeteredTests counts a workspace's tests on the metered panels
	// since a moment.
	CountMeteredTests(ctx context.Context, orgID uuid.UUID, since time.Time) (int, error)
	CountRunning(ctx context.Context, orgID uuid.UUID) (int, error)
	// SenderBusy reports whether a sender still has probes waiting to leave.
	SenderBusy(ctx context.Context, senderID uuid.UUID) (bool, error)
	// IsProbeSentCopy reports whether a message a mailbox synced is its own
	// sent copy of a placement probe.
	IsProbeSentCopy(ctx context.Context, accountID uuid.UUID, messageID string) (bool, error)
	// SampleLead is the campaign's first lead, the contact a test renders the
	// copy for when none is chosen. Nil for a campaign with no leads.
	SampleLead(ctx context.Context, campaignID uuid.UUID) (*uuid.UUID, error)

	// Cloud side: a test the cloud runs for a linked instance.
	RecordRemoteSent(ctx context.Context, instanceID, testID, seedAccountID uuid.UUID, messageID string, sentAt *time.Time, failure string) error
	GetRemoteTest(ctx context.Context, instanceID, testID uuid.UUID) (*models.PlacementTest, []models.PlacementResult, error)
	// Instance side: probes to report to the cloud, and verdicts back.
	ListRemoteReports(ctx context.Context, limit int) ([]PlacementRemoteReport, error)
	MarkRemoteReported(ctx context.Context, resultIDs []uuid.UUID) error
	ListRemoteOpenTests(ctx context.Context, limit int) ([]models.PlacementTest, error)
	RecordRemoteVerdict(ctx context.Context, testID, remoteSeedID uuid.UUID, folder string, at *time.Time) error

	GetMonitor(ctx context.Context, orgID, campaignID uuid.UUID) (*models.PlacementMonitor, error)
	GetMonitorByID(ctx context.Context, id uuid.UUID) (*models.PlacementMonitor, error)
	UpsertMonitor(ctx context.Context, m *models.PlacementMonitor) error
	DeleteMonitor(ctx context.Context, orgID, campaignID uuid.UUID) (bool, error)
	ListDueMonitors(ctx context.Context, now time.Time, limit int) ([]models.PlacementMonitor, error)
	MarkMonitorRun(ctx context.Context, id uuid.UUID, next time.Time, testID, senderID *uuid.UUID, lastErr string) error
	MarkMonitorAlert(ctx context.Context, id uuid.UUID, at time.Time) error
}

type placementRepository struct {
	db *db.DB
}

func NewPlacementRepository(db *db.DB) PlacementRepository {
	return &placementRepository{db: db}
}

const placementTestCols = `id, organization_id, sender_account_id, sender_email, created_by, campaign_id,
	sequence_id, contact_id, monitor_id, subject, body_plain, body_html, open_tracking, link_tracking,
	compare_group_id, origin, panel, status, error, remote_instance_id, remote_test_id, created_at, finished_at`

func scanPlacementTest(row pgx.Row) (*models.PlacementTest, error) {
	var t models.PlacementTest
	err := row.Scan(&t.ID, &t.OrganizationID, &t.SenderAccountID, &t.SenderEmail, &t.CreatedBy, &t.CampaignID,
		&t.SequenceID, &t.ContactID, &t.MonitorID, &t.Subject, &t.BodyPlain, &t.BodyHTML, &t.OpenTracking, &t.LinkTracking,
		&t.CompareGroupID, &t.Origin, &t.Panel, &t.Status, &t.Error, &t.RemoteInstanceID, &t.RemoteTestID, &t.CreatedAt, &t.FinishedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &t, nil
}

const placementResultCols = `id, test_id, seed_account_id, seed_address, provider, remote_seed_id, task_id,
	message_id, folder, scheduled_at, sent_at, detected_at, raw_flags, error, remote_synced_at`

func scanPlacementResult(row pgx.Row) (models.PlacementResult, error) {
	var r models.PlacementResult
	err := row.Scan(&r.ID, &r.TestID, &r.SeedAccountID, &r.SeedAddress, &r.Family, &r.RemoteSeedID, &r.TaskID,
		&r.MessageID, &r.Folder, &r.ScheduledAt, &r.SentAt, &r.DetectedAt, &r.RawFlags, &r.Error, &r.RemoteSyncedAt)
	return r, err
}

func (r *placementRepository) CreateTest(ctx context.Context, t *models.PlacementTest, results []models.PlacementResult, tasks []Task) error {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	err = tx.QueryRow(ctx, `
		INSERT INTO placement_tests (id, organization_id, sender_account_id, sender_email, created_by, campaign_id,
			sequence_id, contact_id, monitor_id, subject, body_plain, body_html, open_tracking, link_tracking,
			compare_group_id, origin, panel, status, error, remote_instance_id, remote_test_id, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18, $19, $20, $21, NOW())
		RETURNING created_at
	`, t.ID, t.OrganizationID, t.SenderAccountID, t.SenderEmail, t.CreatedBy, t.CampaignID,
		t.SequenceID, t.ContactID, t.MonitorID, t.Subject, t.BodyPlain, t.BodyHTML, t.OpenTracking, t.LinkTracking,
		t.CompareGroupID, t.Origin, t.Panel, t.Status, t.Error, t.RemoteInstanceID, t.RemoteTestID).Scan(&t.CreatedAt)
	if err != nil {
		return err
	}

	batch := &pgx.Batch{}
	// The task rows go first: each result points at its task.
	for i := range tasks {
		task := &tasks[i]
		batch.Queue(`
			INSERT INTO tasks (id, task_type, email_account_id, status, message_id, scheduled_at, created_at, updated_at)
			VALUES ($1, $2, $3, $4, $5, $6, NOW(), NOW())
		`, task.ID, task.TaskType, task.EmailAccountID, task.Status, task.MessageID, task.ScheduledAt)
	}
	for i := range results {
		res := &results[i]
		if res.ID == uuid.Nil {
			res.ID = uuid.New()
		}
		folder := res.Folder
		if folder == "" {
			folder = models.PlacementFolderPending
		}
		batch.Queue(`
			INSERT INTO placement_results (id, test_id, seed_account_id, seed_address, provider, remote_seed_id,
				task_id, message_id, folder, scheduled_at, error)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
		`, res.ID, t.ID, res.SeedAccountID, res.SeedAddress, res.Family, res.RemoteSeedID,
			res.TaskID, res.MessageID, folder, res.ScheduledAt, res.Error)
	}
	br := tx.SendBatch(ctx, batch)
	for range len(tasks) + len(results) {
		if _, err := br.Exec(); err != nil {
			_ = br.Close()
			return err
		}
	}
	if err := br.Close(); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (r *placementRepository) GetTest(ctx context.Context, id uuid.UUID) (*models.PlacementTest, error) {
	return scanPlacementTest(r.db.QueryRow(ctx, `SELECT `+placementTestCols+` FROM placement_tests WHERE id = $1`, id))
}

func (r *placementRepository) GetOrgTest(ctx context.Context, orgID, id uuid.UUID) (*models.PlacementTest, error) {
	return scanPlacementTest(r.db.QueryRow(ctx,
		`SELECT `+placementTestCols+` FROM placement_tests WHERE id = $2 AND organization_id = $1`, orgID, id))
}

func (r *placementRepository) ListTests(ctx context.Context, f PlacementTestFilter) ([]models.PlacementTest, int, error) {
	if f.Limit <= 0 {
		f.Limit = 25
	}
	where := `($1::uuid IS NULL OR organization_id = $1) AND ($2::uuid IS NULL OR campaign_id = $2)`

	var total int
	if err := r.db.QueryRow(ctx, `SELECT COUNT(*) FROM placement_tests WHERE `+where, f.OrganizationID, f.CampaignID).Scan(&total); err != nil {
		return nil, 0, err
	}
	rows, err := r.db.Query(ctx, `
		SELECT `+placementTestCols+`
		FROM placement_tests
		WHERE `+where+`
		ORDER BY created_at DESC, id DESC
		LIMIT $3 OFFSET $4
	`, f.OrganizationID, f.CampaignID, f.Limit, f.Offset)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	out := make([]models.PlacementTest, 0, f.Limit)
	for rows.Next() {
		t, err := scanPlacementTest(rows)
		if err != nil {
			return nil, 0, err
		}
		out = append(out, *t)
	}
	return out, total, rows.Err()
}

func (r *placementRepository) ListResults(ctx context.Context, testIDs []uuid.UUID) (map[uuid.UUID][]models.PlacementResult, error) {
	out := make(map[uuid.UUID][]models.PlacementResult, len(testIDs))
	if len(testIDs) == 0 {
		return out, nil
	}
	rows, err := r.db.Query(ctx, `
		SELECT `+placementResultCols+`
		FROM placement_results
		WHERE test_id = ANY($1)
		ORDER BY provider ASC, seed_address ASC
	`, testIDs)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		res, err := scanPlacementResult(rows)
		if err != nil {
			return nil, err
		}
		out[res.TestID] = append(out[res.TestID], res)
	}
	return out, rows.Err()
}

func (r *placementRepository) ListGroup(ctx context.Context, orgID, groupID uuid.UUID) ([]models.PlacementTest, error) {
	rows, err := r.db.Query(ctx, `
		SELECT `+placementTestCols+`
		FROM placement_tests
		WHERE organization_id = $1 AND compare_group_id = $2
		ORDER BY link_tracking OR open_tracking, created_at
	`, orgID, groupID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []models.PlacementTest
	for rows.Next() {
		t, err := scanPlacementTest(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *t)
	}
	return out, rows.Err()
}

func (r *placementRepository) CancelTest(ctx context.Context, orgID, id uuid.UUID) (bool, error) {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return false, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	tag, err := tx.Exec(ctx, `
		UPDATE placement_tests SET status = 'cancelled'
		WHERE id = $2 AND organization_id = $1 AND status = 'running'
	`, orgID, id)
	if err != nil {
		return false, err
	}
	if tag.RowsAffected() == 0 {
		return false, nil
	}
	// The dispatcher skips a task that is no longer pending, so flipping the
	// row is the whole cancellation.
	if _, err := tx.Exec(ctx, `
		UPDATE tasks SET status = 'cancelled', updated_at = NOW()
		WHERE status = 'pending' AND id IN (
			SELECT task_id FROM placement_results
			WHERE test_id = $1 AND sent_at IS NULL AND task_id IS NOT NULL
		)
	`, id); err != nil {
		return false, err
	}
	if _, err := tx.Exec(ctx, `
		UPDATE placement_results SET folder = 'cancelled', error = 'Cancelled before it was sent'
		WHERE test_id = $1 AND folder = 'pending' AND sent_at IS NULL
	`, id); err != nil {
		return false, err
	}
	return true, tx.Commit(ctx)
}

func (r *placementRepository) GetProbeByTask(ctx context.Context, taskID uuid.UUID) (*PlacementProbe, error) {
	res, err := scanPlacementResult(r.db.QueryRow(ctx,
		`SELECT `+placementResultCols+` FROM placement_results WHERE task_id = $1`, taskID))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	test, err := r.GetTest(ctx, res.TestID)
	if err != nil || test == nil {
		return nil, err
	}
	return &PlacementProbe{Test: *test, Result: res}, nil
}

func (r *placementRepository) MarkProbeSent(ctx context.Context, resultID uuid.UUID, messageID string, sentAt time.Time) error {
	_, err := r.db.Exec(ctx, `
		UPDATE placement_results SET message_id = $2, sent_at = $3
		WHERE id = $1 AND folder = 'pending'
	`, resultID, messageID, sentAt)
	return err
}

func (r *placementRepository) FailProbe(ctx context.Context, resultID uuid.UUID, reason string) error {
	_, err := r.db.Exec(ctx, `
		UPDATE placement_results SET folder = 'failed', error = $2, detected_at = NOW()
		WHERE id = $1 AND folder = 'pending'
	`, resultID, truncateRunes(reason, 500))
	return err
}

func (r *placementRepository) SetProbeMessageIDByTask(ctx context.Context, taskID uuid.UUID, messageID string) error {
	if strings.TrimSpace(messageID) == "" {
		return nil
	}
	_, err := r.db.Exec(ctx, `UPDATE placement_results SET message_id = $2 WHERE task_id = $1`, taskID, messageID)
	return err
}

func (r *placementRepository) FailProbeByTask(ctx context.Context, taskID uuid.UUID, reason string) error {
	_, err := r.db.Exec(ctx, `
		UPDATE placement_results SET folder = 'failed', error = $2, detected_at = NOW()
		WHERE task_id = $1 AND folder = 'pending'
	`, taskID, truncateRunes(reason, 500))
	return err
}

func (r *placementRepository) FindLandings(ctx context.Context, limit int) ([]PlacementLanding, error) {
	if limit <= 0 {
		limit = 500
	}
	// The window reaches an hour before the send: internal_date is the
	// message's own Date header on some providers, and clocks drift.
	rows, err := r.db.Query(ctx, `
		SELECT pr.id, pr.test_id, ue.folder, ue.flags
		FROM placement_results pr
		JOIN email_accounts ea ON ea.id = pr.seed_account_id
		JOIN LATERAL (
			SELECT u.folder, u.flags
			FROM unibox_emails u
			WHERE u.user_id = ea.user_id
			  AND u.email_id = pr.seed_account_id
			  AND u.internal_date >= pr.sent_at - interval '1 hour'
			  AND btrim(u.message_id, '<> ') = btrim(pr.message_id, '<> ')
			ORDER BY u.internal_date DESC
			LIMIT 1
		) ue ON true
		WHERE pr.folder = 'pending'
		  AND pr.sent_at IS NOT NULL
		  AND pr.message_id <> ''
		  AND pr.seed_account_id IS NOT NULL
		LIMIT $1
	`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []PlacementLanding
	for rows.Next() {
		var l PlacementLanding
		if err := rows.Scan(&l.ResultID, &l.TestID, &l.Folder, &l.Flags); err != nil {
			return nil, err
		}
		out = append(out, l)
	}
	return out, rows.Err()
}

func (r *placementRepository) RecordLanding(ctx context.Context, resultID uuid.UUID, folder, rawFlags string, at time.Time) error {
	_, err := r.db.Exec(ctx, `
		UPDATE placement_results SET folder = $2, raw_flags = $3, detected_at = $4
		WHERE id = $1 AND folder = 'pending'
	`, resultID, folder, truncateRunes(rawFlags, 1000), at)
	return err
}

func (r *placementRepository) ExpireProbes(ctx context.Context, sentBefore, scheduledBefore, remoteBefore time.Time) error {
	// A cloud seed's verdict comes back on the next sync, so it gets half an
	// hour more before the instance calls it missing itself.
	if _, err := r.db.Exec(ctx, `
		UPDATE placement_results SET folder = 'missing', detected_at = NOW()
		WHERE folder = 'pending' AND sent_at IS NOT NULL
		  AND sent_at < $1 - CASE WHEN remote_seed_id IS NULL THEN interval '0' ELSE interval '30 minutes' END
	`, sentBefore); err != nil {
		return err
	}
	if _, err := r.db.Exec(ctx, `
		UPDATE placement_results SET folder = 'failed', error = 'The instance never reported this send', detected_at = NOW()
		WHERE folder = 'pending' AND sent_at IS NULL AND scheduled_at IS NULL
		  AND test_id IN (SELECT id FROM placement_tests WHERE origin = 'remote' AND created_at < $1)
	`, remoteBefore); err != nil {
		return err
	}
	// A probe whose task never ran (a lost dispatch, a removed worker) would
	// otherwise hold its test open for ever.
	_, err := r.db.Exec(ctx, `
		UPDATE placement_results SET folder = 'failed', error = 'The send was never dispatched', detected_at = NOW()
		WHERE folder = 'pending' AND sent_at IS NULL AND scheduled_at IS NOT NULL AND scheduled_at < $1
	`, scheduledBefore)
	return err
}

func (r *placementRepository) FinishTests(ctx context.Context) ([]PlacementFinished, error) {
	rows, err := r.db.Query(ctx, `
		UPDATE placement_tests pt
		SET status = CASE
				WHEN pt.status <> 'running' THEN pt.status
				WHEN NOT EXISTS (
					SELECT 1 FROM placement_results pr
					WHERE pr.test_id = pt.id AND pr.folder IN ('inbox', 'promotions', 'other', 'spam', 'missing')
				) THEN 'failed'
				ELSE 'completed'
			END,
			error = CASE
				WHEN pt.status = 'running' AND NOT EXISTS (
					SELECT 1 FROM placement_results pr
					WHERE pr.test_id = pt.id AND pr.folder IN ('inbox', 'promotions', 'other', 'spam', 'missing')
				) THEN 'No copy of the test left the sender'
				ELSE pt.error
			END,
			finished_at = NOW()
		WHERE pt.status IN ('running', 'cancelled')
		  AND pt.finished_at IS NULL
		  AND pt.origin <> 'remote'
		  AND NOT EXISTS (
			SELECT 1 FROM placement_results pr WHERE pr.test_id = pt.id AND pr.folder = 'pending'
		  )
		RETURNING pt.id, pt.organization_id, pt.created_by, pt.origin, pt.status, pt.monitor_id, pt.campaign_id, pt.compare_group_id
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []PlacementFinished
	for rows.Next() {
		var f PlacementFinished
		if err := rows.Scan(&f.ID, &f.OrganizationID, &f.CreatedBy, &f.Origin, &f.Status, &f.MonitorID, &f.CampaignID, &f.CompareGroupID); err != nil {
			return nil, err
		}
		out = append(out, f)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	// The cloud's copy of a remote test closes on its own clock: the instance
	// reports sends in batches and may never report some.
	if _, err := r.db.Exec(ctx, `
		UPDATE placement_tests pt SET status = 'completed', finished_at = NOW()
		WHERE pt.origin = 'remote' AND pt.status = 'running'
		  AND NOT EXISTS (SELECT 1 FROM placement_results pr WHERE pr.test_id = pt.id AND pr.folder = 'pending')
	`); err != nil {
		return nil, err
	}
	return out, nil
}

const seedSelectCols = `ea.id, ea.organization_id, ea.user_id, ea.email, ea.name, ea.provider::text, ea.mail_host,
	ea.status::text, ea.worker_id, COALESCE(ea.seed_scope, '')`

func scanSeed(rows pgx.Row) (SeedAccount, error) {
	var s SeedAccount
	err := rows.Scan(&s.ID, &s.OrganizationID, &s.UserID, &s.Email, &s.Name, &s.Provider, &s.MailHost,
		&s.Status, &s.WorkerID, &s.SeedScope)
	return s, err
}

func (r *placementRepository) querySeeds(ctx context.Context, query string, args ...any) ([]SeedAccount, error) {
	rows, err := r.db.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]SeedAccount, 0)
	for rows.Next() {
		s, err := scanSeed(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

func (r *placementRepository) ListSeeds(ctx context.Context, scope string, orgID *uuid.UUID, activeOnly bool) ([]SeedAccount, error) {
	return r.querySeeds(ctx, `
		SELECT `+seedSelectCols+`
		FROM email_accounts ea
		WHERE ea.seed_scope IS NOT NULL
		  AND ($1 = '' OR ea.seed_scope = $1)
		  AND ($2::uuid IS NULL OR ea.organization_id = $2)
		  AND (NOT $3 OR (ea.status = 'active' AND ea.worker_id IS NOT NULL))
		ORDER BY ea.email ASC
	`, scope, orgID, activeOnly)
}

func (r *placementRepository) GetSeedAccount(ctx context.Context, id uuid.UUID) (*SeedAccount, error) {
	seeds, err := r.querySeeds(ctx, `SELECT `+seedSelectCols+` FROM email_accounts ea WHERE ea.id = $1`, id)
	if err != nil || len(seeds) == 0 {
		return nil, err
	}
	return &seeds[0], nil
}

func (r *placementRepository) SetSeedScope(ctx context.Context, accountID uuid.UUID, scope string) error {
	var value *string
	if scope != "" {
		value = &scope
	}
	_, err := r.db.Exec(ctx, `UPDATE email_accounts SET seed_scope = $2, updated_at = NOW() WHERE id = $1`, accountID, value)
	return err
}

func (r *placementRepository) SeedScope(ctx context.Context, accountID uuid.UUID) (string, error) {
	var scope string
	err := r.db.QueryRow(ctx, `SELECT COALESCE(seed_scope, '') FROM email_accounts WHERE id = $1`, accountID).Scan(&scope)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", nil
	}
	return scope, err
}

func (r *placementRepository) ListSeedCandidates(ctx context.Context, search string, limit int) ([]SeedAccount, error) {
	if limit <= 0 || limit > 100 {
		limit = 50
	}
	return r.querySeeds(ctx, `
		SELECT `+seedSelectCols+`
		FROM email_accounts ea
		WHERE ea.status = 'active'
		  AND ($1 = '' OR ea.email ILIKE '%' || $1 || '%')
		ORDER BY (ea.seed_scope IS NOT NULL) DESC, ea.email ASC
		LIMIT $2
	`, strings.TrimSpace(search), limit)
}

func (r *placementRepository) ListOrgMailboxes(ctx context.Context, orgID uuid.UUID) ([]SeedAccount, map[uuid.UUID]string, error) {
	rows, err := r.db.Query(ctx, `
		SELECT `+seedSelectCols+`,
			CASE
				WHEN ea.seed_scope = 'instance' THEN 'This mailbox is on the instance seed panel'
				WHEN ea.seed_scope IS NULL AND EXISTS (
					SELECT 1 FROM placement_tests pt
					WHERE pt.sender_account_id = ea.id AND pt.status = 'running'
				) THEN 'A placement test is sending from this mailbox'
				ELSE ''
			END
		FROM email_accounts ea
		WHERE ea.organization_id = $1
		ORDER BY (ea.seed_scope IS NOT NULL) DESC, ea.email ASC
	`, orgID)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()
	out := make([]SeedAccount, 0)
	blockers := map[uuid.UUID]string{}
	for rows.Next() {
		var s SeedAccount
		var blocker string
		if err := rows.Scan(&s.ID, &s.OrganizationID, &s.UserID, &s.Email, &s.Name, &s.Provider, &s.MailHost,
			&s.Status, &s.WorkerID, &s.SeedScope, &blocker); err != nil {
			return nil, nil, err
		}
		out = append(out, s)
		if blocker != "" {
			blockers[s.ID] = blocker
		}
	}
	return out, blockers, rows.Err()
}

func (r *placementRepository) CountMeteredTests(ctx context.Context, orgID uuid.UUID, since time.Time) (int, error) {
	var n int
	err := r.db.QueryRow(ctx, `
		SELECT COUNT(*) FROM placement_tests
		WHERE organization_id = $1
		  AND panel IN ('instance', 'cloud')
		  AND origin IN ('manual', 'monitor', 'remote')
		  AND created_at >= $2
	`, orgID, since).Scan(&n)
	return n, err
}

func (r *placementRepository) CountRunning(ctx context.Context, orgID uuid.UUID) (int, error) {
	var n int
	err := r.db.QueryRow(ctx, `
		SELECT COUNT(*) FROM placement_tests
		WHERE organization_id = $1 AND status = 'running' AND origin IN ('manual', 'monitor', 'remote')
	`, orgID).Scan(&n)
	return n, err
}

func (r *placementRepository) SenderBusy(ctx context.Context, senderID uuid.UUID) (bool, error) {
	var busy bool
	err := r.db.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM placement_results pr
			JOIN placement_tests pt ON pt.id = pr.test_id
			WHERE pt.sender_account_id = $1 AND pt.status = 'running'
			  AND pr.folder = 'pending' AND pr.sent_at IS NULL
		)
	`, senderID).Scan(&busy)
	return busy, err
}

func (r *placementRepository) IsProbeSentCopy(ctx context.Context, accountID uuid.UUID, messageID string) (bool, error) {
	messageID = strings.Trim(strings.TrimSpace(messageID), "<>")
	if messageID == "" {
		return false, nil
	}
	var found bool
	err := r.db.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM tasks
			WHERE message_id IN ($2, '<' || $2 || '>')
			  AND email_account_id = $1
			  AND task_type = 'placement'
		)
	`, accountID, messageID).Scan(&found)
	return found, err
}

func (r *placementRepository) SampleLead(ctx context.Context, campaignID uuid.UUID) (*uuid.UUID, error) {
	var id uuid.UUID
	err := r.db.QueryRow(ctx, `
		SELECT contact_id FROM campaign_leads
		WHERE campaign_id = $1
		ORDER BY position NULLS LAST, contact_id
		LIMIT 1
	`, campaignID).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &id, nil
}

func (r *placementRepository) RecordRemoteSent(ctx context.Context, instanceID, testID, seedAccountID uuid.UUID, messageID string, sentAt *time.Time, failure string) error {
	if failure != "" {
		_, err := r.db.Exec(ctx, `
			UPDATE placement_results pr SET folder = 'failed', error = $4, detected_at = NOW()
			FROM placement_tests pt
			WHERE pt.id = pr.test_id AND pt.id = $2 AND pt.remote_instance_id = $1
			  AND pr.seed_account_id = $3 AND pr.folder = 'pending'
		`, instanceID, testID, seedAccountID, truncateRunes(failure, 500))
		return err
	}
	_, err := r.db.Exec(ctx, `
		UPDATE placement_results pr SET message_id = $4, sent_at = COALESCE($5, NOW())
		FROM placement_tests pt
		WHERE pt.id = pr.test_id AND pt.id = $2 AND pt.remote_instance_id = $1
		  AND pr.seed_account_id = $3 AND pr.folder = 'pending'
	`, instanceID, testID, seedAccountID, messageID, sentAt)
	return err
}

func (r *placementRepository) GetRemoteTest(ctx context.Context, instanceID, testID uuid.UUID) (*models.PlacementTest, []models.PlacementResult, error) {
	test, err := scanPlacementTest(r.db.QueryRow(ctx,
		`SELECT `+placementTestCols+` FROM placement_tests WHERE id = $2 AND remote_instance_id = $1`, instanceID, testID))
	if err != nil || test == nil {
		return nil, nil, err
	}
	results, err := r.ListResults(ctx, []uuid.UUID{test.ID})
	if err != nil {
		return nil, nil, err
	}
	return test, results[test.ID], nil
}

func (r *placementRepository) ListRemoteReports(ctx context.Context, limit int) ([]PlacementRemoteReport, error) {
	if limit <= 0 {
		limit = 200
	}
	rows, err := r.db.Query(ctx, `
		SELECT pr.id, pt.remote_test_id, pr.remote_seed_id, pr.message_id, pr.sent_at, pr.folder, pr.error
		FROM placement_results pr
		JOIN placement_tests pt ON pt.id = pr.test_id
		WHERE pt.remote_test_id IS NOT NULL
		  AND pr.remote_seed_id IS NOT NULL
		  AND pr.remote_synced_at IS NULL
		  AND ((pr.sent_at IS NOT NULL AND pr.message_id <> '') OR pr.folder IN ('failed', 'cancelled'))
		ORDER BY pr.sent_at NULLS LAST
		LIMIT $1
	`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []PlacementRemoteReport
	for rows.Next() {
		var rep PlacementRemoteReport
		if err := rows.Scan(&rep.ResultID, &rep.RemoteTestID, &rep.RemoteSeedID, &rep.MessageID, &rep.SentAt, &rep.Folder, &rep.Error); err != nil {
			return nil, err
		}
		out = append(out, rep)
	}
	return out, rows.Err()
}

func (r *placementRepository) MarkRemoteReported(ctx context.Context, resultIDs []uuid.UUID) error {
	if len(resultIDs) == 0 {
		return nil
	}
	_, err := r.db.Exec(ctx, `UPDATE placement_results SET remote_synced_at = NOW() WHERE id = ANY($1)`, resultIDs)
	return err
}

func (r *placementRepository) ListRemoteOpenTests(ctx context.Context, limit int) ([]models.PlacementTest, error) {
	if limit <= 0 {
		limit = 50
	}
	rows, err := r.db.Query(ctx, `
		SELECT `+placementTestCols+`
		FROM placement_tests pt
		WHERE pt.remote_test_id IS NOT NULL
		  AND pt.status IN ('running', 'cancelled')
		  AND EXISTS (
			SELECT 1 FROM placement_results pr
			WHERE pr.test_id = pt.id AND pr.folder = 'pending' AND pr.remote_synced_at IS NOT NULL
		  )
		ORDER BY pt.created_at
		LIMIT $1
	`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []models.PlacementTest
	for rows.Next() {
		t, err := scanPlacementTest(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *t)
	}
	return out, rows.Err()
}

func (r *placementRepository) RecordRemoteVerdict(ctx context.Context, testID, remoteSeedID uuid.UUID, folder string, at *time.Time) error {
	_, err := r.db.Exec(ctx, `
		UPDATE placement_results SET folder = $3, detected_at = COALESCE($4, NOW())
		WHERE test_id = $1 AND remote_seed_id = $2 AND folder = 'pending'
	`, testID, remoteSeedID, folder, at)
	return err
}

const placementMonitorCols = `id, organization_id, campaign_id, created_by, enabled, interval_days, panel,
	alert_below, pause_on_alert, next_run_at, last_run_at, last_test_id, last_sender_id, last_alert_at,
	last_error, created_at, updated_at`

func scanPlacementMonitor(row pgx.Row) (*models.PlacementMonitor, error) {
	var m models.PlacementMonitor
	err := row.Scan(&m.ID, &m.OrganizationID, &m.CampaignID, &m.CreatedBy, &m.Enabled, &m.IntervalDays, &m.Panel,
		&m.AlertBelow, &m.PauseOnAlert, &m.NextRunAt, &m.LastRunAt, &m.LastTestID, &m.LastSenderID, &m.LastAlertAt,
		&m.LastError, &m.CreatedAt, &m.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &m, nil
}

func (r *placementRepository) GetMonitor(ctx context.Context, orgID, campaignID uuid.UUID) (*models.PlacementMonitor, error) {
	return scanPlacementMonitor(r.db.QueryRow(ctx,
		`SELECT `+placementMonitorCols+` FROM placement_monitors WHERE organization_id = $1 AND campaign_id = $2`, orgID, campaignID))
}

func (r *placementRepository) GetMonitorByID(ctx context.Context, id uuid.UUID) (*models.PlacementMonitor, error) {
	return scanPlacementMonitor(r.db.QueryRow(ctx, `SELECT `+placementMonitorCols+` FROM placement_monitors WHERE id = $1`, id))
}

func (r *placementRepository) UpsertMonitor(ctx context.Context, m *models.PlacementMonitor) error {
	if m.ID == uuid.Nil {
		m.ID = uuid.New()
	}
	row := r.db.QueryRow(ctx, `
		INSERT INTO placement_monitors (id, organization_id, campaign_id, created_by, enabled, interval_days,
			panel, alert_below, pause_on_alert, next_run_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
		ON CONFLICT (campaign_id) DO UPDATE SET
			enabled = EXCLUDED.enabled,
			interval_days = EXCLUDED.interval_days,
			panel = EXCLUDED.panel,
			alert_below = EXCLUDED.alert_below,
			pause_on_alert = EXCLUDED.pause_on_alert,
			next_run_at = EXCLUDED.next_run_at,
			last_error = '',
			updated_at = NOW()
		WHERE placement_monitors.organization_id = EXCLUDED.organization_id
		RETURNING `+placementMonitorCols,
		m.ID, m.OrganizationID, m.CampaignID, m.CreatedBy, m.Enabled, m.IntervalDays,
		m.Panel, m.AlertBelow, m.PauseOnAlert, m.NextRunAt)
	saved, err := scanPlacementMonitor(row)
	if err != nil {
		return err
	}
	if saved == nil {
		return pgx.ErrNoRows
	}
	*m = *saved
	return nil
}

func (r *placementRepository) DeleteMonitor(ctx context.Context, orgID, campaignID uuid.UUID) (bool, error) {
	tag, err := r.db.Exec(ctx, `DELETE FROM placement_monitors WHERE organization_id = $1 AND campaign_id = $2`, orgID, campaignID)
	if err != nil {
		return false, err
	}
	return tag.RowsAffected() > 0, nil
}

func (r *placementRepository) ListDueMonitors(ctx context.Context, now time.Time, limit int) ([]models.PlacementMonitor, error) {
	if limit <= 0 {
		limit = 50
	}
	rows, err := r.db.Query(ctx, `
		SELECT `+placementMonitorCols+`
		FROM placement_monitors
		WHERE enabled AND next_run_at <= $1
		ORDER BY next_run_at
		LIMIT $2
	`, now, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []models.PlacementMonitor
	for rows.Next() {
		m, err := scanPlacementMonitor(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *m)
	}
	return out, rows.Err()
}

func (r *placementRepository) MarkMonitorRun(ctx context.Context, id uuid.UUID, next time.Time, testID, senderID *uuid.UUID, lastErr string) error {
	_, err := r.db.Exec(ctx, `
		UPDATE placement_monitors SET
			next_run_at = $2,
			last_run_at = NOW(),
			last_test_id = COALESCE($3, last_test_id),
			last_sender_id = COALESCE($4, last_sender_id),
			last_error = $5,
			updated_at = NOW()
		WHERE id = $1
	`, id, next, testID, senderID, truncateRunes(lastErr, 500))
	return err
}

func (r *placementRepository) MarkMonitorAlert(ctx context.Context, id uuid.UUID, at time.Time) error {
	_, err := r.db.Exec(ctx, `UPDATE placement_monitors SET last_alert_at = $2 WHERE id = $1`, id, at)
	return err
}
