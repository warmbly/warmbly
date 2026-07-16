// Package warmupcontent runs the offline warmup-content generator: it calls
// the AI client, lints each result, and caches accepted threads in the
// warmup_conversations bank for the live send path to draw from. Generation is
// always offline (admin-triggered or scheduled) and never on the send hot path.
package warmupcontent

import (
	"context"
	"errors"
	"fmt"
	"hash/fnv"
	"strings"
	"time"

	"github.com/getsentry/sentry-go"
	"github.com/google/uuid"
	"github.com/rs/zerolog/log"

	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/pkg/generation"
	"github.com/warmbly/warmbly/internal/pkg/humanlint"
	"github.com/warmbly/warmbly/internal/pkg/warmlint"
	"github.com/warmbly/warmbly/internal/repository"
)

// ErrNotConfigured is returned when generation is requested but no AI client is
// wired (the provider is not OpenAI or AI_API_KEY is unset).
var ErrNotConfigured = errors.New("warmup AI generation is not configured")

// defaultThemes seed topic variety when a generation run doesn't pin a theme.
var defaultThemes = []string{
	"productivity", "learning", "collaboration", "industry trends", "tools",
	"networking", "feedback", "planning", "reading", "wellness", "events",
	"career growth", "remote work", "meetings", "automation", "team culture",
	"customer success", "writing", "travel", "community",
}

// maxPerRun bounds a single sync generation run regardless of request size.
const maxPerRun = 200

// maxPerBatch bounds a single Batch API submission. Batch is async and cheap so
// it tolerates a much larger fan-out than the sync path; OpenAI itself allows up
// to 50,000 requests per batch input file.
const maxPerBatch = 2000

// GenerateRequest describes one generation run.
type GenerateRequest struct {
	RequestedBy *uuid.UUID
	Trigger     string // "manual" | "schedule"
	PoolType    string
	Segment     string
	Theme       string
	Model       string
	Count       int
	// MaxMessages overrides the per-thread follow-up count for this run; 0 falls
	// back to the admin generation settings. Used by the batch path so callers
	// have full control over the thread shape.
	MaxMessages int
	// CompletionWindow is the OpenAI Batch API processing window (batch path
	// only); empty defaults to "24h".
	CompletionWindow string
}

// Service drives offline warmup content generation.
type Service interface {
	// Generate starts an offline generation run in the background and returns
	// the job ID immediately so callers can track progress.
	Generate(ctx context.Context, req GenerateRequest) (uuid.UUID, error)
	// GenerateBatch submits an async OpenAI Batch API run (~50% cheaper) and
	// returns the job ID immediately. Results are ingested later by PollBatches.
	GenerateBatch(ctx context.Context, req GenerateRequest) (uuid.UUID, error)
	// PollBatches reconciles in-flight batch jobs against OpenAI: it ingests
	// completed batches and marks failed/expired/cancelled ones.
	PollBatches(ctx context.Context) error
	// CancelBatch cancels an in-flight batch job (OpenAI + local job row).
	CancelBatch(ctx context.Context, jobID uuid.UUID) error
	// RunScheduled tops every enabled pool/segment up toward its target.
	RunScheduled(ctx context.Context) error
	// Enabled reports whether an AI client is configured.
	Enabled() bool
}

type service struct {
	repo repository.WarmupContentRepository
	gen  *generation.GenerationClient
}

// NewService creates the generation service. gen may be nil (provider not OpenAI),
// in which case generation requests return ErrNotConfigured.
func NewService(repo repository.WarmupContentRepository, gen *generation.GenerationClient) Service {
	return &service{repo: repo, gen: gen}
}

func (s *service) Enabled() bool { return s.gen != nil }

func (s *service) Generate(ctx context.Context, req GenerateRequest) (uuid.UUID, error) {
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
		req.Count = 10
	}
	if req.Count > maxPerRun {
		req.Count = maxPerRun
	}

	settings, _ := s.repo.GetGenerationSettings(ctx)
	if req.Model == "" && settings != nil {
		req.Model = settings.Model
	}

	job := &models.WarmupGenerationJob{
		ID:             uuid.New(),
		RequestedBy:    req.RequestedBy,
		Trigger:        req.Trigger,
		PoolType:       req.PoolType,
		Segment:        req.Segment,
		Theme:          req.Theme,
		Model:          req.Model,
		RequestedCount: req.Count,
		Status:         "pending",
	}
	if err := s.repo.CreateGenerationJob(ctx, job); err != nil {
		return uuid.Nil, err
	}

	// Run in the background so the admin request returns immediately; the job
	// row is the progress/visibility surface.
	go func() {
		bg, cancel := context.WithTimeout(context.Background(), time.Duration(req.Count)*45*time.Second+time.Minute)
		defer cancel()
		s.runJob(bg, job)
	}()

	return job.ID, nil
}

func (s *service) RunScheduled(ctx context.Context) error {
	if s.gen == nil {
		return nil
	}
	settings, err := s.repo.GetGenerationSettings(ctx)
	if err != nil || settings == nil {
		return err
	}
	if !settings.ScheduleEnabled {
		return nil
	}

	remaining := dailyRemaining(ctx, s.repo, settings.DailyGenerationCap)
	if settings.DailyGenerationCap > 0 && remaining <= 0 {
		log.Info().Msg("warmup generation: daily cap reached; scheduled run skipped")
		return nil
	}

	for _, pool := range settings.Pools {
		if !pool.Enabled {
			continue
		}
		segments := pool.Segments
		if len(segments) == 0 {
			segments = []string{""}
		}
		for _, segment := range segments {
			active, _ := s.repo.CountActiveConversations(ctx, pool.PoolType, segment)

			// Continuous refresh: once the library is at target, retire the
			// most-used AI threads so the top-up below mints fresh replacements.
			// Retire only what today's budget can regenerate, so the library
			// never shrinks without being refilled.
			if settings.RefreshEnabled && pool.TargetActiveThreads > 0 && active >= pool.TargetActiveThreads {
				recycle := settings.RefreshPerRun
				if recycle > 25 {
					recycle = 25
				}
				if settings.DailyGenerationCap > 0 && recycle > remaining {
					recycle = remaining
				}
				if recycle > 0 {
					retired, err := s.repo.RetireMostUsedConversations(ctx, pool.PoolType, segment, recycle)
					if err != nil {
						log.Warn().Err(err).Str("segment", segment).Msg("warmup generation: refresh retirement failed")
					} else if retired > 0 {
						active -= retired
						log.Info().Int("retired", retired).Str("segment", segment).Msg("warmup generation: recycled most-used threads for refresh")
					}
				}
			}

			deficit := pool.TargetActiveThreads - active
			if deficit <= 0 {
				continue
			}
			count := deficit
			if count > 25 {
				count = 25 // per-segment per-run cap so one run doesn't monopolise
			}
			if settings.DailyGenerationCap > 0 && count > remaining {
				count = remaining
			}
			if count <= 0 {
				continue
			}

			job := &models.WarmupGenerationJob{
				ID:             uuid.New(),
				Trigger:        "schedule",
				PoolType:       pool.PoolType,
				Segment:        segment,
				Model:          settings.Model,
				RequestedCount: count,
				Status:         "pending",
			}
			if err := s.repo.CreateGenerationJob(ctx, job); err != nil {
				log.Warn().Err(err).Msg("warmup generation: failed to create scheduled job")
				continue
			}
			generated := s.runJob(ctx, job)
			if settings.DailyGenerationCap > 0 {
				remaining -= generated
				if remaining <= 0 {
					return nil
				}
			}
		}
	}
	return nil
}

// runJob executes a generation job, updating its row as it goes. Returns the
// number of conversations successfully cached.
func (s *service) runJob(ctx context.Context, job *models.WarmupGenerationJob) int {
	now := time.Now()
	job.StartedAt = &now
	job.Status = "running"
	if err := s.repo.UpdateGenerationJob(ctx, job); err != nil {
		log.Warn().Err(err).Str("job_id", job.ID.String()).Msg("warmup generation: failed to mark job running")
	}

	settings, _ := s.repo.GetGenerationSettings(ctx)
	maxMessages := 4
	if settings != nil {
		maxMessages = settings.MaxMessagesPerThread
	}

	for i := 0; i < job.RequestedCount; i++ {
		if ctx.Err() != nil {
			job.Error = ctx.Err().Error()
			break
		}
		theme := job.Theme
		if theme == "" {
			theme = defaultThemes[i%len(defaultThemes)]
		}

		conv, err := s.gen.GenerateConversation(ctx, theme, job.Model, maxMessages)
		if err != nil {
			job.FailedCount++
			log.Warn().Err(err).Str("job_id", job.ID.String()).Str("theme", theme).Msg("warmup generation: model call failed")
			continue
		}

		subject := strings.TrimSpace(conv.Subject)
		description := strings.TrimSpace(conv.Description)
		messages := cleanMessages(conv.Messages)
		if description == "" || subject == "" {
			job.FailedCount++
			continue
		}

		// Humanize BEFORE the gates: strip AI tells (em-dashes, stock openers,
		// AI-accent vocabulary, "not only X but also Y") and apply casual
		// contractions, so the cached copy reads like a person wrote it.
		subject, description, messages = humanizeThread(subject, description, messages)

		lintBody := description
		if len(messages) > 0 {
			lintBody += "\n\n" + strings.Join(messages, "\n")
		}
		// Reject threads that still read machine-generated after cleanup.
		if humanlint.LooksRobotic(lintBody) {
			job.LintRejectedCount++
			continue
		}
		if err := warmlint.Check(subject, lintBody, false); err != nil {
			job.LintRejectedCount++
			log.Debug().Err(err).Str("job_id", job.ID.String()).Msg("warmup generation: lint rejected a thread")
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

	finished := time.Now()
	job.FinishedAt = &finished
	job.Status = "completed"
	if job.GeneratedCount == 0 && (job.FailedCount > 0 || job.Error != "") {
		job.Status = "failed"
		if job.Error == "" {
			job.Error = fmt.Sprintf("all %d generations failed", job.FailedCount)
		}
	}
	if err := s.repo.UpdateGenerationJob(ctx, job); err != nil {
		log.Warn().Err(err).Str("job_id", job.ID.String()).Msg("warmup generation: failed to finalise job")
	}
	log.Info().
		Str("job_id", job.ID.String()).
		Int("generated", job.GeneratedCount).
		Int("lint_rejected", job.LintRejectedCount).
		Int("failed", job.FailedCount).
		Msg("warmup generation run finished")
	return job.GeneratedCount
}

func cleanMessages(in []string) []string {
	out := make([]string, 0, len(in))
	for _, m := range in {
		m = strings.TrimSpace(m)
		if m != "" {
			out = append(out, m)
		}
	}
	return out
}

// humanizeThread runs the deterministic humanizer over a generated thread's
// subject, opening line, and follow-ups, seeded by content so the same thread
// humanizes identically (reproducible) while differing across threads. Shared
// by the sync (runJob) and batch (ingestBatch) ingest paths.
func humanizeThread(subject, description string, messages []string) (string, string, []string) {
	h := fnv.New64a()
	_, _ = h.Write([]byte(subject + "|" + description))
	seed := int64(h.Sum64())

	subject = humanlint.HumanizeSubject(subject, seed)
	description = humanlint.Humanize(description, seed+1)
	for i := range messages {
		messages[i] = humanlint.Humanize(messages[i], seed+int64(i)+2)
	}
	return subject, description, messages
}

func dailyRemaining(ctx context.Context, repo repository.WarmupContentRepository, dailyCap int) int {
	if dailyCap <= 0 {
		return 1 << 30 // effectively unlimited
	}
	since := time.Now().Truncate(24 * time.Hour)
	used, err := repo.GeneratedCountSince(ctx, since)
	if err != nil {
		return dailyCap
	}
	return dailyCap - used
}
