package tasks

import (
	"context"
	"errors"
	"hash/fnv"
	"html"
	"math/rand"
	"sort"
	"strings"

	"github.com/google/uuid"
	"github.com/rs/zerolog/log"

	"github.com/warmbly/warmbly/internal/app/unsublink"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/pkg/tmplvars"
	"github.com/warmbly/warmbly/internal/repository"
)

// LeadReviewAware is implemented by a TasksService that can read per-lead copy.
type LeadReviewAware interface {
	WireLeadReview(repo repository.CampaignReviewRepository)
}

// LeadEmailsService renders and prepares one lead's emails ahead of sending.
type LeadEmailsService interface {
	LeadEmails(ctx context.Context, orgID uuid.UUID, campaign *models.Campaign, contact *models.Contact) ([]models.LeadEmailStep, *models.LeadEmailFrom, *errx.Error)
	// WriteLeadAI writes the step's AI copy for the lead; rewrite replaces what was written.
	WriteLeadAI(ctx context.Context, orgID uuid.UUID, campaign *models.Campaign, contact *models.Contact, stepID uuid.UUID, rewrite bool) *errx.Error
	// AIBlockIDs lists the AI blocks in a step's own copy.
	AIBlockIDs(bodyHTML string) []AIBlockRef
	// AIAvailable reports whether this instance can write AI copy at all.
	AIAvailable() bool
}

func (s *tasksService) AIAvailable() bool {
	return s.aiProvider != nil && s.aiCredits != nil
}

// AIBlockRef is one AI block in a step: its id, its author's name for it, and whether it researches the web.
type AIBlockRef struct {
	ID       string
	Name     string
	Research bool
}

func (s *tasksService) WireLeadReview(repo repository.CampaignReviewRepository) {
	s.leadReview = repo
}

// leadOverride reads a lead's own copy of a step, nil when there is none.
func (s *tasksService) leadOverride(ctx context.Context, campaignID, contactID, stepID uuid.UUID) (*models.LeadStepOverride, error) {
	if s.leadReview == nil {
		return nil, nil
	}
	return s.leadReview.GetOverride(ctx, campaignID, contactID, stepID)
}

// spintaxRand picks spintax options the same way for one lead, step and part
// every time, so a preview and a retried send carry the copy the send does.
// The HTML and plain parts share a seed, so they agree when their groups come in the same order.
func spintaxRand(campaignID, contactID, stepID uuid.UUID, part string) *rand.Rand {
	h := fnv.New64a()
	_, _ = h.Write(campaignID[:])
	_, _ = h.Write(contactID[:])
	_, _ = h.Write(stepID[:])
	_, _ = h.Write([]byte(part))
	return rand.New(rand.NewSource(int64(h.Sum64())))
}

func expandSpintaxWith(s string, rng *rand.Rand) string {
	return ResolveSpintax(s, func(opts []string) string { return opts[rng.Intn(len(opts))] })
}

// renderLeadCopy renders a step's chosen copy for one lead: merge fields once,
// then spintax seeded by lead and step. The send path and the preview share it.
func renderLeadCopy(campaignID, stepID uuid.UUID, contact models.Contact, extra TemplateContext, subject, bodyHTML, bodyPlain string) (string, string, string) {
	out := []string{
		expandSpintaxWith(RenderTemplateWith(subject, contact, extra), spintaxRand(campaignID, contact.ID, stepID, "subject")),
		expandSpintaxWith(RenderHTMLTemplateWith(bodyHTML, contact, extra), spintaxRand(campaignID, contact.ID, stepID, "body")),
		expandSpintaxWith(RenderTemplateWith(bodyPlain, contact, extra), spintaxRand(campaignID, contact.ID, stepID, "body")),
	}
	return out[0], out[1], out[2]
}

func (s *tasksService) AIBlockIDs(bodyHTML string) []AIBlockRef {
	refs := findAIVarRefs(bodyHTML)
	out := make([]AIBlockRef, 0, len(refs))
	for _, r := range refs {
		ref := AIBlockRef{ID: r.id}
		if cfg, ok := decodeAIVarConfig(r.config); ok {
			ref.Name = strings.TrimSpace(cfg.Name)
			ref.Research = strings.EqualFold(strings.TrimSpace(cfg.Mode), "research")
		}
		out = append(out, ref)
	}
	return out
}

// variantPeeker is the read-only half of A/B selection a preview uses.
type variantPeeker interface {
	PeekVariant(ctx context.Context, organizationID, campaignID, contactID, sequenceID uuid.UUID, subject, bodyHTML, bodyPlain string) (*models.VariantSelection, *errx.Error)
}

// leadStepSource is the raw copy a step sends one lead: their own copy when
// they have one, otherwise the step's or the A/B arm drawn for them.
type leadStepSource struct {
	subject, bodyHTML, bodyPlain string
	override                     *models.LeadStepOverride
	variantID                    *uuid.UUID
}

func (s *tasksService) leadStepCopy(ctx context.Context, orgID, campaignID uuid.UUID, contactID uuid.UUID, step *models.Sequence, peek bool) (leadStepSource, error) {
	src := leadStepSource{subject: step.Subject, bodyHTML: step.BodyHTML, bodyPlain: step.BodyPlain}
	o, err := s.leadOverride(ctx, campaignID, contactID, step.ID)
	if err != nil {
		return src, err
	}
	if o != nil {
		src.subject, src.bodyHTML, src.bodyPlain, src.override = o.Subject, o.BodyHTML, o.BodyPlain, o
		return src, nil
	}
	if s.advanced == nil {
		return src, nil
	}
	var sel *models.VariantSelection
	var xerr *errx.Error
	if peeker, ok := s.advanced.(variantPeeker); ok && peek {
		sel, xerr = peeker.PeekVariant(ctx, orgID, campaignID, contactID, step.ID, src.subject, src.bodyHTML, src.bodyPlain)
	} else {
		sel, xerr = s.advanced.SelectVariant(ctx, orgID, campaignID, contactID, step.ID, src.subject, src.bodyHTML, src.bodyPlain)
	}
	if xerr != nil {
		return src, xerr
	}
	if sel != nil {
		src.subject, src.bodyHTML, src.bodyPlain = sel.Subject, sel.BodyHTML, sel.BodyPlain
		if sel.StepArm != nil {
			src.variantID = sel.StepArm.VariantID
		} else {
			src.variantID = sel.VariantID
		}
	}
	return src, nil
}

func isEmailStep(step *models.Sequence) bool {
	return step.Kind == "" || step.Kind == "email"
}

// LeadEmails renders every email step of the campaign for one lead, the way
// the send path will: their own copy or A/B arm, merge fields, spintax, the AI
// copy written so far, signature and opt-out. Nothing is written or charged.
func (s *tasksService) LeadEmails(ctx context.Context, orgID uuid.UUID, campaign *models.Campaign, contact *models.Contact) ([]models.LeadEmailStep, *models.LeadEmailFrom, *errx.Error) {
	steps, err := s.campaignRepo.GetSequencesByCampaignID(ctx, campaign.ID)
	if err != nil {
		return nil, nil, errx.InternalError()
	}
	progress, err := s.campaignProgressRepo.GetContactProgress(ctx, campaign.ID, contact.ID)
	if err != nil {
		return nil, nil, errx.InternalError()
	}
	sent := map[uuid.UUID]repository.CampaignContactProgress{}
	for _, p := range progress {
		if p.SentAt != nil {
			sent[p.SequenceID] = p
		}
	}

	// The mailbox already writing to this lead, when there is one.
	var account *models.Email
	if last, lerr := s.campaignProgressRepo.LastSenderForLead(ctx, campaign.ID, contact.ID); lerr == nil && last != nil {
		if acc, xerr := s.emailRepo.GetByID(ctx, *last); xerr == nil {
			account = acc
		}
	}
	parent, _ := s.campaignProgressRepo.ThreadParentForLead(ctx, campaign.ID, contact.ID)

	unsubURL := PreviewUnsubscribeLink
	if s.unsubLinks != nil && s.unsubLinks.Enabled() {
		unsubURL = s.unsubLinks.TicketURL("", unsublink.ExampleTicket)
	}
	optOut := s.resolveOptOut(ctx, orgID, campaign)
	extra := templateContext(account, unsubURL)
	values := map[string]string{"FirstName": contact.FirstName, "LastName": contact.LastName, "Company": contact.Company, "Phone": contact.Phone, "Email": contact.Email}

	out := []models.LeadEmailStep{}
	firstSubject := ""
	for i := range steps {
		step := &steps[i]
		if !isEmailStep(step) {
			continue
		}
		src, serr := s.leadStepCopy(ctx, orgID, campaign.ID, contact.ID, step, true)
		if serr != nil {
			log.Warn().Err(serr).Str("campaign_id", campaign.ID.String()).Msg("lead emails: could not read the lead's copy")
			return nil, nil, errx.InternalError()
		}
		item := models.LeadEmailStep{StepID: step.ID, Position: step.Position, Name: step.Name, VariantID: src.variantID,
			Source: models.LeadStepOverrideInput{Subject: src.subject, BodyHTML: src.bodyHTML, BodyPlain: src.bodyPlain}}
		if src.override != nil {
			item.Personalized = true
			edited := src.override.UpdatedAt
			item.EditedAt = &edited
			item.StepChanged = step.UpdatedAt.After(edited)
		}
		for _, part := range []tmplvars.Part{{Name: "subject", Text: src.subject}, {Name: "body", Text: src.bodyHTML, HTML: true}, {Name: "plain text", Text: src.bodyPlain}} {
			if cerr := tmplvars.CheckCopy(part); cerr != nil {
				item.Errors = append(item.Errors, cerr.Error())
			}
		}
		item.Missing = missingFor(values, src.subject, src.bodyHTML, src.bodyPlain)

		subject, bodyHTML, bodyPlain := renderLeadCopy(campaign.ID, step.ID, *contact, extra, src.subject, src.bodyHTML, src.bodyPlain)
		// A follow-up replying in the thread carries the conversation's subject.
		if step.ThreadReply && firstSubject != "" {
			item.Threaded = true
			subject = firstSubject
			if parent != nil && parent.Subject != "" {
				subject = parent.Subject
				if !parent.SubjectSent {
					// As the send renders an unrecorded conversation subject: merge fields, then this step's spintax.
					subject, _, _ = renderLeadCopy(campaign.ID, step.ID, *contact, extra, parent.Subject, "", "")
				}
			}
		}

		resolved, _ := s.campaignProgressRepo.GetResolvedAIVariables(ctx, campaign.ID, contact.ID, step.ID)
		blocks := s.AIBlockIDs(src.bodyHTML)
		for _, b := range blocks {
			_, ok := resolved[b.ID]
			item.AIBlocks = append(item.AIBlocks, models.LeadEmailAIBlock{ID: b.ID, Name: b.Name, Written: ok})
		}
		subject, bodyHTML, bodyPlain = previewAIVariables(subject, bodyHTML, bodyPlain, resolved, blocks)
		bodyHTML, bodyPlain = finishBody(bodyHTML, bodyPlain, campaign.TextOnly, account, &optOut, unsubURL)
		item.Subject, item.BodyHTML, item.BodyPlain = subject, bodyHTML, bodyPlain
		if firstSubject == "" {
			firstSubject = subject
		}
		if p, ok := sent[step.ID]; ok {
			item.Sent = true
			item.SentAt = p.SentAt
		}
		out = append(out, item)
	}
	sort.SliceStable(out, func(a, b int) bool { return out[a].Position < out[b].Position })

	var from *models.LeadEmailFrom
	if account != nil {
		from = &models.LeadEmailFrom{Name: extra.Sender.Name, Email: extra.Sender.Email}
	}
	return out, from, nil
}

// aiPendingPlaceholder stands in for an unwritten AI block in plain text and the subject.
func aiPendingPlaceholder(name string) string {
	if name == "" {
		return "[AI copy]"
	}
	return "[AI: " + name + "]"
}

// previewAIVariables substitutes AI blocks for a preview: written copy wrapped
// in a span naming its block, so the dashboard can mark what AI wrote, and an
// empty marker where copy is still to be written. Never used on a send.
func previewAIVariables(subject, bodyHTML, bodyPlain string, resolved map[string]string, blocks []AIBlockRef) (string, string, string) {
	names := map[string]string{}
	for _, b := range blocks {
		names[b.ID] = b.Name
	}
	bodyHTML = aiVarSpanRE.ReplaceAllStringFunc(bodyHTML, func(span string) string {
		m := aiVarSpanRE.FindStringSubmatch(span)
		if len(m) < 2 {
			return ""
		}
		id := html.EscapeString(m[1])
		if text, ok := resolved[m[1]]; ok {
			return `<span data-ai-block="` + id + `">` + html.EscapeString(text) + `</span>`
		}
		return `<span data-ai-pending="` + id + `" data-ai-name="` + html.EscapeString(names[m[1]]) + `"></span>`
	})
	plainShown := map[string]string{}
	for _, b := range blocks {
		if text, ok := resolved[b.ID]; ok {
			plainShown[b.ID] = text
		} else {
			plainShown[b.ID] = aiPendingPlaceholder(b.Name)
		}
	}
	return replaceAIVarTokens(subject, plainShown), bodyHTML, replaceAIVarTokens(bodyPlain, plainShown)
}

// missingFor lists the built-in fields the copy prints with no fallback that this lead has empty.
func missingFor(values map[string]string, parts ...string) []string {
	gaps := map[string]bool{}
	for _, p := range parts {
		g, ok := tmplvars.BlankGaps(p)
		if !ok {
			g = tmplvars.References(p)
		}
		for k := range g {
			gaps[k] = true
		}
	}
	var out []string
	for _, f := range models.MissingFieldNames {
		if gaps[f] && strings.TrimSpace(values[f]) == "" {
			out = append(out, f)
		}
	}
	return out
}

// leadCopyChanged reports whether the lead's own copy or written AI copy of the
// step differs from what this send built. Read after the send's claim, which
// every copy write is ordered against.
func (s *tasksService) leadCopyChanged(ctx context.Context, campaignID, contactID, stepID uuid.UUID, used *models.LeadStepOverride, usedAI map[string]string) (bool, error) {
	now, err := s.leadOverride(ctx, campaignID, contactID, stepID)
	if err != nil {
		return false, err
	}
	if (now == nil) != (used == nil) || (now != nil && !now.UpdatedAt.Equal(used.UpdatedAt)) {
		return true, nil
	}
	if len(usedAI) == 0 {
		return false, nil
	}
	cached, err := s.campaignProgressRepo.GetResolvedAIVariables(ctx, campaignID, contactID, stepID)
	if err != nil {
		return false, err
	}
	for id, text := range usedAI {
		if cached[id] != text {
			return true, nil
		}
	}
	return false, nil
}

// ErrLeadStepNotEmail is a write-ahead asked for on a step that sends no email.
var ErrLeadStepNotEmail = errors.New("step is not an email step")

// WriteLeadAI writes a step's AI copy for one lead now rather than at send
// time, so it can be read before it goes out. It is cached exactly where the
// send reads it, so the send reuses it and nothing is charged twice.
func (s *tasksService) WriteLeadAI(ctx context.Context, orgID uuid.UUID, campaign *models.Campaign, contact *models.Contact, stepID uuid.UUID, rewrite bool) *errx.Error {
	if s.aiProvider == nil || s.aiCredits == nil {
		return errx.NewWithIdentifier(errx.ServiceUnavailable, "ai_unavailable", "AI is not available on this instance.")
	}
	steps, err := s.campaignRepo.GetSequencesByCampaignID(ctx, campaign.ID)
	if err != nil {
		return errx.InternalError()
	}
	var step *models.Sequence
	for i := range steps {
		if steps[i].ID == stepID {
			step = &steps[i]
		}
	}
	if step == nil {
		return errx.New(errx.NotFound, "step not found in this campaign")
	}
	if !isEmailStep(step) {
		return errx.New(errx.BadRequest, ErrLeadStepNotEmail.Error())
	}
	src, serr := s.leadStepCopy(ctx, orgID, campaign.ID, contact.ID, step, false)
	if serr != nil {
		return errx.InternalError()
	}
	if len(findAIVarRefs(src.bodyHTML)) == 0 {
		return nil
	}
	if s.leadReview == nil {
		return errx.InternalError()
	}
	sentErr := errx.NewWithIdentifier(errx.Conflict, "lead_step_sent", "This lead already received this email, so its copy can no longer change.")
	// Checked before any charge; the guarded save below is what holds.
	if sentAt, err := s.campaignProgressRepo.GetStepSentAt(ctx, campaign.ID, contact.ID, step.ID); err != nil {
		return errx.InternalError()
	} else if sentAt != nil {
		return sentErr
	}
	if rewrite {
		ctx = withAIVarRewrite(ctx)
	}
	// Each block replaces its copy only once written, and never on a step already reserved or sent.
	w := aiVarWrite{fresh: rewrite, save: func(id, text string) error {
		return s.leadReview.SaveLeadAIVariable(ctx, campaign.ID, contact.ID, step.ID, id, text, rewrite)
	}}
	extra := templateContext(nil, PreviewUnsubscribeLink)
	subject, bodyHTML, bodyPlain := renderLeadCopy(campaign.ID, step.ID, *contact, extra, src.subject, src.bodyHTML, src.bodyPlain)
	if _, _, _, _, aerr := s.resolveAIVariablesWith(ctx, campaign, contact, step.ID, subject, bodyHTML, bodyPlain, w); aerr != nil {
		switch {
		case errors.Is(aerr, repository.ErrLeadStepSent):
			return sentErr
		case errors.Is(aerr, ErrAIVarBalance):
			return errx.NewWithIdentifier(errx.PaymentRequired, "insufficient_credits", "The workspace balance is too low. Add money under Billing to keep going.")
		case errors.Is(aerr, ErrAIVarCap):
			return errx.NewWithIdentifier(errx.TooManyRequests, "usage_cap_exceeded", "The workspace reached its spend limit for now.")
		}
		log.Warn().Err(aerr).Str("campaign_id", campaign.ID.String()).Str("step_id", step.ID.String()).Msg("write AI copy ahead failed")
		return errx.NewWithIdentifier(errx.ServiceUnavailable, "ai_generation_failed", "The AI copy could not be written. Try again in a moment.")
	}
	return nil
}
