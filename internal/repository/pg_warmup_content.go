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
	"github.com/warmbly/warmbly/internal/pkg/generation"
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

type WarmupContentPick struct {
	Segment     string
	TaskID      uuid.UUID
	SenderID    uuid.UUID
	RecipientID uuid.UUID
	// IncludeAI admits generated threads; static rows are admitted only when listed in StaticIDs.
	IncludeAI bool
	StaticIDs []uuid.UUID
}

// WarmupContentRepeatWindow is how long a mailbox goes before it may see a conversation again, in either role.
const WarmupContentRepeatWindow = 30 * 24 * time.Hour

// WarmupContentRepository is the data access for the warmup content bank,
// offline generation jobs, generation settings, and content-cohort analytics.
type WarmupContentRepository interface {
	// Conversation bank
	InsertConversation(ctx context.Context, c *models.WarmupConversation) error
	// SeedStaticConversations inserts library rows that are missing and never revives one an admin archived.
	SeedStaticConversations(ctx context.Context, rows []models.WarmupConversation) error
	// PickConversation atomically draws and accounts for an active thread from the shared
	// library, regardless of tier. It prefers what neither mailbox has seen within
	// WarmupContentRepeatWindow, then the least recently seen, so it only returns nil
	// when the library has no eligible row at all.
	PickConversation(ctx context.Context, pick WarmupContentPick) (*models.WarmupConversation, error)
	GetConversation(ctx context.Context, id uuid.UUID) (*models.WarmupConversation, error)
	ListConversations(ctx context.Context, f ConversationFilter) ([]models.WarmupConversation, int, error)
	SetConversationStatus(ctx context.Context, id uuid.UUID, status string) error
	// RefreshUnusedConversations replaces unused stock without retiring active threads.
	RefreshUnusedConversations(ctx context.Context, poolType, segment string, n int) (int, error)
	RetireRiskyConversations(ctx context.Context, since time.Time, minSends, minSpamPlacements int, maxSpamRate float64) (int, error)
	DeleteConversation(ctx context.Context, id uuid.UUID) error
	CountAvailableConversations(ctx context.Context, poolType, segment string) (int, error)
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
	// MarkBatchCancelling records an admin cancel on a job only while it is still
	// in flight, so it never overwrites a job the poller finished meanwhile.
	// It reports whether the job was claimed.
	MarkBatchCancelling(ctx context.Context, id uuid.UUID, reason string) (bool, error)
	GeneratedCountSince(ctx context.Context, since time.Time) (int, error)
	ReservedGenerationCountSince(ctx context.Context, since time.Time) (int, error)
	ExpireStaleScheduledJobs(ctx context.Context, before time.Time) (int64, error)
	WarmupMailboxDemandSince(ctx context.Context, since time.Time) (int, error)

	// Settings (admin_settings key/value)
	GetGenerationSettings(ctx context.Context) (*models.WarmupGenerationSettings, error)
	PutGenerationSettings(ctx context.Context, settings models.WarmupGenerationSettings, updatedBy *uuid.UUID) error

	// Content-cohort A/B analytics
	SpamPlacementByCohort(ctx context.Context, since time.Time) ([]WarmupCohortStat, error)
}

type warmupContentRepository struct {
	db *pgxpool.Pool
}

var ErrWarmupConversationDuplicate = errors.New("warmup subject or opening is already in the conversation library")

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
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if c.SemanticReview == "passed" {
		// Serialize approved inserts so simultaneous batches cannot admit identical content.
		if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended('warmbly:approved-warmup-content',0))`); err != nil {
			return err
		}
		var duplicate bool
		if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM warmup_conversations WHERE id<>$3 AND semantic_review='passed' AND (
 lower(btrim(regexp_replace(subject,'\s+',' ','g')))=lower(btrim(regexp_replace($1::text,'\s+',' ','g'))) OR
 lower(btrim(regexp_replace(description,'\s+',' ','g')))=lower(btrim(regexp_replace($2::text,'\s+',' ','g')))))`, c.Subject, c.Description, c.ID).Scan(&duplicate); err != nil {
			return err
		}
		if duplicate {
			return ErrWarmupConversationDuplicate
		}
	}
	query := `
		INSERT INTO warmup_conversations
			(id, pool_type, segment, source, theme, subject, description, messages, status, lint_passed, reply_eligible, usage_count, generated_by_job_id, scenario_version, rendering_version, semantic_review)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, COALESCE(NULLIF($16, ''), 'legacy_unknown'))
		ON CONFLICT (id) DO NOTHING
	`
	_, err = tx.Exec(ctx, query,
		c.ID, c.PoolType, c.Segment, c.Source, c.Theme, c.Subject, c.Description,
		msgs, c.Status, c.LintPassed, c.ReplyEligible, c.UsageCount, c.GeneratedByJob, c.ScenarioVersion, c.RenderingVersion, c.SemanticReview,
	)
	if err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func scanConversation(row pgx.Row) (*models.WarmupConversation, error) {
	var c models.WarmupConversation
	var msgs []byte
	err := row.Scan(
		&c.ID, &c.PoolType, &c.Segment, &c.Source, &c.Theme, &c.Subject, &c.Description,
		&msgs, &c.Status, &c.LintPassed, &c.ReplyEligible, &c.UsageCount, &c.GeneratedByJob, &c.CreatedAt, &c.UpdatedAt, &c.ScenarioVersion, &c.RenderingVersion, &c.SemanticReview,
	)
	if err != nil {
		return nil, err
	}
	if len(msgs) > 0 {
		_ = json.Unmarshal(msgs, &c.Messages)
	}
	return &c, nil
}

const conversationCols = `id, pool_type, segment, source, theme, subject, description, messages, status, lint_passed, reply_eligible, usage_count, generated_by_job_id, created_at, updated_at, scenario_version, rendering_version, semantic_review`
const qualifiedConversationCols = `c.id, c.pool_type, c.segment, c.source, c.theme, c.subject, c.description, c.messages, c.status, c.lint_passed, c.reply_eligible, c.usage_count, c.generated_by_job_id, c.created_at, c.updated_at, c.scenario_version, c.rendering_version, c.semantic_review`

// Opening eligibility is independent of existing threads' continuation authority.
// openingConversationWhere is the eligibility predicate for a new thread, qualified by table alias t.
func openingConversationWhere(t string) string {
	return t + "status='active' AND " + t + "reply_eligible AND " + t + "lint_passed AND " + t + "semantic_review='passed' AND " +
		t + "scenario_version='" + generation.WarmupScenarioVersion + "' AND " + t + "rendering_version='" + generation.CanonicalRenderingVersion + "'"
}

var openingConversationSQL = openingConversationWhere("")

func (r *warmupContentRepository) PickConversation(ctx context.Context, pick WarmupContentPick) (*models.WarmupConversation, error) {
	if pick.TaskID == uuid.Nil || pick.SenderID == uuid.Nil || pick.RecipientID == uuid.Nil || pick.SenderID == pick.RecipientID {
		return nil, errors.New("warmup content requires a task and distinct mailbox participants")
	}
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var task uuid.UUID
	if err := tx.QueryRow(ctx, `SELECT id FROM tasks WHERE id=$1 AND email_account_id=$2 FOR UPDATE`, pick.TaskID, pick.SenderID).Scan(&task); err != nil {
		return nil, err
	}
	// A retried task resends what it already reserved; created_at on a reservation is when the mailbox last saw the conversation.
	var existing uuid.UUID
	err = tx.QueryRow(ctx, `SELECT conversation_id FROM warmup_conversation_mailboxes WHERE task_id=$1 LIMIT 1`, pick.TaskID).Scan(&existing)
	if err == nil {
		c, err := scanConversation(tx.QueryRow(ctx, `SELECT `+conversationCols+` FROM warmup_conversations WHERE id=$1 AND `+openingConversationSQL+`
 AND (SELECT COUNT(*) FROM warmup_conversation_mailboxes WHERE task_id=$2 AND conversation_id=$1 AND email_account_id IN ($3,$4))=2`, existing, pick.TaskID, pick.SenderID, pick.RecipientID))
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return c, err
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return nil, err
	}
	staticIDs := pick.StaticIDs
	if staticIDs == nil {
		staticIDs = []uuid.UUID{}
	}
	query := `
		WITH seen AS (
			SELECT conversation_id, MAX(created_at) AS last_seen
			FROM warmup_conversation_mailboxes
			WHERE email_account_id IN ($2,$3)
			GROUP BY conversation_id
		), picked AS (
			SELECT c.id
			FROM warmup_conversations c
			LEFT JOIN seen ON seen.conversation_id = c.id
			WHERE ` + openingConversationWhere("c.") + ` AND (c.segment = $1 OR c.segment = '')
			  AND ((c.source = 'ai' AND $4) OR (c.source = 'static' AND c.id = ANY($5)))
			ORDER BY (seen.last_seen IS NOT NULL AND seen.last_seen > NOW() - $6::interval), (c.segment = $1) DESC,
			         seen.last_seen ASC NULLS FIRST, c.usage_count ASC, random()
			LIMIT 1
			FOR UPDATE OF c SKIP LOCKED
		)
		UPDATE warmup_conversations AS c
		SET usage_count = c.usage_count + 1,
			updated_at = NOW()
		FROM picked
		WHERE c.id = picked.id
		RETURNING ` + qualifiedConversationCols + `
	`
	c, err := scanConversation(tx.QueryRow(ctx, query, pick.Segment, pick.SenderID, pick.RecipientID, pick.IncludeAI, staticIDs, WarmupContentRepeatWindow.String()))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if _, err := tx.Exec(ctx, `INSERT INTO warmup_conversation_mailboxes(conversation_id,email_account_id,task_id)
 VALUES($1,$2,$4),($1,$3,$4)
 ON CONFLICT (conversation_id,email_account_id) DO UPDATE SET task_id=EXCLUDED.task_id, created_at=NOW()`, c.ID, pick.SenderID, pick.RecipientID, pick.TaskID); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return c, nil
}

func (r *warmupContentRepository) SeedStaticConversations(ctx context.Context, rows []models.WarmupConversation) error {
	batch := &pgx.Batch{}
	for _, c := range rows {
		msgs, err := json.Marshal(c.Messages)
		if err != nil {
			return err
		}
		batch.Queue(`INSERT INTO warmup_conversations
 (id, pool_type, segment, source, theme, subject, description, messages, status, lint_passed, reply_eligible, usage_count, scenario_version, rendering_version, semantic_review)
 VALUES ($1,$2,$3,'static',$4,$5,$6,$7,$8,$9,$10,0,$11,$12,$13)
 ON CONFLICT (id) DO NOTHING`, c.ID, c.PoolType, c.Segment, c.Theme, c.Subject, c.Description, msgs, c.Status, c.LintPassed, c.ReplyEligible, c.ScenarioVersion, c.RenderingVersion, c.SemanticReview)
	}
	return r.db.SendBatch(ctx, batch).Close()
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
	tag, err := r.db.Exec(ctx, `UPDATE warmup_conversations SET status = $2, updated_at = NOW() WHERE id = $1 AND ($2 != 'active' OR semantic_review IN ('passed', 'legacy_unknown'))`, id, status)
	if err == nil && tag.RowsAffected() == 0 {
		return errors.New("conversation missing or semantic review does not permit activation")
	}
	return err
}

func (r *warmupContentRepository) RefreshUnusedConversations(ctx context.Context, poolType, segment string, n int) (int, error) {
	tag, err := r.db.Exec(ctx, `
		UPDATE warmup_conversations SET status = 'archived', updated_at = NOW()
		WHERE usage_count=0 AND id IN (
			SELECT id FROM warmup_conversations
			WHERE pool_type = $1 AND segment = $2 AND `+openingConversationSQL+` AND source = 'ai' AND usage_count=0
			ORDER BY created_at ASC
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

// CountAvailableConversations is the generated stock the two most-exposed mailboxes have not seen within the repeat window.
func (r *warmupContentRepository) CountAvailableConversations(ctx context.Context, poolType, segment string) (int, error) {
	var n int
	err := r.db.QueryRow(ctx, `
 WITH available AS MATERIALIZED (
   SELECT id FROM warmup_conversations
   WHERE pool_type=$1 AND segment=$2 AND source='ai' AND `+openingConversationSQL+`
 ), exposure AS (
   SELECT COUNT(*) AS seen_count FROM warmup_conversation_mailboxes used
   JOIN available a ON a.id=used.conversation_id
   JOIN email_accounts ea ON ea.id=used.email_account_id
   WHERE used.created_at > NOW() - $3::interval
     AND ea.status='active' AND ea.warmup IS NOT NULL
     AND ea.warmup_paused_at IS NULL AND ea.warmup_stopped_at IS NULL
   GROUP BY used.email_account_id
 )
 SELECT GREATEST(0, (SELECT COUNT(*) FROM available)
   - COALESCE((SELECT SUM(seen_count) FROM (SELECT seen_count FROM exposure ORDER BY seen_count DESC LIMIT 2) most_exposed),0))
 `, poolType, segment, WarmupContentRepeatWindow.String()).Scan(&n)
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

const warmupGenerationPolicyLockID int64 = 1791368245

func (r *warmupContentRepository) CreateGenerationJob(ctx context.Context, j *models.WarmupGenerationJob) error {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock($1)`, warmupGenerationPolicyLockID); err != nil {
		return err
	}
	settings, err := getGenerationSettings(ctx, tx)
	if err != nil {
		return err
	}
	if !settings.GenerationEnabled {
		return errors.New("warmup content generation is stopped")
	}
	if settings.DailyGenerationCap > 0 {
		var reserved int
		if err := tx.QueryRow(ctx, `SELECT COALESCE(SUM(requested_count), 0) FROM warmup_generation_jobs WHERE created_at >= $1`, time.Now().UTC().Truncate(24*time.Hour)).Scan(&reserved); err != nil {
			return err
		}
		if j.RequestedCount <= 0 || j.RequestedCount > settings.DailyGenerationCap-reserved {
			return errors.New("daily generation reservation cap reached")
		}
	}
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
			 batch_id, batch_input_file_id, batch_output_file_id, batch_status, completion_window, content_version, max_messages_per_thread)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17)
	`
	_, err = tx.Exec(ctx, query,
		j.ID, j.RequestedBy, j.Trigger, j.Mode, j.PoolType, j.Segment, j.Theme, j.Model, j.RequestedCount, j.Status,
		j.BatchID, j.BatchInputFileID, j.BatchOutputFileID, j.BatchStatus, j.CompletionWindow, j.ContentVersion, j.MaxMessagesPerThread,
	)
	if err != nil {
		return err
	}
	return tx.Commit(ctx)
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

func (r *warmupContentRepository) MarkBatchCancelling(ctx context.Context, id uuid.UUID, reason string) (bool, error) {
	tag, err := r.db.Exec(ctx, `
		UPDATE warmup_generation_jobs
		SET batch_status = 'cancelling',
			error = CASE WHEN error = '' THEN $2 ELSE error END,
			updated_at = NOW()
		WHERE id = $1
		  AND status = 'running'
		  AND batch_status NOT IN ('completed', 'failed', 'expired', 'cancelled', 'cancelling')
	`, id, reason)
	if err != nil {
		return false, err
	}
	return tag.RowsAffected() > 0, nil
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

const generationJobCols = `id, requested_by, trigger, mode, pool_type, segment, theme, model, requested_count, generated_count, lint_rejected_count, failed_count, status, error, batch_id, batch_input_file_id, batch_output_file_id, batch_status, completion_window, started_at, finished_at, created_at, updated_at, content_version, max_messages_per_thread`

func scanGenerationJob(row pgx.Row) (*models.WarmupGenerationJob, error) {
	var j models.WarmupGenerationJob
	err := row.Scan(
		&j.ID, &j.RequestedBy, &j.Trigger, &j.Mode, &j.PoolType, &j.Segment, &j.Theme, &j.Model,
		&j.RequestedCount, &j.GeneratedCount, &j.LintRejectedCount, &j.FailedCount,
		&j.Status, &j.Error, &j.BatchID, &j.BatchInputFileID, &j.BatchOutputFileID, &j.BatchStatus, &j.CompletionWindow,
		&j.StartedAt, &j.FinishedAt, &j.CreatedAt, &j.UpdatedAt, &j.ContentVersion, &j.MaxMessagesPerThread,
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

func (r *warmupContentRepository) WarmupMailboxDemandSince(ctx context.Context, since time.Time) (int, error) {
	var sends int
	err := r.db.QueryRow(ctx, `
		SELECT COALESCE(MAX(exposures),0)::integer FROM (
          SELECT COUNT(DISTINCT conversation_id) AS exposures FROM (
            SELECT sender_account_id AS mailbox,conversation_id FROM warmup_tokens WHERE created_at >= $1 AND conversation_id IS NOT NULL
            UNION ALL
            SELECT recipient_account_id AS mailbox,conversation_id FROM warmup_tokens WHERE created_at >= $1 AND conversation_id IS NOT NULL
          ) participants GROUP BY mailbox
        ) demand
	`, since).Scan(&sends)
	return sends, err
}

func (r *warmupContentRepository) ReservedGenerationCountSince(ctx context.Context, since time.Time) (int, error) {
	var n int
	err := r.db.QueryRow(ctx, `SELECT COALESCE(SUM(requested_count), 0) FROM warmup_generation_jobs WHERE created_at >= $1`, since).Scan(&n)
	return n, err
}

func (r *warmupContentRepository) GetGenerationSettings(ctx context.Context) (*models.WarmupGenerationSettings, error) {
	return getGenerationSettings(ctx, r.db)
}

type generationSettingsReader interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}

func getGenerationSettings(ctx context.Context, db generationSettingsReader) (*models.WarmupGenerationSettings, error) {
	var raw []byte
	err := db.QueryRow(ctx, `SELECT value FROM admin_settings WHERE key = $1`, models.AdminSettingsKeyWarmupGeneration).Scan(&raw)
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

func (r *warmupContentRepository) PutGenerationSettings(ctx context.Context, settings models.WarmupGenerationSettings, updatedBy *uuid.UUID) error {
	settings.Normalize()
	raw, err := json.Marshal(settings)
	if err != nil {
		return err
	}
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock($1)`, warmupGenerationPolicyLockID); err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `INSERT INTO admin_settings (key, value, updated_by, updated_at) VALUES ($1, $2, $3, now())
		ON CONFLICT (key) DO UPDATE SET value = EXCLUDED.value, updated_by = EXCLUDED.updated_by, updated_at = now()`,
		models.AdminSettingsKeyWarmupGeneration, raw, updatedBy)
	if err != nil {
		return err
	}
	return tx.Commit(ctx)
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
