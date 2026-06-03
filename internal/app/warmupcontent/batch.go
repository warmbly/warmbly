package warmupcontent

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/getsentry/sentry-go"
	"github.com/google/uuid"
	"github.com/rs/zerolog/log"

	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/pkg/generation"
	"github.com/warmbly/warmbly/internal/pkg/humanlint"
	"github.com/warmbly/warmbly/internal/pkg/warmlint"
)

// themeForIndex mirrors runJob's theme selection: a pinned theme wins, otherwise
// rotate through the default theme set so a batch spans varied topics.
func themeForIndex(pinned string, i int) string {
	if pinned != "" {
		return pinned
	}
	return defaultThemes[i%len(defaultThemes)]
}

// GenerateBatch submits an async OpenAI Batch API run. It builds N
// chat-completion requests (theme rotation identical to the sync runJob),
// uploads them as a batch, and persists a job row in mode='batch' with the
// OpenAI batch/file identifiers. It returns immediately — the batch is ingested
// later by PollBatches when OpenAI finishes processing (up to the completion
// window, typically 24h).
func (s *service) GenerateBatch(ctx context.Context, req GenerateRequest) (uuid.UUID, error) {
	if s.gen == nil {
		return uuid.Nil, ErrNotConfigured
	}
	if req.PoolType == "" {
		req.PoolType = "premium"
	}
	if req.Trigger == "" {
		req.Trigger = "manual"
	}
	if req.Count <= 0 {
		req.Count = 100
	}
	if req.Count > maxPerBatch {
		req.Count = maxPerBatch
	}

	settings, _ := s.repo.GetGenerationSettings(ctx)
	if req.Model == "" && settings != nil {
		req.Model = settings.Model
	}
	maxMessages := 4
	if settings != nil {
		maxMessages = settings.MaxMessagesPerThread
	}
	if req.MaxMessages > 0 {
		maxMessages = req.MaxMessages
	}

	// Respect the daily generation cap (shared with the sync/scheduled paths) so
	// a huge batch can't blow past the admin's budget for the day.
	if settings != nil && settings.DailyGenerationCap > 0 {
		remaining := dailyRemaining(ctx, s.repo, settings.DailyGenerationCap)
		if remaining <= 0 {
			return uuid.Nil, fmt.Errorf("daily generation cap reached")
		}
		if req.Count > remaining {
			req.Count = remaining
		}
	}

	window := req.CompletionWindow
	if window == "" {
		window = "24h"
	}

	job := &models.WarmupGenerationJob{
		ID:               uuid.New(),
		RequestedBy:      req.RequestedBy,
		Trigger:          req.Trigger,
		Mode:             models.WarmupGenerationModeBatch,
		PoolType:         req.PoolType,
		Segment:          req.Segment,
		Theme:            req.Theme,
		Model:            req.Model,
		RequestedCount:   req.Count,
		Status:           "pending",
		CompletionWindow: window,
	}

	// custom_id → theme so results map back after the (unordered) batch returns.
	requests := make([]generation.BatchRequest, 0, req.Count)
	for i := 0; i < req.Count; i++ {
		theme := themeForIndex(req.Theme, i)
		requests = append(requests, generation.BatchRequest{
			CustomID:    fmt.Sprintf("%s-%d", job.ID.String(), i),
			Theme:       theme,
			Model:       req.Model,
			MaxMessages: maxMessages,
		})
	}

	batchID, inputFileID, err := s.gen.SubmitBatch(ctx, requests, window)
	if err != nil {
		// Persist a failed job row for visibility rather than dropping silently.
		now := time.Now()
		job.Status = "failed"
		job.Error = err.Error()
		job.StartedAt = &now
		job.FinishedAt = &now
		if cerr := s.repo.CreateGenerationJob(ctx, job); cerr != nil {
			return uuid.Nil, cerr
		}
		return uuid.Nil, err
	}

	now := time.Now()
	job.Status = "running"
	job.BatchStatus = "submitted"
	job.BatchID = batchID
	job.BatchInputFileID = inputFileID
	job.StartedAt = &now
	if err := s.repo.CreateGenerationJob(ctx, job); err != nil {
		return uuid.Nil, err
	}

	log.Info().
		Str("job_id", job.ID.String()).
		Str("batch_id", batchID).
		Int("requested", req.Count).
		Str("completion_window", window).
		Msg("warmup batch generation submitted")

	return job.ID, nil
}

// PollBatches reconciles every in-flight batch job against OpenAI. Completed
// batches are downloaded and ingested (clean + lint + cache, mirroring runJob);
// failed/expired/cancelled batches mark the job failed; otherwise the latest
// batch status is persisted so the admin UI reflects progress.
func (s *service) PollBatches(ctx context.Context) error {
	if s.gen == nil {
		return nil
	}
	jobs, err := s.repo.ListActiveBatchJobs(ctx)
	if err != nil {
		return err
	}
	for i := range jobs {
		job := &jobs[i]
		if err := s.pollBatchJob(ctx, job); err != nil {
			sentry.CaptureException(err)
			log.Warn().Err(err).Str("job_id", job.ID.String()).Str("batch_id", job.BatchID).
				Msg("warmup batch generation: poll failed")
		}
	}
	return nil
}

// pollBatchJob reconciles a single batch job.
func (s *service) pollBatchJob(ctx context.Context, job *models.WarmupGenerationJob) error {
	status, outputFileID, counts, err := s.gen.GetBatch(ctx, job.BatchID)
	if err != nil {
		return err
	}
	job.BatchStatus = status

	switch status {
	case "completed":
		job.BatchOutputFileID = outputFileID
		return s.ingestBatch(ctx, job, outputFileID, counts)
	case "failed", "expired", "cancelled":
		now := time.Now()
		job.Status = "failed"
		job.FinishedAt = &now
		if job.Error == "" {
			job.Error = fmt.Sprintf("batch %s", status)
		}
		return s.repo.UpdateGenerationJob(ctx, job)
	default:
		// validating | in_progress | finalizing | cancelling | submitted —
		// still running; persist the latest status for visibility.
		return s.repo.UpdateGenerationJob(ctx, job)
	}
}

// ingestBatch downloads a completed batch's output, cleans + lints + caches each
// conversation (mirroring runJob), and finalises the job row.
func (s *service) ingestBatch(ctx context.Context, job *models.WarmupGenerationJob, outputFileID string, counts generation.BatchCounts) error {
	results, err := s.gen.FetchBatchResults(ctx, outputFileID)
	if err != nil {
		now := time.Now()
		job.Status = "failed"
		job.FinishedAt = &now
		job.Error = err.Error()
		_ = s.repo.UpdateGenerationJob(ctx, job)
		return err
	}

	// Reset the per-ingest counters; the output file is the source of truth.
	job.GeneratedCount = 0
	job.LintRejectedCount = 0
	job.FailedCount = 0

	for i := range results {
		r := &results[i]
		if r.Err != "" || r.Conversation == nil {
			job.FailedCount++
			if r.Err != "" {
				log.Debug().Str("job_id", job.ID.String()).Str("custom_id", r.CustomID).Str("err", r.Err).
					Msg("warmup batch generation: result line failed")
			}
			continue
		}

		theme := themeForCustomID(r.CustomID, job.Theme)

		subject := strings.TrimSpace(r.Conversation.Subject)
		description := strings.TrimSpace(r.Conversation.Description)
		messages := cleanMessages(r.Conversation.Messages)
		if description == "" || subject == "" {
			job.FailedCount++
			continue
		}

		// Humanize before the gates (identical to the sync runJob path).
		subject, description, messages = humanizeThread(subject, description, messages)

		lintBody := description
		if len(messages) > 0 {
			lintBody += "\n\n" + strings.Join(messages, "\n")
		}
		if humanlint.LooksRobotic(lintBody) {
			job.LintRejectedCount++
			continue
		}
		if err := warmlint.Check(subject, lintBody, false); err != nil {
			job.LintRejectedCount++
			continue
		}

		record := &models.WarmupConversation{
			ID:             uuid.New(),
			PoolType:       job.PoolType,
			Segment:        job.Segment,
			Source:         models.WarmupContentSourceAI,
			Theme:          theme,
			Subject:        subject,
			Description:    description,
			Messages:       messages,
			Status:         "active",
			LintPassed:     true,
			GeneratedByJob: &job.ID,
		}
		if err := s.repo.InsertConversation(ctx, record); err != nil {
			job.FailedCount++
			sentry.CaptureException(err)
			continue
		}
		job.GeneratedCount++
	}

	// The output file already contains one line per request (success or error),
	// so iterating results above accounts for every line — including provider
	// failures, which surface as error lines. counts.Failed is therefore not
	// added on top (that would double-count); it's reconciled only if the output
	// reported fewer lines than the batch's total, which shouldn't normally
	// happen but guards against a truncated/partial download.
	if missing := counts.Total - len(results); missing > 0 {
		job.FailedCount += missing
	}

	now := time.Now()
	job.FinishedAt = &now
	job.Status = "completed"
	if job.GeneratedCount == 0 && (job.FailedCount > 0 || job.Error != "") {
		job.Status = "failed"
		if job.Error == "" {
			job.Error = fmt.Sprintf("all %d batch results failed", job.FailedCount)
		}
	}
	if err := s.repo.UpdateGenerationJob(ctx, job); err != nil {
		return err
	}
	log.Info().
		Str("job_id", job.ID.String()).
		Str("batch_id", job.BatchID).
		Int("generated", job.GeneratedCount).
		Int("lint_rejected", job.LintRejectedCount).
		Int("failed", job.FailedCount).
		Msg("warmup batch generation ingested")
	return nil
}

// themeForCustomID recovers the theme for a result. The custom_id is
// "<jobID>-<index>"; the index re-derives the rotated theme so cached rows carry
// the right topic even though the batch output is unordered.
func themeForCustomID(customID, pinnedTheme string) string {
	if pinnedTheme != "" {
		return pinnedTheme
	}
	idx := strings.LastIndexByte(customID, '-')
	if idx < 0 || idx+1 >= len(customID) {
		return defaultThemes[0]
	}
	n := 0
	for _, ch := range customID[idx+1:] {
		if ch < '0' || ch > '9' {
			return defaultThemes[0]
		}
		n = n*10 + int(ch-'0')
	}
	return defaultThemes[n%len(defaultThemes)]
}

// CancelBatch cancels an in-flight batch job both on OpenAI and locally.
func (s *service) CancelBatch(ctx context.Context, jobID uuid.UUID) error {
	if s.gen == nil {
		return ErrNotConfigured
	}
	job, err := s.repo.GetGenerationJob(ctx, jobID)
	if err != nil {
		return err
	}
	if job == nil {
		return fmt.Errorf("generation job not found")
	}
	if job.Mode != models.WarmupGenerationModeBatch || job.BatchID == "" {
		return fmt.Errorf("job is not a batch job")
	}
	if job.Status == "completed" || job.Status == "failed" {
		return fmt.Errorf("job already finished")
	}

	if err := s.gen.CancelBatch(ctx, job.BatchID); err != nil {
		return err
	}

	now := time.Now()
	job.Status = "failed"
	job.BatchStatus = "cancelling"
	job.FinishedAt = &now
	if job.Error == "" {
		job.Error = "cancelled by admin"
	}
	return s.repo.UpdateGenerationJob(ctx, job)
}
