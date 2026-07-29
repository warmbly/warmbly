// Package warmupcontent runs the offline warmup-content generator: it calls
// the AI client, lints each result, and caches accepted threads in the
// warmup_conversations bank for the live send path to draw from. Generation is
// always scheduled offline and never runs on the send hot path.
package warmupcontent

import (
	"context"
	"errors"
	"hash/fnv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog/log"

	"github.com/warmbly/warmbly/internal/pkg/generation"
	"github.com/warmbly/warmbly/internal/pkg/humanlint"
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

// maxPerBatch bounds a single Batch API submission. Batch is async and cheap so
// it tolerates a much larger fan-out than the sync path; OpenAI itself allows up
// to 50,000 requests per batch input file.
const maxPerBatch = 2000

const (
	minimumAdaptiveThreads = 200
	maximumAdaptiveThreads = 5000
	usesPerThreadPerDay    = 20
	scheduledBatchMax      = 250
)

// AdaptiveThreadTarget sizes the bank from recent demand while bounding model
// spend. The configured target is a floor, not an ongoing tuning obligation.
func AdaptiveThreadTarget(sevenDaySends, aiSelectionShare, configuredFloor int) int {
	if configuredFloor < minimumAdaptiveThreads {
		configuredFloor = minimumAdaptiveThreads
	}
	averageDaily := (sevenDaySends + 6) / 7
	aiDaily := (averageDaily*aiSelectionShare + 99) / 100
	target := (aiDaily + usesPerThreadPerDay - 1) / usesPerThreadPerDay
	if target < configuredFloor {
		target = configuredFloor
	}
	if target > maximumAdaptiveThreads {
		target = maximumAdaptiveThreads
	}
	return target
}

// GenerateRequest describes one generation run.
type GenerateRequest struct {
	RequestedBy *uuid.UUID
	Trigger     string // "schedule"
	PoolType    string
	Segment     string
	Theme       string
	Model       string
	Count       int
	// MaxMessages overrides the per-thread follow-up count for this run; 0 falls
	// back to the controller policy.
	MaxMessages int
	// CompletionWindow is the OpenAI Batch API processing window (batch path
	// only); empty defaults to "24h".
	CompletionWindow string
}

// Service drives offline warmup content generation.
type Service interface {
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

	if expired, err := s.repo.ExpireStaleScheduledJobs(ctx, time.Now().Add(-15*time.Minute)); err != nil {
		return err
	} else if expired > 0 {
		log.Warn().Int64("expired", expired).Msg("warmup generation: released stale batch reservations")
	}
	if retired, err := s.repo.RetireRiskyConversations(ctx, time.Now().AddDate(0, 0, -14), 20, 3, 0.15); err != nil {
		return err
	} else if retired > 0 {
		log.Warn().Int("retired", retired).Msg("warmup generation: retired threads with unsafe spam placement")
	}

	remaining := dailyRemaining(ctx, s.repo, settings.DailyGenerationCap)
	if settings.DailyGenerationCap > 0 && remaining <= 0 {
		log.Info().Msg("warmup generation: daily cap reached; scheduled run skipped")
		return nil
	}

	totalDemand, err := s.repo.WarmupSendsSince(ctx, time.Now().AddDate(0, 0, -7))
	if err != nil {
		return err
	}

	for _, pool := range settings.Pools {
		if !pool.Enabled {
			continue
		}
		segmentSet := map[string]struct{}{"": {}}
		for _, segment := range pool.Segments {
			segmentSet[segment] = struct{}{}
		}
		for segment := range segmentSet {
			active, _ := s.repo.CountActiveConversations(ctx, pool.PoolType, segment)
			target := AdaptiveThreadTarget(totalDemand, settings.AISelectionShare, pool.TargetActiveThreads)

			// Continuous refresh: once the library is at target, retire the
			// most-used AI threads so the top-up below mints fresh replacements.
			// Retire only what today's budget can regenerate, so the library
			// never shrinks without being refilled.
			if settings.RefreshEnabled && active >= target {
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

			deficit := target - active
			if deficit <= 0 {
				continue
			}
			count := deficit
			if count > scheduledBatchMax {
				count = scheduledBatchMax
			}
			if settings.DailyGenerationCap > 0 && count > remaining {
				count = remaining
			}
			if count <= 0 {
				continue
			}

			// Scheduled work is asynchronous and discounted. It never holds the
			// scheduler loop open on model latency, and the durable job is picked up
			// by the batch poller after restarts.
			_, err := s.GenerateBatch(ctx, GenerateRequest{
				Trigger:  "schedule",
				PoolType: pool.PoolType,
				Segment:  segment,
				Model:    settings.Model,
				Count:    count,
			})
			if err != nil {
				log.Debug().Err(err).Str("segment", segment).Msg("warmup generation: scheduled batch not submitted")
				continue
			}
			if settings.DailyGenerationCap > 0 {
				remaining -= count
				if remaining <= 0 {
					return nil
				}
			}
		}
	}
	return nil
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
// by the batch ingest path.
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
