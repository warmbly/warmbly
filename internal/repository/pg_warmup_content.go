package repository

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/warmbly/warmbly/internal/models"
)

// ConversationFilter narrows a warmup_conversations listing.
type ConversationFilter struct {
	PoolType string
	Segment  string
	Source   string
	Status   string
	Limit    int
	Offset   int
}

// WarmupConversationStat is a grouped count for the admin overview.
type WarmupConversationStat struct {
	PoolType string `json:"pool_type"`
	Segment  string `json:"segment"`
	Source   string `json:"source"`
	Active   int    `json:"active"`
	Archived int    `json:"archived"`
}

// WarmupCohortStat is the per-content-source spam-placement aggregate.
type WarmupCohortStat struct {
	ContentSource  string `json:"content_source"`
	Sent           int    `json:"sent"`
	SpamPlacements int    `json:"spam_placements"`
}

// WarmupContentRepository is the data access for the warmup content bank,
// offline generation jobs, generation settings, and content-cohort analytics.
type WarmupContentRepository interface {
	// Conversation bank
	InsertConversation(ctx context.Context, c *models.WarmupConversation) error
	// PickConversation atomically draws and accounts for an active thread from the SHARED
	// content library, regardless of tier: free/premium separate which mailboxes
	// warm together (reputation isolation), not what they say, so content is not
	// split by pool. Prefers an exact segment match, falls back to generic.
	PickConversation(ctx context.Context, segment string) (*models.WarmupConversation, error)
	GetConversation(ctx context.Context, id uuid.UUID) (*models.WarmupConversation, error)
	ListConversations(ctx context.Context, f ConversationFilter) ([]models.WarmupConversation, int, error)
	SetConversationStatus(ctx context.Context, id uuid.UUID, status string) error
	// RetireMostUsedConversations archives the n most-used active AI threads
	// for a segment so the scheduler can replace them with fresh generations.
	// Static-source rows are never touched (they aren't regenerable).
	RetireMostUsedConversations(ctx context.Context, poolType, segment string, n int) (int, error)
	RetireRiskyConversations(ctx context.Context, since time.Time, minSends, minSpamPlacements int, maxSpamRate float64) (int, error)
	DeleteConversation(ctx context.Context, id uuid.UUID) error
	CountActiveConversations(ctx context.Context, poolType, segment string) (int, error)
	ConversationStats(ctx context.Context) ([]WarmupConversationStat, error)
	LastGeneratedAt(ctx context.Context) (*time.Time, error)

	// Generation jobs
	CreateGenerationJob(ctx context.Context, j *models.WarmupGenerationJob) error
	UpdateGenerationJob(ctx context.Context, j *models.WarmupGenerationJob) error
	GetGenerationJob(ctx context.Context, id uuid.UUID) (*models.WarmupGenerationJob, error)
	ListGenerationJobs(ctx context.Context, limit, offset int) ([]models.WarmupGenerationJob, int, error)
	// ListActiveBatchJobs returns batch-mode jobs still in flight (running with a
	// non-terminal OpenAI batch status), for the poller to reconcile.
	ListActiveBatchJobs(ctx context.Context) ([]models.WarmupGenerationJob, error)
	GeneratedCountSince(ctx context.Context, since time.Time) (int, error)
	ExpireStaleScheduledJobs(ctx context.Context, before time.Time) (int64, error)
	WarmupSendsSince(ctx context.Context, since time.Time) (int, error)

	// Settings (admin_settings key/value)
	GetGenerationSettings(ctx context.Context) (*models.WarmupGenerationSettings, error)

	// Content-cohort A/B analytics
	SpamPlacementByCohort(ctx context.Context, since time.Time) ([]WarmupCohortStat, error)
}

type warmupContentRepository struct {
	db *pgxpool.Pool
}

// NewWarmupContentRepository creates a new warmup content repository.
func NewWarmupContentRepository(db *pgxpool.Pool) WarmupContentRepository {
	return &warmupContentRepository{db: db}
}

func (r *warmupContentRepository) InsertConversation(ctx context.Context, c *models.WarmupConversation) error {
	if c.ID == uuid.Nil {
		c.ID = uuid.New()
	}
	msgs, err := json.Marshal(c.Messages)
	if err != nil {
		return err
	}
	query := `
		INSERT INTO warmup_conversations
			(id, pool_type, segment, source, theme, subject, description, messages, status, lint_passed, reply_eligible, usage_count, generated_by_job_id)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13)
	`
	_, err = r.db.Exec(ctx, query,
		c.ID, c.PoolType, c.Segment, c.Source, c.Theme, c.Subject, c.Description,
		msgs, c.Status, c.LintPassed, c.ReplyEligible, c.UsageCount, c.GeneratedByJob,
	)
	return err
}

func scanConversation(row pgx.Row) (*models.WarmupConversation, error) {
	var c models.WarmupConversation
	var msgs []byte
	err := row.Scan(
		&c.ID, &c.PoolType, &c.Segment, &c.Source, &c.Theme, &c.Subject, &c.Description,
		&msgs, &c.Status, &c.LintPassed, &c.ReplyEligible, &c.UsageCount, &c.GeneratedByJob, &c.CreatedAt, &c.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}
	if len(msgs) > 0 {
		_ = json.Unmarshal(msgs, &c.Messages)
	}
	return &c, nil
}

const conversationCols = `id, pool_type, segment, source, theme, subject, description, messages, status, lint_passed, reply_eligible, usage_count, generated_by_job_id, created_at, updated_at`
const qualifiedConversationCols = `c.id, c.pool_type, c.segment, c.source, c.theme, c.subject, c.description, c.messages, c.status, c.lint_passed, c.reply_eligible, c.usage_count, c.generated_by_job_id, c.created_at, c.updated_at`

// PickConversation returns a lightly-used active conversation from the shared
// library and increments its usage in the same statement. Keeping selection and
// accounting atomic prevents hot threads and removes one database round trip
// from every warmup send. A small random tie-break keeps concurrent senders from
// marching through the bank in the same order.
func (r *warmupContentRepository) PickConversation(ctx context.Context, segment string) (*models.WarmupConversation, error) {
	query := `
		WITH picked AS (
			SELECT id
			FROM warmup_conversations
			WHERE status = 'active' AND (segment = $1 OR segment = '')
			ORDER BY (segment = $1) DESC, usage_count ASC, random()
			LIMIT 1
			FOR UPDATE SKIP LOCKED
		)
		UPDATE warmup_conversations AS c
		SET usage_count = c.usage_count + 1,
			updated_at = NOW()
		FROM picked
		WHERE c.id = picked.id
		RETURNING ` + qualifiedConversationCols + `
	`
	c, err := scanConversation(r.db.QueryRow(ctx, query, segment))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	return c, err
}

func (r *warmupContentRepository) GetConversation(ctx context.Context, id uuid.UUID) (*models.WarmupConversation, error) {
	query := `SELECT ` + conversationCols + ` FROM warmup_conversations WHERE id = $1`
	c, err := scanConversation(r.db.QueryRow(ctx, query, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	return c, err
}

func (r *warmupContentRepository) ListConversations(ctx context.Context, f ConversationFilter) ([]models.WarmupConversation, int, error) {
	where := `WHERE 1=1`
	args := []any{}
	add := func(clause string, val any) {
		args = append(args, val)
		where += clause
	}
	if f.PoolType != "" {
		add(" AND pool_type = $"+itoa(len(args)+1), f.PoolType)
	}
	if f.Segment != "" {
		add(" AND segment = $"+itoa(len(args)+1), f.Segment)
	}
	if f.Source != "" {
		add(" AND source = $"+itoa(len(args)+1), f.Source)
	}
	if f.Status != "" {
		add(" AND status = $"+itoa(len(args)+1), f.Status)
	}

	var total int
	if err := r.db.QueryRow(ctx, `SELECT COUNT(*) FROM warmup_conversations `+where, args...).Scan(&total); err != nil {
		return nil, 0, err
	}

	limit := f.Limit
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	args = append(args, limit)
	limitIdx := itoa(len(args))
	args = append(args, f.Offset)
	offsetIdx := itoa(len(args))

	query := `SELECT ` + conversationCols + ` FROM warmup_conversations ` + where +
		` ORDER BY created_at DESC LIMIT $` + limitIdx + ` OFFSET $` + offsetIdx
	rows, err := r.db.Query(ctx, query, args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	out := []models.WarmupConversation{}
	for rows.Next() {
		c, err := scanConversation(rows)
		if err != nil {
			return nil, 0, err
		}
		out = append(out, *c)
	}
	return out, total, rows.Err()
}

func (r *warmupContentRepository) SetConversationStatus(ctx context.Context, id uuid.UUID, status string) error {
	_, err := r.db.Exec(ctx, `UPDATE warmup_conversations SET status = $2, updated_at = NOW() WHERE id = $1`, id, status)
	return err
}

func (r *warmupContentRepository) RetireMostUsedConversations(ctx context.Context, poolType, segment string, n int) (int, error) {
	tag, err := r.db.Exec(ctx, `
		UPDATE warmup_conversations SET status = 'archived', updated_at = NOW()
		WHERE id IN (
			SELECT id FROM warmup_conversations
			WHERE pool_type = $1 AND segment = $2 AND status = 'active' AND source = 'ai'
			ORDER BY usage_count DESC, created_at ASC
			LIMIT $3
		)`, poolType, segment, n)
	if err != nil {
		return 0, err
	}
	return int(tag.RowsAffected()), nil
}

// RetireRiskyConversations removes generated threads whose observed placement
// is clearly unsafe. A minimum sample and absolute placement count prevent one
// noisy delivery from retiring otherwise healthy content.
func (r *warmupContentRepository) RetireRiskyConversations(
	ctx context.Context,
	since time.Time,
	minSends, minSpamPlacements int,
	maxSpamRate float64,
) (int, error) {
	tag, err := r.db.Exec(ctx, `
		WITH performance AS (
			SELECT wt.conversation_id,
			       COUNT(DISTINCT wt.token) AS sends,
			       COUNT(DISTINCT wsr.id) AS spam_placements
			FROM warmup_tokens wt
			JOIN tasks t ON t.id = wt.task_id
			LEFT JOIN warmup_spam_reports wsr
			  ON wsr.message_id = t.message_id
			 AND wsr.report_type = 'spam_placement'
			WHERE wt.content_source = 'ai'
			  AND wt.conversation_id IS NOT NULL
			  AND wt.created_at >= $1
			GROUP BY wt.conversation_id
		), risky AS (
			SELECT conversation_id
			FROM performance
			WHERE sends >= $2
			  AND spam_placements >= $3
			  AND spam_placements::double precision / sends::double precision >= $4
		)
		UPDATE warmup_conversations c
		SET status = 'archived', reply_eligible = false, updated_at = NOW()
		FROM risky
		WHERE c.id = risky.conversation_id
		  AND c.status = 'active'
		  AND c.source = 'ai'
	`, since, minSends, minSpamPlacements, maxSpamRate)
	if err != nil {
		return 0, err
	}
	return int(tag.RowsAffected()), nil
}

func (r *warmupContentRepository) DeleteConversation(ctx context.Context, id uuid.UUID) error {
	_, err := r.db.Exec(ctx, `DELETE FROM warmup_conversations WHERE id = $1`, id)
	return err
}

func (r *warmupContentRepository) CountActiveConversations(ctx context.Context, poolType, segment string) (int, error) {
	var n int
	err := r.db.QueryRow(ctx,
		`SELECT COUNT(*) FROM warmup_conversations WHERE pool_type = $1 AND segment = $2 AND status = 'active'`,
		poolType, segment).Scan(&n)
	return n, err
}

func (r *warmupContentRepository) ConversationStats(ctx context.Context) ([]WarmupConversationStat, error) {
	query := `
		SELECT pool_type, segment, source,
			COUNT(*) FILTER (WHERE status = 'active')   AS active,
			COUNT(*) FILTER (WHERE status = 'archived') AS archived
		FROM warmup_conversations
		GROUP BY pool_type, segment, source
		ORDER BY pool_type, segment, source
	`
	rows, err := r.db.Query(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []WarmupConversationStat{}
	for rows.Next() {
		var s WarmupConversationStat
		if err := rows.Scan(&s.PoolType, &s.Segment, &s.Source, &s.Active, &s.Archived); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

func (r *warmupContentRepository) LastGeneratedAt(ctx context.Context) (*time.Time, error) {
	var t *time.Time
	err := r.db.QueryRow(ctx, `SELECT MAX(created_at) FROM warmup_conversations WHERE source = 'ai'`).Scan(&t)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	return t, err
}

func (r *warmupContentRepository) CreateGenerationJob(ctx context.Context, j *models.WarmupGenerationJob) error {
	if j.ID == uuid.Nil {
		j.ID = uuid.New()
	}
	if j.Mode == "" {
		j.Mode = models.WarmupGenerationModeSync
	}
	if j.CompletionWindow == "" {
		j.CompletionWindow = "24h"
	}
	query := `
		INSERT INTO warmup_generation_jobs
			(id, requested_by, trigger, mode, pool_type, segment, theme, model, requested_count, status,
			 batch_id, batch_input_file_id, batch_output_file_id, batch_status, completion_window)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15)
	`
	_, err := r.db.Exec(ctx, query,
		j.ID, j.RequestedBy, j.Trigger, j.Mode, j.PoolType, j.Segment, j.Theme, j.Model, j.RequestedCount, j.Status,
		j.BatchID, j.BatchInputFileID, j.BatchOutputFileID, j.BatchStatus, j.CompletionWindow,
	)
	return err
}

func (r *warmupContentRepository) UpdateGenerationJob(ctx context.Context, j *models.WarmupGenerationJob) error {
	query := `
		UPDATE warmup_generation_jobs SET
			generated_count = $2,
			lint_rejected_count = $3,
			failed_count = $4,
			status = $5,
			error = $6,
			started_at = $7,
			finished_at = $8,
			batch_id = $9,
			batch_input_file_id = $10,
			batch_output_file_id = $11,
			batch_status = $12,
			completion_window = $13,
			updated_at = NOW()
		WHERE id = $1
	`
	_, err := r.db.Exec(ctx, query,
		j.ID, j.GeneratedCount, j.LintRejectedCount, j.FailedCount, j.Status, j.Error, j.StartedAt, j.FinishedAt,
		j.BatchID, j.BatchInputFileID, j.BatchOutputFileID, j.BatchStatus, j.CompletionWindow,
	)
	return err
}

// ExpireStaleScheduledJobs releases reservations left behind if a backend dies
// after reserving a batch but before receiving the provider batch ID.
func (r *warmupContentRepository) ExpireStaleScheduledJobs(ctx context.Context, before time.Time) (int64, error) {
	tag, err := r.db.Exec(ctx, `
		UPDATE warmup_generation_jobs
		SET status = 'failed',
			error = 'stale scheduled batch reservation',
			finished_at = NOW(),
			updated_at = NOW()
		WHERE trigger = 'schedule'
		  AND status = 'pending'
		  AND batch_id = ''
		  AND created_at < $1
	`, before)
	if err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
}

const generationJobCols = `id, requested_by, trigger, mode, pool_type, segment, theme, model, requested_count, generated_count, lint_rejected_count, failed_count, status, error, batch_id, batch_input_file_id, batch_output_file_id, batch_status, completion_window, started_at, finished_at, created_at, updated_at`

func scanGenerationJob(row pgx.Row) (*models.WarmupGenerationJob, error) {
	var j models.WarmupGenerationJob
	err := row.Scan(
		&j.ID, &j.RequestedBy, &j.Trigger, &j.Mode, &j.PoolType, &j.Segment, &j.Theme, &j.Model,
		&j.RequestedCount, &j.GeneratedCount, &j.LintRejectedCount, &j.FailedCount,
		&j.Status, &j.Error, &j.BatchID, &j.BatchInputFileID, &j.BatchOutputFileID, &j.BatchStatus, &j.CompletionWindow,
		&j.StartedAt, &j.FinishedAt, &j.CreatedAt, &j.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}
	return &j, nil
}

func (r *warmupContentRepository) GetGenerationJob(ctx context.Context, id uuid.UUID) (*models.WarmupGenerationJob, error) {
	j, err := scanGenerationJob(r.db.QueryRow(ctx, `SELECT `+generationJobCols+` FROM warmup_generation_jobs WHERE id = $1`, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	return j, err
}

func (r *warmupContentRepository) ListGenerationJobs(ctx context.Context, limit, offset int) ([]models.WarmupGenerationJob, int, error) {
	var total int
	if err := r.db.QueryRow(ctx, `SELECT COUNT(*) FROM warmup_generation_jobs`).Scan(&total); err != nil {
		return nil, 0, err
	}
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	rows, err := r.db.Query(ctx,
		`SELECT `+generationJobCols+` FROM warmup_generation_jobs ORDER BY created_at DESC LIMIT $1 OFFSET $2`,
		limit, offset)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	out := []models.WarmupGenerationJob{}
	for rows.Next() {
		j, err := scanGenerationJob(rows)
		if err != nil {
			return nil, 0, err
		}
		out = append(out, *j)
	}
	return out, total, rows.Err()
}

// ListActiveBatchJobs returns batch-mode jobs that are still running with a
// non-terminal OpenAI batch status. Terminal statuses (completed/failed/expired/
// cancelled) and a terminal job status are excluded so the poller only touches
// in-flight work. An empty batch_status (just submitted, not yet polled) is
// treated as active.
func (r *warmupContentRepository) ListActiveBatchJobs(ctx context.Context) ([]models.WarmupGenerationJob, error) {
	query := `SELECT ` + generationJobCols + ` FROM warmup_generation_jobs
		WHERE mode = 'batch'
		  AND status = 'running'
		  AND batch_status NOT IN ('completed', 'failed', 'expired', 'cancelled')
		  AND batch_id <> ''
		ORDER BY created_at ASC`
	rows, err := r.db.Query(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []models.WarmupGenerationJob{}
	for rows.Next() {
		j, err := scanGenerationJob(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *j)
	}
	return out, rows.Err()
}

func (r *warmupContentRepository) GeneratedCountSince(ctx context.Context, since time.Time) (int, error) {
	var n int
	err := r.db.QueryRow(ctx,
		`SELECT COALESCE(SUM(generated_count), 0) FROM warmup_generation_jobs WHERE created_at >= $1`, since).Scan(&n)
	return n, err
}

func (r *warmupContentRepository) WarmupSendsSince(ctx context.Context, since time.Time) (int, error) {
	var sends int
	err := r.db.QueryRow(ctx, `
		SELECT COALESCE(SUM(emails_sent), 0)::integer
		FROM warmup_statistics
		WHERE date >= $1::date
	`, since).Scan(&sends)
	return sends, err
}

func (r *warmupContentRepository) GetGenerationSettings(ctx context.Context) (*models.WarmupGenerationSettings, error) {
	var raw []byte
	err := r.db.QueryRow(ctx, `SELECT value FROM admin_settings WHERE key = $1`, models.AdminSettingsKeyWarmupGeneration).Scan(&raw)
	if errors.Is(err, pgx.ErrNoRows) {
		def := models.DefaultWarmupGenerationSettings()
		return &def, nil
	}
	if err != nil {
		return nil, err
	}
	s := models.DefaultWarmupGenerationSettings()
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &s); err != nil {
			return nil, err
		}
	}
	s.Normalize()
	return &s, nil
}

func (r *warmupContentRepository) SpamPlacementByCohort(ctx context.Context, since time.Time) ([]WarmupCohortStat, error) {
	stats := map[string]*WarmupCohortStat{}
	get := func(src string) *WarmupCohortStat {
		if src == "" {
			src = models.WarmupContentSourceStatic
		}
		if s, ok := stats[src]; ok {
			return s
		}
		s := &WarmupCohortStat{ContentSource: src}
		stats[src] = s
		return s
	}

	sentRows, err := r.db.Query(ctx,
		`SELECT content_source, COUNT(*) FROM warmup_tokens WHERE created_at >= $1 GROUP BY content_source`, since)
	if err != nil {
		return nil, err
	}
	for sentRows.Next() {
		var src string
		var n int
		if err := sentRows.Scan(&src, &n); err != nil {
			sentRows.Close()
			return nil, err
		}
		get(src).Sent += n
	}
	sentRows.Close()
	if err := sentRows.Err(); err != nil {
		return nil, err
	}

	placeRows, err := r.db.Query(ctx,
		`SELECT content_source, COUNT(*) FROM warmup_spam_reports WHERE report_type = 'spam_placement' AND created_at >= $1 GROUP BY content_source`, since)
	if err != nil {
		return nil, err
	}
	for placeRows.Next() {
		var src string
		var n int
		if err := placeRows.Scan(&src, &n); err != nil {
			placeRows.Close()
			return nil, err
		}
		get(src).SpamPlacements += n
	}
	placeRows.Close()
	if err := placeRows.Err(); err != nil {
		return nil, err
	}

	out := make([]WarmupCohortStat, 0, len(stats))
	for _, s := range stats {
		out = append(out, *s)
	}
	return out, nil
}
