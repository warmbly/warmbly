package tasks

import (
	"context"
	"math/rand"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/models"
)

// warmupContent is the rendered content for one warmup send plus the cohort
// metadata recorded on the verification token for the A/B harness.
type warmupContent struct {
	subject        string
	body           string
	theme          string
	contentSource  string
	conversationID *uuid.UUID
}

// getWarmupSettings returns the warmup generation settings with a short
// in-process TTL cache so the per-send AI-vs-static decision doesn't hit
// Postgres on every warmup send. Falls back to defaults when unconfigured.
func (s *tasksService) getWarmupSettings(ctx context.Context) models.WarmupGenerationSettings {
	if s.warmupContentRepo == nil {
		return models.DefaultWarmupGenerationSettings()
	}
	if c := s.warmupSettings; c != nil {
		c.mu.RLock()
		fresh := !c.fetched.IsZero() && time.Since(c.fetched) < 60*time.Second
		val := c.val
		c.mu.RUnlock()
		if fresh {
			return val
		}
	}
	set, err := s.warmupContentRepo.GetGenerationSettings(ctx)
	if err != nil || set == nil {
		return models.DefaultWarmupGenerationSettings()
	}
	if c := s.warmupSettings; c != nil {
		c.mu.Lock()
		c.val = *set
		c.fetched = time.Now()
		c.mu.Unlock()
	}
	return *set
}

// pickNewWarmupContent selects content for a NEW (non-reply) warmup send. When
// AI enrichment is enabled and the dice roll falls within AISelectionShare it
// draws a thread from the cached, segment-aware AI bank; otherwise it uses the
// static in-code library. The static library is always the safe fallback, so
// an empty bank or disabled generator never stops warmup.
func (s *tasksService) pickNewWarmupContent(ctx context.Context, account Email) warmupContent {
	settings := s.getWarmupSettings(ctx)

	if settings.Enabled && s.warmupContentRepo != nil && rand.Intn(100) < settings.AISelectionShare {
		// Content is drawn from the shared library by segment; tier doesn't
		// affect content (only mailbox reputation isolation uses the pool).
		segment := strings.TrimSpace(account.WarmupTag)
		conv, err := s.warmupContentRepo.PickConversation(ctx, segment)
		if err == nil && conv != nil {
			c := Conversation{ID: conv.ID, Theme: conv.Theme, Description: conv.Description, Messages: conv.Messages}
			body := GenerateConversationEmail(c, account, false)
			subject := spinClean(conv.Subject)
			if subject == "" {
				subject = generateWarmupSubject()
			}
			_ = s.warmupContentRepo.IncrementConversationUsage(ctx, conv.ID)
			id := conv.ID
			return warmupContent{
				subject:        subject,
				body:           body,
				theme:          conv.Theme,
				contentSource:  models.WarmupContentSourceAI,
				conversationID: &id,
			}
		}
	}

	conversation := randomWarmupConversation()
	staticID := conversation.ID
	return warmupContent{
		subject:        generateWarmupSubject(),
		body:           GenerateConversationEmail(conversation, account, false),
		theme:          conversation.Theme,
		contentSource:  models.WarmupContentSourceStatic,
		conversationID: &staticID,
	}
}
