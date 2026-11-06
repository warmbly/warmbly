package tasks

import (
	"context"
	"errors"
	"math/rand"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog/log"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/pkg/generation"
	"github.com/warmbly/warmbly/internal/repository"
)

// warmupContent is the rendered content for one warmup send plus the cohort
// metadata recorded on the verification token for the A/B harness.
type warmupContent struct {
	subject          string
	body             string
	theme            string
	contentSource    string
	conversationID   *uuid.UUID
	scenarioVersion  string
	renderingVersion string
	maxTurns         int
	// Campaign copy only: the HTML part and List-Unsubscribe target the
	// campaign would send, and the step and arm it came from.
	bodyHTML       string
	unsubscribeURL string
	tracking       *models.TrackingInfo
	sequenceID     *uuid.UUID
	variantID      *uuid.UUID
}

// getWarmupSettings caches selection settings briefly between warmup sends.
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

var errWarmupContentUnavailable = errors.New("warmup content is unavailable")

// pickNewWarmupContent prefers a conversation neither mailbox has seen recently and always yields one.
// A mature sender opens a share of its threads with its own running campaign copy instead.
func (s *tasksService) pickNewWarmupContent(ctx context.Context, account Email, recipient *Email, taskID uuid.UUID) (warmupContent, error) {
	library := warmupConversations()
	if s.warmupContentRepo != nil {
		if ownCopyDraw(account, recipient, time.Now(), rand.Intn(100)) {
			content, ok, err := s.campaignCopyContent(ctx, account, recipient, taskID)
			if err != nil {
				return warmupContent{}, err
			}
			if ok {
				return content, nil
			}
		}
		settings := s.getWarmupSettings(ctx)
		s.seedStaticWarmupLibrary(ctx, library)
		staticIDs := make([]uuid.UUID, len(library))
		for i, c := range library {
			staticIDs[i] = c.ID
		}
		conv, err := s.warmupContentRepo.PickConversation(ctx, repository.WarmupContentPick{Segment: strings.TrimSpace(account.WarmupTag), TaskID: taskID, SenderID: account.ID, RecipientID: recipient.ID, IncludeAI: settings.Enabled, StaticIDs: staticIDs})
		if err != nil {
			return warmupContent{}, err
		}
		if conv != nil {
			if content, ok := pickedWarmupContent(conv, account); ok {
				return content, nil
			}
		}
	}
	if len(library) == 0 {
		return warmupContent{}, errWarmupContentUnavailable
	}
	return staticWarmupContent(library[rand.Intn(len(library))], account), nil
}

// ownCopyDraw decides whether a new thread opens with the sender's campaign
// copy. A mailbox in its first WarmupOwnCopyMinDays of warmup never carries
// it, so sales copy never rides a mailbox with no history. Nor does a
// recipient that leaves warmup mail in its inbox, where a person reads it and
// someone else's sales copy is unsolicited mail.
func ownCopyDraw(account Email, recipient *Email, now time.Time, roll int) bool {
	return account.OrganizationID != nil && ownCopyMature(account, now) && ownCopyRecipient(recipient) && roll < models.WarmupOwnCopySharePercent
}

// ownCopyRecipient reports whether the recipient is a business inbox on Google
// Workspace or Microsoft 365, where cold copy is read, and files warmup mail
// out of its inbox.
func ownCopyRecipient(recipient *Email) bool {
	if !recipient.BusinessMailbox() {
		return false
	}
	placement, _ := recipient.WarmupFiling()
	return placement != models.WarmupPlacementInbox
}

// ownCopyMature reports whether the mailbox has warmed long enough to carry its own campaign copy.
func ownCopyMature(account Email, now time.Time) bool {
	return account.Warmup != nil && !account.Warmup.After(now.AddDate(0, 0, -models.WarmupOwnCopyMinDays))
}

func pickedWarmupContent(conv *models.WarmupConversation, account Email) (warmupContent, bool) {
	if conv.Source == models.WarmupContentSourceStatic {
		c, ok := resolveStaticConversation(conv.ID)
		return staticWarmupContent(c, account), ok
	}
	if conv.ScenarioVersion == nil || conv.RenderingVersion == nil || conv.Status != models.WarmupConversationActive || !conv.ReplyEligible || !conv.LintPassed || conv.SemanticReview != "passed" {
		return warmupContent{}, false
	}
	if generation.ValidateFreshWarmup(generation.Conversation{Subject: conv.Subject, Description: conv.Description, Messages: conv.Messages}) != nil {
		return warmupContent{}, false
	}
	c := Conversation{ID: conv.ID, Subject: conv.Subject, Theme: conv.Theme, Description: conv.Description, Messages: conv.Messages}
	body := GenerateConversationOpeningEmail(c, account)
	subject := strings.TrimSpace(conv.Subject)
	if body == "" || subject == "" || strings.ContainsAny(subject, "<>{}\x00\r\n") {
		return warmupContent{}, false
	}
	id := conv.ID
	return warmupContent{
		subject:          subject,
		body:             body,
		theme:            conv.Theme,
		contentSource:    models.WarmupContentSourceAI,
		conversationID:   &id,
		scenarioVersion:  *conv.ScenarioVersion,
		renderingVersion: *conv.RenderingVersion,
		maxTurns:         len(conv.Messages) + 1,
	}, true
}

func staticWarmupContent(c Conversation, account Email) warmupContent {
	id := c.ID
	return warmupContent{
		subject:          c.Subject,
		body:             GenerateConversationOpeningEmail(c, account),
		theme:            c.Theme,
		contentSource:    models.WarmupContentSourceStatic,
		conversationID:   &id,
		scenarioVersion:  c.Version,
		renderingVersion: generation.CanonicalRenderingVersion,
		maxTurns:         len(c.Messages) + 1,
	}
}

// seedStaticWarmupLibrary mirrors the in-code library into the bank once per process so it shares rotation with AI threads.
func (s *tasksService) seedStaticWarmupLibrary(ctx context.Context, library []Conversation) {
	s.staticSeed.Lock()
	defer s.staticSeed.Unlock()
	if s.staticSeed.done {
		return
	}
	rows := make([]models.WarmupConversation, len(library))
	scenario, rendering := generation.WarmupScenarioVersion, generation.CanonicalRenderingVersion
	for i, c := range library {
		rows[i] = models.WarmupConversation{ID: c.ID, PoolType: "premium", Source: models.WarmupContentSourceStatic, Theme: c.Theme, Subject: c.Subject, Description: c.Description, Messages: c.Messages, Status: models.WarmupConversationActive, LintPassed: true, ReplyEligible: true, ScenarioVersion: &scenario, RenderingVersion: &rendering, SemanticReview: "passed"}
	}
	if err := s.warmupContentRepo.SeedStaticConversations(ctx, rows); err != nil {
		log.Warn().Err(err).Msg("warmup static library not mirrored into the content bank; retrying on the next send")
		return
	}
	s.staticSeed.done = true
}
