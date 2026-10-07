package warmupcontent

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog/log"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/observability/errs"

	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/pkg/generation"
	"github.com/warmbly/warmbly/internal/pkg/warmlint"
)

// themeForIndex uses a pinned theme when present, otherwise it rotates through
// the default theme set so a batch spans varied topics.
func themeForIndex(pinned string, i int) string {
	if pinned != "" {
		return pinned
	}
	return defaultThemes[i%len(defaultThemes)]
}

// GenerateBatch submits an async OpenAI Batch API run. It builds N
// chat-completion requests with deterministic theme rotation,
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
		req.Trigger = "schedule"
	}
	if req.Count <= 0 {
		req.Count = 100
	}
	if req.Count > maxPerBatch {
		req.Count = maxPerBatch
	}

	settings, err := s.repo.GetGenerationSettings(ctx)
	if err != nil {
		return uuid.Nil, err
	}
	if settings == nil || !settings.GenerationEnabled {
		return uuid.Nil, ErrGenerationStopped
	}
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
	maxMessages = max(1, min(5, maxMessages))

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
	if window != "24h" {
		return uuid.Nil, fmt.Errorf("batch completion window must be 24h")
	}
	if err := s.gen.CheckModel(ctx, req.Model); err != nil {
		return uuid.Nil, err
	}

	version := generation.DiagnosticScenarioVersion
	job := &models.WarmupGenerationJob{
		ContentVersion:       &version,
		MaxMessagesPerThread: &maxMessages,
		ID:                   uuid.New(),
		RequestedBy:          req.RequestedBy,
		Trigger:              req.Trigger,
		Mode:                 models.WarmupGenerationModeBatch,
		PoolType:             req.PoolType,
		Segment:              req.Segment,
		Theme:                req.Theme,
		Model:                req.Model,
		RequestedCount:       req.Count,
		Status:               "pending",
		CompletionWindow:     window,
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

	// Reserve the scheduled job before making the provider call. A partial
	// unique index permits only one in-flight scheduled job per library segment,
	// so multiple backend replicas cannot submit duplicate batches.
	if err := s.repo.CreateGenerationJob(ctx, job); err != nil {
		return uuid.Nil, err
	}

	batchID, inputFileID, err := s.gen.SubmitBatch(ctx, requests, window)
	if err != nil {
		now := time.Now()
		job.Status = "failed"
		job.Error = "provider batch submission failed"
		job.StartedAt = &now
		job.FinishedAt = &now
		if updateErr := s.repo.UpdateGenerationJob(ctx, job); updateErr != nil {
			return uuid.Nil, updateErr
		}
		return uuid.Nil, fmt.Errorf("provider batch submission failed")
	}

	now := time.Now()
	job.Status = "running"
	job.BatchStatus = "submitted"
	job.BatchID = batchID
	job.BatchInputFileID = inputFileID
	job.StartedAt = &now
	if err := s.repo.UpdateGenerationJob(ctx, job); err != nil {
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

// PollBatches reconciles every in-flight batch job against OpenAI. A batch
// that stopped running is ingested from whatever file it left (clean, lint,
// and cache), with an expired or cancelled reason recorded; one that left
// nothing marks the job failed. Otherwise the latest batch status is
// persisted so the admin UI reflects progress.
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
			errs.CaptureException(err)
			log.Warn().Err(err).Str("job_id", job.ID.String()).Str("batch_id", job.BatchID).
				Msg("warmup batch generation: poll failed")
		}
	}
	return nil
}

// pollBatchJob reconciles a single batch job.
func (s *service) pollBatchJob(ctx context.Context, job *models.WarmupGenerationJob) error {
	state, err := s.gen.GetBatch(ctx, job.BatchID)
	if err != nil {
		return fmt.Errorf("provider batch status unavailable")
	}
	job.BatchStatus = state.Status

	if !batchEnded(state.Status) {
		// validating | in_progress | finalizing | cancelling | submitted —
		// still running; persist the latest status for visibility.
		return s.repo.UpdateGenerationJob(ctx, job)
	}

	outcome := endedBatchOutcome(state)
	if job.Error == "" {
		job.Error = outcome.Error
	}
	if outcome.ResultsFileID != "" {
		job.BatchOutputFileID = state.OutputFileID
		return s.ingestBatch(ctx, job, outcome.ResultsFileID, state.Counts)
	}

	now := time.Now()
	job.Status = "failed"
	job.FinishedAt = &now
	return s.repo.UpdateGenerationJob(ctx, job)
}

// batchEnded reports whether a batch status is terminal.
func batchEnded(status string) bool {
	switch status {
	case "completed", "failed", "expired", "cancelled":
		return true
	}
	return false
}

// batchOutcome is what the poller does with a batch that stopped running.
type batchOutcome struct {
	// ResultsFileID is the JSONL to ingest, empty when the batch left none.
	ResultsFileID string
	// Error is the reason to record, empty when the batch ended cleanly.
	Error string
}

// endedBatchOutcome decides what a terminal batch leaves behind. A window that
// closes early still hands back the requests it did finish, so an expired or
// cancelled batch is ingested rather than discarded, with the reason recorded
// so the shortfall is not read as a clean run.
func endedBatchOutcome(state generation.BatchState) batchOutcome {
	// Output holds the successes, the error file the refusals; same JSONL shape.
	results := state.OutputFileID
	if results == "" {
		results = state.ErrorFileID
	}
	if state.Status == "completed" {
		if results == "" {
			return batchOutcome{Error: "batch completed with no output file"}
		}
		return batchOutcome{ResultsFileID: results}
	}
	reason := fmt.Sprintf("batch %s", state.Status)
	return batchOutcome{ResultsFileID: results, Error: reason}
}

// ingestBatch downloads a completed batch's output, cleans, lints, and caches
// each conversation, then finalises the job row.
func (s *service) ingestBatch(ctx context.Context, job *models.WarmupGenerationJob, outputFileID string, counts generation.BatchCounts) error {
	results, err := s.gen.FetchBatchResults(ctx, outputFileID)
	if err != nil {
		return fmt.Errorf("provider batch results unavailable")
	}

	// Reset the per-ingest counters; the output file is the source of truth.
	job.GeneratedCount = 0
	job.LintRejectedCount = 0
	job.FailedCount = 0

	seen := make(map[string]bool, len(results))
	for i := range results {
		r := &results[i]
		index, indexErr := strconv.Atoi(strings.TrimPrefix(r.CustomID, job.ID.String()+"-"))
		if !strings.HasPrefix(r.CustomID, job.ID.String()+"-") || indexErr != nil || index < 0 || index >= job.RequestedCount || r.CustomID != fmt.Sprintf("%s-%d", job.ID, index) || seen[r.CustomID] {
			continue
		}
		seen[r.CustomID] = true
		if r.Err != "" || r.Conversation == nil {
			job.FailedCount++
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
		recordID := batchConversationID(job.ID, r.CustomID)
		prior, err := s.repo.GetConversation(ctx, recordID)
		if err != nil {
			return err
		}
		if prior != nil {
			if prior.SemanticReview == "passed" || prior.SemanticReview == "legacy_unreviewed" {
				job.GeneratedCount++
			}
			if prior.SemanticReview == "rejected" {
				job.LintRejectedCount++
			}
			if prior.SemanticReview == "unavailable" {
				job.FailedCount++
			}
			continue
		}
		if job.ContentVersion == nil {
			record := &models.WarmupConversation{ID: recordID, PoolType: job.PoolType, Segment: job.Segment, Source: models.WarmupContentSourceAI, Theme: theme, Subject: subject, Description: description, Messages: messages, Status: models.WarmupConversationArchived, SemanticReview: "legacy_unreviewed", GeneratedByJob: &job.ID}
			if err := s.repo.InsertConversation(ctx, record); err != nil {
				job.FailedCount++
				return err
			}
			job.GeneratedCount++
			continue
		}
		if *job.ContentVersion != generation.DiagnosticScenarioVersion {
			job.FailedCount++
			continue
		}
		if job.MaxMessagesPerThread != nil && len(messages) != *job.MaxMessagesPerThread {
			job.LintRejectedCount++
			continue
		}

		// Preserve canonical facts before reviewing the complete rendered thread.
		subject, description, messages = preserveCanonicalThread(subject, description, messages)
		rendered, err := generation.RenderDiagnostic(generation.Conversation{Subject: subject, Description: description, Messages: messages}, [2]string{"Mailbox A (diagnostic)", "Mailbox B (diagnostic)"})
		if err != nil {
			job.LintRejectedCount++
			continue
		}
		lintBody := rendered.Description + "\n\n" + strings.Join(rendered.Messages, "\n")
		if err := warmlint.Check(subject, lintBody, false); err != nil {
			job.LintRejectedCount++
			continue
		}
		review, status := "unavailable", models.WarmupConversationArchived
		if s.judge != nil {
			if judgment, err := judgeThread(ctx, s.judge, rendered.Subject, rendered.Description, rendered.Messages); err != nil {
				review = "unavailable"
				job.FailedCount++
			} else {
				if reject, _ := judgment.Reject(); reject {
					review = "rejected"
					job.LintRejectedCount++
				} else {
					review, status = "passed", models.WarmupConversationActive
				}
			}
		} else {
			job.FailedCount++
		}
		scenarioVersion, renderingVersion := generation.DiagnosticScenarioVersion, generation.CanonicalRenderingVersion

		record := &models.WarmupConversation{
			ID:               recordID,
			PoolType:         job.PoolType,
			Segment:          job.Segment,
			Source:           models.WarmupContentSourceAI,
			Theme:            theme,
			Subject:          subject,
			Description:      description,
			Messages:         messages,
			Status:           status,
			LintPassed:       true,
			ReplyEligible:    status == models.WarmupConversationActive,
			GeneratedByJob:   &job.ID,
			ScenarioVersion:  &scenarioVersion,
			RenderingVersion: &renderingVersion,
			SemanticReview:   review,
		}
		if err := s.repo.InsertConversation(ctx, record); err != nil {
			job.FailedCount++
			return err
		}
		if status == models.WarmupConversationActive {
			job.GeneratedCount++
		}
	}

	// The output file already contains one line per request (success or error),
	// so iterating results above accounts for every line — including provider
	// failures, which surface as error lines. counts.Failed is therefore not
	// added on top (that would double-count); it's reconciled only if the output
	// reported fewer lines than the batch's total, which shouldn't normally
	// happen but guards against a truncated/partial download.
	if missing := max(counts.Total, job.RequestedCount) - len(seen); missing > 0 {
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

// CancelBatch asks OpenAI to cancel an in-flight batch. The job stays running
// so the poller ingests what the batch finished before the cancel landed; the
// admin's reason is recorded now and survives that ingest.
func (s *service) CancelBatch(ctx context.Context, jobID uuid.UUID) error {
	if s.gen == nil {
		return ErrNotConfigured
	}
	job, err := s.repo.GetGenerationJob(ctx, jobID)
	if err != nil {
		return err
	}
	if job == nil {
		return errx.New(errx.NotFound, "generation job not found")
	}
	if job.Mode != models.WarmupGenerationModeBatch || job.BatchID == "" {
		return errx.New(errx.BadRequest, "job is not a batch job")
	}
	if job.Status == "completed" || job.Status == "failed" {
		return errx.New(errx.BadRequest, "job already finished")
	}
	if job.BatchStatus == "cancelling" {
		return errx.New(errx.BadRequest, "cancellation already requested")
	}

	if err := s.gen.CancelBatch(ctx, job.BatchID); err != nil {
		return fmt.Errorf("provider batch cancellation failed")
	}

	// A conditional write: if the poller finished the job between the read
	// above and now, its terminal state and counts stand and there is nothing
	// left to mark.
	_, err = s.repo.MarkBatchCancelling(ctx, jobID, "cancelled by admin")
	return err
}

func batchConversationID(jobID uuid.UUID, customID string) uuid.UUID {
	return uuid.NewSHA1(jobID, []byte(customID))
}

// firstResultError returns the first non-empty per-line error in a batch's
// results, used to name the reason a fully failed batch produced nothing.
func firstResultError(results []generation.BatchResult) string {
	for i := range results {
		if results[i].Err != "" {
			return results[i].Err
		}
	}
	return ""
}
