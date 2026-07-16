// Package inboxagent implements the inbox agent (M10): a paid, opt-in feature
// that drafts a suggested reply when an inbound HUMAN reply lands, persists it
// awaiting a human Approve-and-send / Edit / Discard in the unibox, and never
// sends on its own. It runs in the consumer (where inbound replies are ingested)
// off the advanced-outreach reply hook, on a detached context so it never blocks
// reply processing.
package inboxagent

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog/log"

	"github.com/warmbly/warmbly/internal/app/credits"
	"github.com/warmbly/warmbly/internal/app/feature"
	"github.com/warmbly/warmbly/internal/app/replyclassify"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/pkg/generation"
	"github.com/warmbly/warmbly/internal/repository"
)

// draftTimeout bounds the whole draft (thread fetch + one completion) on the
// detached background context.
const draftTimeout = 45 * time.Second

// maxThreadMessages caps how much thread history grounds the draft.
const maxThreadMessages = 20

// OrgReader reads the org for its opt-in flag + voice profile.
type OrgReader interface {
	GetByID(ctx context.Context, id uuid.UUID) (*models.Organization, error)
}

// ThreadReader loads a thread's messages for grounding (repository.UniboxRepository).
type ThreadReader interface {
	GetByThread(ctx context.Context, orgID, emailID uuid.UUID, threadID string, limit int, cursor string) (*models.MailSearchResult, error)
}

// SkillsSource optionally contributes the org's enabled skills preamble.
type SkillsSource interface {
	EnabledPreamble(ctx context.Context, orgID uuid.UUID) string
}

// ContactReader optionally grounds the draft in the counterpart contact's CRM
// record (name, company, custom fields). Satisfied by repository.ContactRepository.
type ContactReader interface {
	GetByID(ctx context.Context, contactID uuid.UUID) (*models.Contact, *errx.Error)
}

// DraftPublisher emits the AI_DRAFT_READY realtime event (*pubsub.StreamingPublisher).
type DraftPublisher interface {
	PublishAIDraftReady(ctx context.Context, orgID, actorID uuid.UUID, threadID, draftID, emailID string)
}

// Service is the inbox agent. DraftForReply is the single entry point, called
// best-effort from the reply hook; it fans the actual work onto its own
// goroutine + context, so a slow model never stalls inbound-reply processing.
type Service interface {
	DraftForReply(ctx context.Context, r models.InboxAgentReply)
}

type service struct {
	provider  generation.Provider
	credits   credits.CreditService
	feature   feature.FeatureGateService
	orgs      OrgReader
	threads   ThreadReader
	skills    SkillsSource
	contacts  ContactReader
	draftRepo repository.AIDraftRepository
	publisher DraftPublisher
}

// NewService builds the inbox agent. A nil provider, credit service, org reader,
// thread reader, or draft repo disables it (DraftForReply becomes a no-op);
// skills + publisher are optional.
func NewService(
	provider generation.Provider,
	creditSvc credits.CreditService,
	featureGate feature.FeatureGateService,
	orgs OrgReader,
	threads ThreadReader,
	skills SkillsSource,
	contacts ContactReader,
	draftRepo repository.AIDraftRepository,
	publisher DraftPublisher,
) Service {
	return &service{
		provider:  provider,
		credits:   creditSvc,
		feature:   featureGate,
		orgs:      orgs,
		threads:   threads,
		skills:    skills,
		contacts:  contacts,
		draftRepo: draftRepo,
		publisher: publisher,
	}
}

// DraftForReply kicks off drafting on a detached context so the caller (inbound
// reply processing) never blocks on the model. All work is best-effort.
func (s *service) DraftForReply(_ context.Context, r models.InboxAgentReply) {
	if s.provider == nil || s.credits == nil || s.orgs == nil || s.threads == nil || s.draftRepo == nil {
		return
	}
	if r.OrganizationID == uuid.Nil || r.ThreadID == "" || r.Counterpart == "" {
		return
	}
	go func() {
		// The draft path is best-effort and must never take down reply ingest: a
		// panic anywhere in it (provider, repo, publisher) is contained here.
		defer func() {
			if rec := recover(); rec != nil {
				log.Error().Interface("panic", rec).Str("thread_id", r.ThreadID).Msg("inbox agent: draft pipeline panicked")
			}
		}()
		ctx, cancel := context.WithTimeout(context.Background(), draftTimeout)
		defer cancel()
		s.draft(ctx, r)
	}()
}

// draft runs the full pipeline: entitlement + opt-in, dedupe, balance pre-check,
// generate, reserve (insert), charge, publish. It charges only after a draft row
// is reserved, and unwinds the row if the charge fails, so an org is never
// charged for a draft it did not receive.
func (s *service) draft(ctx context.Context, r models.InboxAgentReply) {
	// Paid entitlement.
	if allowed, ferr := s.feature.CanUseInboxAgent(ctx, r.OrganizationID); ferr != nil || !allowed {
		return
	}
	// Per-org opt-in + voice grounding come from the org row.
	org, oerr := s.orgs.GetByID(ctx, r.OrganizationID)
	if oerr != nil || org == nil || !org.InboxAgentEnabled {
		return
	}

	// A trivial ack ("thanks", "ok, got it") does not warrant a paid reply
	// draft. Reuse the classifier's content-sanity gate so an org isn't charged
	// 5 credits to reply to one-liners.
	if !replyclassify.WorthModeling(replyclassify.Input{BodyText: r.Snippet}) {
		return
	}

	// Dedupe: never draft twice for the same inbound message, and at most one
	// pending draft per thread. Cheap pre-check before any model spend.
	src := &r.SourceMessageID
	if r.SourceMessageID == uuid.Nil {
		src = nil
	}
	if has, herr := s.draftRepo.HasActiveDraft(ctx, r.OrganizationID, r.ThreadID, src); herr != nil || has {
		return
	}

	// Balance + abuse-cap pre-check so a broke/capped org never does free model
	// work (the charge itself is enforced again below).
	if bal, berr := s.credits.GetBalance(ctx, r.OrganizationID); berr != nil || bal < credits.CostInboxAgentThread {
		return
	}
	if err := s.credits.CheckUsageCaps(ctx, r.OrganizationID); err != nil {
		return
	}

	// Ground the draft in the thread history + org voice + enabled skills.
	history := s.threadHistory(ctx, r.OrganizationID, r.ThreadID)
	if history == "" {
		return
	}
	voice := generation.VoiceContext{
		ProductDescription: org.ProductDescription,
		ICPNotes:           org.ICPNotes,
		VoiceProfile:       org.VoiceProfile,
	}
	system := generation.BuildReplyRules(voice)
	if s.skills != nil {
		if pre := s.skills.EnabledPreamble(ctx, r.OrganizationID); pre != "" {
			system += "\n\n" + pre
		}
	}
	model := s.provider.ModelForTier(true) // inbox agent is paid-only
	res, gerr := s.provider.Complete(ctx, generation.CompletionRequest{
		System: system,
		Prompt: buildReplyPrompt(history, s.contactGrounding(ctx, r.ContactID)),
		Model:  model,
	})
	if gerr != nil || res == nil || strings.TrimSpace(res.Text) == "" {
		return
	}

	// Reserve the draft row (the partial unique indexes enforce dedupe under a
	// race). Only after a successful reserve do we charge.
	draft := &models.AIThreadDraft{
		OrganizationID:  r.OrganizationID,
		EmailAccountID:  r.EmailAccountID,
		OwnerUserID:     r.OwnerUserID,
		ThreadID:        r.ThreadID,
		SourceMessageID: src,
		ToAddr:          r.Counterpart,
		Subject:         replySubject(r.Subject),
		InReplyTo:       r.InReplyTo,
		Body:            strings.TrimSpace(res.Text),
		IntentClass:     r.IntentClass,
		Confidence:      r.Confidence,
		Model:           res.Model,
		Status:          models.AIDraftPending,
	}
	if r.ContactID != uuid.Nil {
		draft.ContactID = &r.ContactID
	}
	if r.CampaignID != uuid.Nil {
		draft.CampaignID = &r.CampaignID
	}
	if cerr := s.draftRepo.CreateDraft(ctx, draft); cerr != nil {
		// ErrDraftExists => another run/existing draft won the race; anything else
		// is a store failure. Either way: no charge, no draft delivered.
		return
	}

	// Charge exactly once (idempotency key = the draft id, unique per reserve).
	// If the charge fails (out of credits at charge time, or a cap trip), unwind
	// the reserved row so no free draft lingers. Delete on a FRESH context: the
	// draft ctx may be near its deadline after a slow completion, and a failed
	// delete would orphan an unpaid pending draft that could later be
	// approved-and-sent for free. A free/local model (AI_FREE) runs
	// un-metered, so skip the charge (and the unwind).
	if !s.provider.IsLocal() {
		if _, chErr := s.credits.Consume(ctx, r.OrganizationID, credits.CostInboxAgentThread, "inbox_agent_draft", res.Model, res.TokensUsed, "inbox_agent:"+draft.ID.String()); chErr != nil {
			delCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			if derr := s.draftRepo.DeleteDraft(delCtx, draft.ID); derr != nil {
				log.Error().Err(derr).Str("draft_id", draft.ID.String()).Msg("inbox agent: failed to unwind unpaid draft after credit charge failure")
			}
			return
		}
	}

	// Live: the whole team sees the draft land on the thread awaiting review. The
	// source id is optional (the event routes on org_id/thread_id); render it
	// nil-safely — src is nil when the inbound message had no stored id.
	emailID := ""
	if src != nil {
		emailID = src.String()
	}
	if s.publisher != nil {
		s.publisher.PublishAIDraftReady(ctx, r.OrganizationID, uuid.Nil, r.ThreadID, draft.ID.String(), emailID)
	}
}

// threadHistory renders the thread's messages oldest-first for grounding.
func (s *service) threadHistory(ctx context.Context, orgID uuid.UUID, threadID string) string {
	thread, err := s.threads.GetByThread(ctx, orgID, uuid.Nil, threadID, maxThreadMessages, "")
	if err != nil || thread == nil || len(thread.Data) == 0 {
		return ""
	}
	var b strings.Builder
	for _, m := range thread.Data {
		from := strings.Join(m.FromAddr, ", ")
		fmt.Fprintf(&b, "From: %s\nSubject: %s\n%s\n\n", from, m.Subject, strings.TrimSpace(m.Snippet))
	}
	return strings.TrimSpace(b.String())
}

func buildReplyPrompt(history, contactCtx string) string {
	var b strings.Builder
	b.WriteString("Thread so far (oldest first):\n\n")
	b.WriteString(history)
	if contactCtx != "" {
		b.WriteString("\n\n")
		b.WriteString(contactCtx)
	}
	b.WriteString("\n\nWrite a reply to the most recent message in this thread. Keep it natural and specific to what they said.")
	return b.String()
}

// contactGrounding renders a compact CRM block for the counterpart contact
// (name, company, known custom fields), or "" when there is no contact or no
// reader wired. Best-effort: a lookup failure just drops the grounding.
func (s *service) contactGrounding(ctx context.Context, contactID uuid.UUID) string {
	if s.contacts == nil || contactID == uuid.Nil {
		return ""
	}
	c, err := s.contacts.GetByID(ctx, contactID)
	if err != nil || c == nil {
		return ""
	}
	var b strings.Builder
	if name := strings.TrimSpace(c.FirstName + " " + c.LastName); name != "" {
		b.WriteString("Contact: " + name)
		if c.Company != "" {
			b.WriteString(" at " + c.Company)
		}
		b.WriteString("\n")
	}
	if len(c.CustomFields) > 0 {
		parts := make([]string, 0, len(c.CustomFields))
		for k, v := range c.CustomFields {
			if k = strings.TrimSpace(k); k != "" && strings.TrimSpace(v) != "" {
				parts = append(parts, k+": "+v)
			}
		}
		if len(parts) > 0 {
			b.WriteString("Known details: " + strings.Join(parts, ", ") + "\n")
		}
	}
	return strings.TrimSpace(b.String())
}

// replySubject prefixes "Re: " once, mirroring a normal reply subject.
func replySubject(subject string) string {
	s := strings.TrimSpace(subject)
	if s == "" {
		return "Re:"
	}
	if strings.HasPrefix(strings.ToLower(s), "re:") {
		return s
	}
	return "Re: " + s
}
