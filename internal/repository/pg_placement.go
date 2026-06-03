package repository

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/warmbly/warmbly/internal/infrastructure/db"
)

// Placement folder classifications. These mirror the CHECK constraint on
// placement_results.folder.
const (
	PlacementFolderPending    = "pending"
	PlacementFolderInbox      = "inbox"
	PlacementFolderPromotions = "promotions"
	PlacementFolderSpam       = "spam"
	PlacementFolderOther      = "other"
)

// Placement test statuses.
const (
	PlacementStatusPending   = "pending"
	PlacementStatusCompleted = "completed"
)

// PlacementTest is one inbox-placement run: a tokenized copy of a template
// sent from one sender mailbox to the active seed panel.
type PlacementTest struct {
	ID              uuid.UUID
	OrganizationID  *uuid.UUID
	SenderAccountID uuid.UUID
	Subject         string
	BodyPlain       string
	BodyHTML        string
	Token           string
	Status          string
	CreatedAt       time.Time
	FinishedAt      *time.Time
}

// PlacementResult is the classification for one (test, seed) pair.
type PlacementResult struct {
	ID            uuid.UUID
	TestID        uuid.UUID
	SeedAccountID uuid.UUID
	Provider      string
	Folder        string
	DetectedAt    *time.Time
	RawFlags      string
}

// SeedAccount is a connected mailbox flagged is_seed, used as a placement
// recipient. UserID is needed to query that seed's unibox entries (unibox is
// keyed by the owning user, not the org).
type SeedAccount struct {
	ID       uuid.UUID
	UserID   uuid.UUID
	Email    string
	Name     string
	Provider string
	Status   string
	WorkerID *uuid.UUID
	IsSeed   bool
}

// PendingResultJob is the minimal view the poller needs to classify one
// pending result: which seed, which user owns its unibox, and the test token.
type PendingResultJob struct {
	ResultID      uuid.UUID
	TestID        uuid.UUID
	SeedAccountID uuid.UUID
	SeedUserID    uuid.UUID
	Provider      string
	Token         string
	TestCreatedAt time.Time
}

// UniboxTokenMatch is a unibox entry whose subject (or stored token) matched a
// placement token, with the flags/labels needed to classify the folder.
type UniboxTokenMatch struct {
	InternalID uuid.UUID
	Subject    string
	Flags      []string
}

// PlacementRepository is the data access surface for seed inbox-placement
// testing. It deliberately does not depend on the worker (control plane only).
type PlacementRepository interface {
	CreateTest(ctx context.Context, t *PlacementTest) error
	GetTest(ctx context.Context, id uuid.UUID) (*PlacementTest, error)
	ListTests(ctx context.Context, orgID *uuid.UUID, limit, offset int) ([]PlacementTest, int, error)
	SetTestStatus(ctx context.Context, id uuid.UUID, status string, finishedAt *time.Time) error

	CreatePendingResult(ctx context.Context, testID, seedAccountID uuid.UUID, provider string) error
	GetTestWithResults(ctx context.Context, id uuid.UUID) (*PlacementTest, []PlacementResult, error)
	RecordResult(ctx context.Context, resultID uuid.UUID, folder, rawFlags string, detectedAt time.Time) error

	// ListPendingResults returns unresolved results joined to their seed's
	// owning user + the test token, so the poller can look each one up.
	ListPendingResults(ctx context.Context, limit int) ([]PendingResultJob, error)
	// CountPendingForTest reports how many results are still pending, used to
	// decide when a test is fully resolved.
	CountPendingForTest(ctx context.Context, testID uuid.UUID) (int, error)

	// ListSeedAccounts returns seed mailboxes. activeOnly restricts to
	// status='active' (the send/classify panel); pass false for management UI.
	ListSeedAccounts(ctx context.Context, activeOnly bool) ([]SeedAccount, error)
	GetSeedAccount(ctx context.Context, id uuid.UUID) (*SeedAccount, error)
	SetIsSeed(ctx context.Context, accountID uuid.UUID, isSeed bool) error
	// ListSeedCandidates lists connected mailboxes (optionally filtered by
	// substring) so an admin can pick which to flag as seeds.
	ListSeedCandidates(ctx context.Context, search string, limit int) ([]SeedAccount, error)

	// FindTokenInUnibox looks for a placement token inside a seed's received
	// mail. Reuses the unibox store (unibox_emails) the worker syncs into. We
	// match the token embedded in the subject (the only token carrier that
	// survives sync without a worker change — see service.go) and return the
	// matching entry's flags/labels for classification.
	FindTokenInUnibox(ctx context.Context, userID, seedAccountID uuid.UUID, token string, since time.Time) (*UniboxTokenMatch, error)
}

type placementRepository struct {
	db *db.DB
}

func NewPlacementRepository(db *db.DB) PlacementRepository {
	return &placementRepository{db: db}
}

func (r *placementRepository) CreateTest(ctx context.Context, t *PlacementTest) error {
	query := `
		INSERT INTO placement_tests (id, organization_id, sender_account_id, subject, body_plain, body_html, token, status, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, NOW())
	`
	_, err := r.db.Exec(ctx, query,
		t.ID, t.OrganizationID, t.SenderAccountID, t.Subject, t.BodyPlain, t.BodyHTML, t.Token, t.Status,
	)
	return err
}

func (r *placementRepository) GetTest(ctx context.Context, id uuid.UUID) (*PlacementTest, error) {
	query := `
		SELECT id, organization_id, sender_account_id, subject, body_plain, body_html, token, status, created_at, finished_at
		FROM placement_tests
		WHERE id = $1
	`
	var t PlacementTest
	err := r.db.QueryRow(ctx, query, id).Scan(
		&t.ID, &t.OrganizationID, &t.SenderAccountID, &t.Subject, &t.BodyPlain, &t.BodyHTML, &t.Token, &t.Status, &t.CreatedAt, &t.FinishedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &t, nil
}

func (r *placementRepository) ListTests(ctx context.Context, orgID *uuid.UUID, limit, offset int) ([]PlacementTest, int, error) {
	if limit <= 0 {
		limit = 25
	}

	var total int
	countQuery := `SELECT COUNT(*) FROM placement_tests WHERE ($1::uuid IS NULL OR organization_id = $1)`
	if err := r.db.QueryRow(ctx, countQuery, orgID).Scan(&total); err != nil {
		return nil, 0, err
	}

	query := `
		SELECT id, organization_id, sender_account_id, subject, body_plain, body_html, token, status, created_at, finished_at
		FROM placement_tests
		WHERE ($1::uuid IS NULL OR organization_id = $1)
		ORDER BY created_at DESC
		LIMIT $2 OFFSET $3
	`
	rows, err := r.db.Query(ctx, query, orgID, limit, offset)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	tests := make([]PlacementTest, 0, limit)
	for rows.Next() {
		var t PlacementTest
		if err := rows.Scan(
			&t.ID, &t.OrganizationID, &t.SenderAccountID, &t.Subject, &t.BodyPlain, &t.BodyHTML, &t.Token, &t.Status, &t.CreatedAt, &t.FinishedAt,
		); err != nil {
			return nil, 0, err
		}
		tests = append(tests, t)
	}
	return tests, total, rows.Err()
}

func (r *placementRepository) SetTestStatus(ctx context.Context, id uuid.UUID, status string, finishedAt *time.Time) error {
	_, err := r.db.Exec(ctx,
		`UPDATE placement_tests SET status = $2, finished_at = $3 WHERE id = $1`,
		id, status, finishedAt,
	)
	return err
}

func (r *placementRepository) CreatePendingResult(ctx context.Context, testID, seedAccountID uuid.UUID, provider string) error {
	_, err := r.db.Exec(ctx, `
		INSERT INTO placement_results (test_id, seed_account_id, provider, folder)
		VALUES ($1, $2, $3, 'pending')
		ON CONFLICT (test_id, seed_account_id) DO NOTHING
	`, testID, seedAccountID, provider)
	return err
}

func (r *placementRepository) GetTestWithResults(ctx context.Context, id uuid.UUID) (*PlacementTest, []PlacementResult, error) {
	test, err := r.GetTest(ctx, id)
	if err != nil || test == nil {
		return test, nil, err
	}

	rows, err := r.db.Query(ctx, `
		SELECT id, test_id, seed_account_id, provider, folder, detected_at, raw_flags
		FROM placement_results
		WHERE test_id = $1
		ORDER BY provider ASC, seed_account_id ASC
	`, id)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()

	results := make([]PlacementResult, 0)
	for rows.Next() {
		var res PlacementResult
		if err := rows.Scan(
			&res.ID, &res.TestID, &res.SeedAccountID, &res.Provider, &res.Folder, &res.DetectedAt, &res.RawFlags,
		); err != nil {
			return nil, nil, err
		}
		results = append(results, res)
	}
	return test, results, rows.Err()
}

func (r *placementRepository) RecordResult(ctx context.Context, resultID uuid.UUID, folder, rawFlags string, detectedAt time.Time) error {
	_, err := r.db.Exec(ctx, `
		UPDATE placement_results
		SET folder = $2, raw_flags = $3, detected_at = $4
		WHERE id = $1 AND folder = 'pending'
	`, resultID, folder, rawFlags, detectedAt)
	return err
}

func (r *placementRepository) ListPendingResults(ctx context.Context, limit int) ([]PendingResultJob, error) {
	if limit <= 0 {
		limit = 200
	}
	rows, err := r.db.Query(ctx, `
		SELECT pr.id, pr.test_id, pr.seed_account_id, ea.user_id, pr.provider, pt.token, pt.created_at
		FROM placement_results pr
		JOIN placement_tests pt ON pt.id = pr.test_id
		JOIN email_accounts ea ON ea.id = pr.seed_account_id
		WHERE pr.folder = 'pending'
		ORDER BY pt.created_at ASC
		LIMIT $1
	`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	jobs := make([]PendingResultJob, 0, limit)
	for rows.Next() {
		var j PendingResultJob
		if err := rows.Scan(
			&j.ResultID, &j.TestID, &j.SeedAccountID, &j.SeedUserID, &j.Provider, &j.Token, &j.TestCreatedAt,
		); err != nil {
			return nil, err
		}
		jobs = append(jobs, j)
	}
	return jobs, rows.Err()
}

func (r *placementRepository) CountPendingForTest(ctx context.Context, testID uuid.UUID) (int, error) {
	var n int
	err := r.db.QueryRow(ctx,
		`SELECT COUNT(*) FROM placement_results WHERE test_id = $1 AND folder = 'pending'`,
		testID,
	).Scan(&n)
	return n, err
}

const seedSelectCols = `ea.id, ea.user_id, ea.email, ea.name, ea.provider, ea.status, ea.worker_id, ea.is_seed`

func scanSeed(rows pgx.Rows) (SeedAccount, error) {
	var s SeedAccount
	err := rows.Scan(&s.ID, &s.UserID, &s.Email, &s.Name, &s.Provider, &s.Status, &s.WorkerID, &s.IsSeed)
	return s, err
}

func (r *placementRepository) ListSeedAccounts(ctx context.Context, activeOnly bool) ([]SeedAccount, error) {
	query := `
		SELECT ` + seedSelectCols + `
		FROM email_accounts ea
		WHERE ea.is_seed = true
	`
	if activeOnly {
		query += ` AND ea.status = 'active'`
	}
	query += ` ORDER BY ea.email ASC`

	rows, err := r.db.Query(ctx, query)
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

func (r *placementRepository) GetSeedAccount(ctx context.Context, id uuid.UUID) (*SeedAccount, error) {
	rows, err := r.db.Query(ctx, `
		SELECT `+seedSelectCols+`
		FROM email_accounts ea
		WHERE ea.id = $1
	`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	if !rows.Next() {
		return nil, nil
	}
	s, err := scanSeed(rows)
	if err != nil {
		return nil, err
	}
	return &s, nil
}

func (r *placementRepository) SetIsSeed(ctx context.Context, accountID uuid.UUID, isSeed bool) error {
	_, err := r.db.Exec(ctx,
		`UPDATE email_accounts SET is_seed = $2, updated_at = NOW() WHERE id = $1`,
		accountID, isSeed,
	)
	return err
}

func (r *placementRepository) ListSeedCandidates(ctx context.Context, search string, limit int) ([]SeedAccount, error) {
	if limit <= 0 || limit > 100 {
		limit = 50
	}
	query := `
		SELECT ` + seedSelectCols + `
		FROM email_accounts ea
		WHERE ea.status = 'active'
	`
	args := []any{}
	if search != "" {
		query += ` AND ea.email ILIKE $1`
		args = append(args, "%"+search+"%")
		query += ` ORDER BY ea.is_seed DESC, ea.email ASC LIMIT $2`
		args = append(args, limit)
	} else {
		query += ` ORDER BY ea.is_seed DESC, ea.email ASC LIMIT $1`
		args = append(args, limit)
	}

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

func (r *placementRepository) FindTokenInUnibox(ctx context.Context, userID, seedAccountID uuid.UUID, token string, since time.Time) (*UniboxTokenMatch, error) {
	// The placement token is embedded in the message subject (see
	// placement.Service — the worker injects only the warmup verify header,
	// which we don't control here, so the subject is the carrier that survives
	// sync into the unibox without a worker change). We match the most recent
	// entry for this seed whose subject contains the token. `since` bounds the
	// scan to mail that arrived after the test was created.
	query := `
		SELECT id, subject, flags
		FROM unibox_emails
		WHERE user_id = $1
		  AND email_id = $2
		  AND subject LIKE '%' || $3 || '%'
		  AND internal_date >= $4
		ORDER BY internal_date DESC
		LIMIT 1
	`
	var m UniboxTokenMatch
	err := r.db.QueryRow(ctx, query, userID, seedAccountID, token, since).Scan(&m.InternalID, &m.Subject, &m.Flags)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &m, nil
}
