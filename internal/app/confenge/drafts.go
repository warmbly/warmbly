package confenge

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/pkg/generation"
)

// GenerateDraft creates or regenerates a draft for an account using AI when
// configured, otherwise a deterministic template (never auto-approved).
func (s *service) GenerateDraft(ctx context.Context, orgID, userID, accountID uuid.UUID, contactID *uuid.UUID) (*models.OutreachDraft, *errx.Error) {
	if xerr := s.requireEnabled(); xerr != nil {
		return nil, xerr
	}
	acc, err := s.repo.GetAccount(ctx, orgID, accountID)
	if err != nil || acc == nil {
		return nil, errx.New(errx.NotFound, "account not found")
	}
	if acc.DoNotContact || acc.Blocked {
		return nil, errx.New(errx.BadRequest, "account is blocked or DO_NOT_CONTACT")
	}
	if err := RequireTargetFit(acc); err != nil {
		return nil, errx.New(errx.BadRequest, err.Error())
	}

	var cand *models.OutreachContactCandidate
	if contactID != nil {
		cand, err = s.repo.GetCandidate(ctx, orgID, *contactID)
		if err != nil || cand == nil || cand.AccountID != accountID {
			return nil, errx.New(errx.NotFound, "contact candidate not found")
		}
	} else {
		list, err := s.repo.ListCandidates(ctx, orgID, accountID)
		if err != nil {
			return nil, errx.New(errx.Internal, "failed to list contacts")
		}
		cand = pickRecommended(list)
	}
	if cand == nil {
		return nil, errx.New(errx.BadRequest, "no contact candidate; resolve NEEDS_CONTACT first")
	}
	if err := RequireEmailOutbound(acc, cand); err != nil {
		return nil, errx.New(errx.BadRequest, err.Error())
	}

	allCands, listErr := s.repo.ListCandidates(ctx, orgID, accountID)
	if listErr != nil {
		allCands = nil
	}
	rec := ResolveRecipient(acc, allCands, time.Now().UTC())

	evidence, err := s.repo.ListEvidence(ctx, orgID, accountID)
	if err != nil {
		return nil, errx.New(errx.Internal, "failed to load evidence")
	}

	existing, _ := s.repo.GetActiveDraftForAccount(ctx, orgID, accountID)
	draft := &models.OutreachDraft{
		OrganizationID:     orgID,
		AccountID:          accountID,
		ContactCandidateID: &cand.ID,
		Channel:            models.OutreachChannelEmail,
		RecipientName:      cand.Name,
		RecipientRole:      cand.Role,
		RecipientEmail:     cand.Email,
		VerificationStatus: cand.VerificationStatus,
		Status:             models.OutreachDraftGenerating,
		PromptVersion:      PromptVersion,
	}
	if isGenericRecipient(cand) {
		draft.RecipientName = ""
		draft.RecipientRole = ""
	}
	if existing != nil {
		draft.ID = existing.ID
		draft.Generation = existing.Generation + 1
		draft.CreatedAt = existing.CreatedAt
	}

	recent := recentDraftBodies(ctx, s, orgID, accountID, models.OutreachChannelEmail)
	in := BuildGenerateInput(ChannelEmailInitial, acc, cand, evidence, recent)
	pb, _ := LoadPlaybook()
	st, plan := BuildOutboundPlan(pb, acc, cand, evidence, 1)
	gen := s.generator()
	var out DraftOutput
	var provider, model string
	var genErr error
	usedTemplate := false
	if plan.Messageability != MessageabilityReady {
		out = FailClosedDraft(plan, ChannelEmailInitial)
		provider, model = "template", "messageability_gate"
	} else {
		out, provider, model, genErr = gen.Generate(ctx, in)
		if genErr != nil {
			out = ComposeFromPlan(plan, acc, cand, ChannelEmailInitial)
			provider = "template"
			model = "strategy_compose"
			usedTemplate = true
			out.RiskFlags = append(out.RiskFlags, "generation_failed_strategy_compose")
		}
	}
	if out.ServiceCode == "" {
		out.ServiceCode = acc.ServiceCode
	}
	if len(out.EvidenceIDs) == 0 && len(acc.MomentEvidenceIDs) > 0 {
		out.EvidenceIDs = acc.MomentEvidenceIDs
	}
	if out.Channel == "" {
		out.Channel = ChannelEmailInitial
	}

	val := ValidateDraft(&out, acc, cand, ValidateOpts{
		MaxWords: s.cfg.MaxInitialEmailWords, Evidence: evidence,
		Channel: ChannelEmailInitial, RecentBodies: recent,
		Strategy: &st, Playbook: pb,
	})
	risk, flags := ClassifyRisk(acc, cand, &out, val)
	if usedTemplate {
		flags = append(flags, "template_fallback")
		if risk == "GREEN" {
			risk = "YELLOW"
		}
	}
	// Template fallback + incomplete commercial context → never send-ready.
	incomplete := containsStr(st.RiskFlags, "incomplete_strategy") ||
		containsStr(st.RiskFlags, "incomplete_copy_context") ||
		containsStr(st.RiskFlags, "unknown_service_code") ||
		containsStr(st.RiskFlags, "missing_service_code") ||
		strings.TrimSpace(st.MicroOfferCode) == "" ||
		strings.TrimSpace(st.WhyThisAccount) == "" ||
		strings.TrimSpace(st.WhyNow) == "" ||
		strings.TrimSpace(st.ObservedFact) == ""
	if usedTemplate && incomplete {
		flags = append(flags, "template_fallback_incomplete_context")
		risk = "RED"
	}
	if incomplete {
		flags = append(flags, "incomplete_copy_context")
		if risk == "GREEN" {
			risk = "YELLOW"
		}
	}
	if plan.Messageability != MessageabilityReady {
		flags = appendUnique(flags, "messageability_"+strings.ToLower(plan.Messageability))
		flags = appendUnique(flags, plan.ReasonCodes...)
		risk = "RED"
		val.OK = false
		if plan.Reason != "" {
			val.Errors = appendUnique(val.Errors, plan.Reason)
		}
	}
	val.Recipient = &rec
	if rec.State != RecipientValidated {
		flags = appendUnique(flags, "recipient_"+strings.ToLower(rec.State))
		flags = appendUnique(flags, rec.ReasonCodes...)
		risk = "RED"
		val.OK = false
		if rec.Reason != "" {
			val.Errors = appendUnique(val.Errors, rec.Reason)
		}
	}
	val.Claims = out.Claims
	val.Rationale = out.Rationale
	val.Channel = out.Channel
	recipient := cand.Email
	if cand.Name != "" && !isGenericRecipient(cand) {
		recipient = cand.Name + " <" + cand.Email + ">"
	}
	valJSON := PackValidationWithPlan(val, st, plan, recipient)
	followJSON, _ := json.Marshal(out.Followups)
	draft.Subject = SanitizeText(out.Subject, 200)
	draft.BodyText = SanitizeText(out.BodyText, 8000)
	draft.BodyHTML = ""
	draft.FollowupsJSON = followJSON
	draft.ServiceCode = SanitizeText(out.ServiceCode, 100)
	draft.StrategyCode = SanitizeText(StrategyCodeFor(st), 200)
	draft.FactUsed = SanitizeText(out.FactUsed, 2000)
	draft.EvidenceIDs = collectEvidenceIDs(&out)
	draft.Question = SanitizeText(out.Question, 1000)
	draft.CTA = SanitizeText(out.CTA, 500)
	draft.Provider = provider
	draft.Model = model
	draft.ValidationJSON = valJSON
	ok := val.OK
	draft.ValidationOK = &ok
	draft.RiskClass = risk
	draft.RiskFlags = flags
	// NEEDS_REVIEW is only for sendable copy awaiting human authorization.
	switch {
	case rec.State == RecipientBlocked || plan.Messageability == MessageabilityBlocked:
		draft.Status = models.OutreachDraftBlocked
		draft.Subject, draft.BodyText = "", ""
	case rec.State != RecipientValidated || plan.Messageability != MessageabilityReady:
		draft.Status = models.OutreachDraftSkipped
		draft.Subject, draft.BodyText = "", ""
	default:
		if !val.OK || strings.TrimSpace(out.BodyText) == "" {
			draft.Status = models.OutreachDraftSkipped
			draft.Subject, draft.BodyText = "", ""
		} else {
			draft.Status = models.OutreachDraftNeedsReview
		}
	}
	if err := s.repo.UpsertDraft(ctx, draft); err != nil {
		return nil, errx.New(errx.Internal, "failed to save draft: "+err.Error())
	}

	// Move account into review queue only when the message is sendable.
	if draft.Status == models.OutreachDraftNeedsReview &&
		(acc.QueueState == models.OutreachQueueReadyToGenerate || acc.QueueState == models.OutreachQueueNeedsContact) {
		_ = s.repo.SetAccountHumanFlags(ctx, orgID, accountID, acc.Blocked, acc.DoNotContact, acc.BlockReason, models.OutreachQueueNeedsReview)
	}

	if s.audit != nil {
		s.audit.LogAction(ctx, orgID, userID, models.AuditActionCreate, models.AuditEntityOutreachAccount, &accountID, "", "",
			map[string]string{"draft_id": draft.ID.String(), "status": draft.Status, "risk": draft.RiskClass},
			map[string]string{"provider": provider},
		)
	}
	return draft, nil
}

func (s *service) generator() DraftGenerator {
	if s.ai != nil {
		return &AIDraftGenerator{Provider: s.ai}
	}
	return TemplateGenerator{}
}

func pickRecommended(list []models.OutreachContactCandidate) *models.OutreachContactCandidate {
	for i := range list {
		c := &list[i]
		if c.Recommended && c.CanEnroll() && c.EmailSendReady && !c.MailboxPurposeSendBlocked {
			return c
		}
	}
	for i := range list {
		c := &list[i]
		if c.CanEnroll() && c.EmailSendReady && !c.MailboxPurposeSendBlocked {
			return c
		}
	}
	var fallback *models.OutreachContactCandidate
	for i := range list {
		c := &list[i]
		if c.DoNotContact || c.Bounced || c.Blocked {
			continue
		}
		if c.Email == "" {
			continue
		}
		if c.Recommended && c.CanEnroll() {
			return c
		}
		if fallback == nil && c.CanEnroll() {
			fallback = c
		}
		if fallback == nil {
			fallback = c
		}
	}
	return fallback
}

// GetDraft returns one draft by id.
func (s *service) GetDraft(ctx context.Context, orgID, id uuid.UUID) (*models.OutreachDraft, *errx.Error) {
	if xerr := s.requireEnabled(); xerr != nil {
		return nil, xerr
	}
	d, err := s.repo.GetDraft(ctx, orgID, id)
	if err != nil {
		return nil, errx.New(errx.Internal, "failed to load draft")
	}
	if d == nil {
		return nil, errx.New(errx.NotFound, "draft not found")
	}
	return d, nil
}

// ListDrafts lists drafts optionally filtered by status.
func (s *service) ListDrafts(ctx context.Context, orgID uuid.UUID, status string, limit, offset int) ([]models.OutreachDraft, *errx.Error) {
	if xerr := s.requireEnabled(); xerr != nil {
		return nil, xerr
	}
	list, err := s.repo.ListDrafts(ctx, orgID, status, limit, offset)
	if err != nil {
		return nil, errx.New(errx.Internal, "failed to list drafts")
	}
	if list == nil {
		list = []models.OutreachDraft{}
	}
	return list, nil
}

// ReviewDraft applies human actions: approve, reject, skip, edit, block.
func (s *service) ReviewDraft(ctx context.Context, orgID, userID, draftID uuid.UUID, action string, edit *DraftEdit) (*models.OutreachDraft, *errx.Error) {
	if xerr := s.requireEnabled(); xerr != nil {
		return nil, xerr
	}
	d, err := s.repo.GetDraft(ctx, orgID, draftID)
	if err != nil || d == nil {
		return nil, errx.New(errx.NotFound, "draft not found")
	}
	beforeBody, beforeSubj := d.BodyText, d.Subject
	switch action {
	case "edit":
		if edit == nil {
			return nil, errx.New(errx.BadRequest, "edit payload required")
		}
		if edit.Subject != nil {
			d.Subject = SanitizeText(*edit.Subject, 200)
		}
		if edit.BodyText != nil {
			d.BodyText = SanitizeText(*edit.BodyText, 8000)
		}
		d.HumanEdited = true
		// Editing a draft invalidates any linked touchpoint approval.
		if linked, _ := s.repo.GetTouchpointByDraft(ctx, orgID, draftID); linked != nil {
			ApplyContentMutation(linked, linked.Channel, linked.Recipient, d.Subject, d.BodyText)
			_ = s.repo.UpdateTouchpoint(ctx, linked)
		}
		// Re-validate after edit
		acc, _ := s.repo.GetAccount(ctx, orgID, d.AccountID)
		var cand *models.OutreachContactCandidate
		if d.ContactCandidateID != nil {
			cand, _ = s.repo.GetCandidate(ctx, orgID, *d.ContactCandidateID)
		}
		out := DraftOutput{
			Subject: d.Subject, BodyText: d.BodyText, FactUsed: d.FactUsed,
			ServiceCode: d.ServiceCode, EvidenceIDs: d.EvidenceIDs,
			Question: d.Question, CTA: d.CTA, Channel: channelKindFromDraft(d),
		}
		ev, _ := s.repo.ListEvidence(ctx, orgID, d.AccountID)
		pb, _ := LoadPlaybook()
		st := PlanOutreachStrategy(pb, acc, cand, ev, 1)
		if out.ServiceCode == "" && acc != nil {
			out.ServiceCode = acc.ServiceCode
		}
		if out.FactUsed == "" && acc != nil {
			out.FactUsed = acc.FactToMention
		}
		val := ValidateDraft(&out, acc, cand, ValidateOpts{
			MaxWords: maxWordsForDraft(s, d), Evidence: ev, Channel: out.Channel,
			SkipEmailRecipient: d.Channel == models.OutreachChannelWhatsApp,
			Strategy:           &st, Playbook: pb,
		})
		if allCands, listErr := s.repo.ListCandidates(ctx, orgID, d.AccountID); listErr == nil {
			rec := ResolveRecipient(acc, allCands, time.Now().UTC())
			val.Recipient = &rec
		}
		risk, flags := ClassifyRisk(acc, cand, &out, val)
		// Persist structural incompleteness flags so approve cannot ignore them.
		for _, f := range st.RiskFlags {
			if f != "" && !containsStr(flags, f) {
				flags = append(flags, f)
			}
		}
		if incomplete := StructuralApproveBlockers(acc, cand, &st, d, pb); len(incomplete) > 0 {
			if !containsStr(flags, "incomplete_copy_context") {
				flags = append(flags, "incomplete_copy_context")
			}
			if risk == "GREEN" {
				risk = "YELLOW"
			}
			// incomplete structural gates demote to not-OK for operator clarity
			val.OK = false
			for _, b := range incomplete {
				val.Errors = append(val.Errors, b)
			}
		}
		recipient := d.RecipientEmail
		if cand != nil && cand.Name != "" {
			recipient = cand.Name + " <" + cand.Email + ">"
		}
		d.ValidationJSON = PackValidationWithStrategy(val, st, recipient)
		ok := val.OK
		d.ValidationOK = &ok
		d.RiskClass = risk
		d.RiskFlags = flags
		d.Status = models.OutreachDraftNeedsReview
	case "approve":
		acc, _ := s.repo.GetAccount(ctx, orgID, d.AccountID)
		var cand *models.OutreachContactCandidate
		if d.ContactCandidateID != nil {
			cand, _ = s.repo.GetCandidate(ctx, orgID, *d.ContactCandidateID)
		}
		out := DraftOutput{
			Subject: d.Subject, BodyText: d.BodyText, FactUsed: d.FactUsed,
			ServiceCode: d.ServiceCode, EvidenceIDs: d.EvidenceIDs,
			Question: d.Question, CTA: d.CTA,
			Channel: channelKindFromDraft(d),
		}
		ev, _ := s.repo.ListEvidence(ctx, orgID, d.AccountID)
		pb, _ := LoadPlaybook()
		// Rebuild strategy from persisted account/evidence — do not trust a
		// weaker text-only revalidation path.
		st := PlanOutreachStrategy(pb, acc, cand, ev, 1)
		if persisted := strategyFromDraft(d); persisted != nil {
			// Merge persisted micro-offer / why fields when human-repaired via regen
			// but keep fail-closed risk flags from both.
			st.RiskFlags = appendUnique(st.RiskFlags, persisted.RiskFlags...)
			if st.MicroOfferCode == "" {
				st.MicroOfferCode = persisted.MicroOfferCode
			}
			if st.WhyThisAccount == "" {
				st.WhyThisAccount = persisted.WhyThisAccount
			}
			if st.WhyNow == "" {
				st.WhyNow = persisted.WhyNow
			}
			if st.ObservedFact == "" {
				st.ObservedFact = persisted.ObservedFact
			}
		}
		if out.ServiceCode == "" && acc != nil {
			out.ServiceCode = acc.ServiceCode
		}
		if out.FactUsed == "" {
			fact := st.ObservedFact
			if fact == "" && acc != nil {
				fact = acc.FactToMention
			}
			out.FactUsed = fact
		}
		val := ValidateDraft(&out, acc, cand, ValidateOpts{
			MaxWords: maxWordsForDraft(s, d), Evidence: ev, Channel: out.Channel,
			SkipEmailRecipient: d.Channel == models.OutreachChannelWhatsApp,
			Strategy:           &st, Playbook: pb,
		})
		if !val.OK {
			return nil, errx.New(errx.BadRequest, "draft failed validation: "+joinErrs(val.Errors))
		}
		if blockers := StructuralApproveBlockers(acc, cand, &st, d, pb); len(blockers) > 0 {
			return nil, errx.New(errx.BadRequest, FormatApproveBlockers(blockers))
		}
		if d.Provider == "template" && s.cfg.RequireHumanApproval {
			// Template is allowed after explicit human approve (still structural-gated).
		}
		now := time.Now().UTC()
		d.Status = models.OutreachDraftApproved
		d.ApprovedBy = &userID
		d.ApprovedAt = &now
		if acc != nil {
			_ = s.repo.SetAccountHumanFlags(ctx, orgID, d.AccountID, acc.Blocked, acc.DoNotContact, acc.BlockReason, models.OutreachQueueApproved)
			email := ""
			if cand != nil {
				email = cand.Email
			}
			_ = s.EnqueueOutcome(ctx, orgID, models.OutreachOutcome{
				IdempotencyKey: "lead_reviewed:" + d.ID.String(),
				SourceLeadID:   acc.SourceLeadID,
				CNPJ14:         acc.CNPJ14,
				ContactEmail:   email,
				EventType:      OutcomeLeadReviewed,
				OccurredAt:     now,
			})
		}
	case "reject":
		d.Status = models.OutreachDraftRejected
	case "skip":
		d.Status = models.OutreachDraftSkipped
		acc, _ := s.repo.GetAccount(ctx, orgID, d.AccountID)
		if acc != nil {
			_ = s.repo.SetAccountHumanFlags(ctx, orgID, d.AccountID, acc.Blocked, acc.DoNotContact, acc.BlockReason, models.OutreachQueueSkipped)
		}
	case "block":
		d.Status = models.OutreachDraftBlocked
		reason := "blocked from review"
		if edit != nil && edit.Reason != nil {
			reason = *edit.Reason
		}
		dnc := edit != nil && edit.DoNotContact
		_ = s.repo.SetAccountHumanFlags(ctx, orgID, d.AccountID, true, dnc, reason,
			map[bool]string{true: models.OutreachQueueDoNotContact, false: models.OutreachQueueBlocked}[dnc])
		if acc, _ := s.repo.GetAccount(ctx, orgID, d.AccountID); acc != nil {
			evType := OutcomeLeadReviewed
			if dnc {
				evType = OutcomeDoNotContact
			}
			email := d.RecipientEmail
			_ = s.EnqueueOutcome(ctx, orgID, models.OutreachOutcome{
				IdempotencyKey: fmt.Sprintf("%s:%s", strings.ToLower(evType), d.ID.String()),
				SourceLeadID:   acc.SourceLeadID,
				CNPJ14:         acc.CNPJ14,
				ContactEmail:   email,
				EventType:      evType,
				OccurredAt:     time.Now().UTC(),
			})
		}
	default:
		return nil, errx.New(errx.BadRequest, "unknown action (approve|reject|skip|edit|block)")
	}
	decision := mapReviewAction(action)
	if decision != "" {
		reasons := []string{}
		if edit != nil && edit.Reason != nil {
			reasons = []string{*edit.Reason}
		}
		if hc, herr := RecordHumanDecision(decision, d.ID.String(), userID.String(), beforeBody, d.BodyText, beforeSubj, d.Subject, reasons); herr == nil {
			attachHumanCorrection(d, hc)
		}
	}
	if err := s.repo.UpsertDraft(ctx, d); err != nil {
		return nil, errx.New(errx.Internal, "failed to update draft")
	}
	if s.audit != nil {
		s.audit.LogAction(ctx, orgID, userID, models.AuditActionUpdate, models.AuditEntityOutreachAccount, &d.AccountID, "", "",
			map[string]string{"draft_id": d.ID.String(), "action": action, "status": d.Status},
			nil,
		)
	}
	return d, nil
}

func mapReviewAction(action string) string {
	switch strings.ToLower(strings.TrimSpace(action)) {
	case "approve":
		return DecisionApprove
	case "edit":
		return DecisionEdit
	case "reject":
		return DecisionReject
	case "skip":
		return DecisionSkip
	case "recipient_change":
		return DecisionRecipientChange
	default:
		return ""
	}
}

// DraftEdit is an optional body for review actions.
type DraftEdit struct {
	Subject      *string `json:"subject"`
	BodyText     *string `json:"body_text"`
	Reason       *string `json:"reason"`
	DoNotContact bool    `json:"do_not_contact"`
}

func joinErrs(errs []string) string {
	return stringsJoin(errs, "; ")
}

func stringsJoin(parts []string, sep string) string {
	if len(parts) == 0 {
		return ""
	}
	out := parts[0]
	for i := 1; i < len(parts); i++ {
		out += sep + parts[i]
	}
	return out
}

// Ensure service holds optional AI provider.
func (s *service) SetAI(p generation.Provider) {
	s.ai = p
}

func recentDraftBodies(ctx context.Context, s *service, orgID, accountID uuid.UUID, channel string) []string {
	if s == nil || s.repo == nil {
		return nil
	}
	list, err := s.repo.ListDrafts(ctx, orgID, "", 40, 0)
	if err != nil || len(list) == 0 {
		return nil
	}
	var bodies []string
	for _, d := range list {
		if d.AccountID != accountID {
			continue
		}
		if channel != "" && d.Channel != "" && d.Channel != channel {
			continue
		}
		if b := strings.TrimSpace(d.BodyText); b != "" {
			bodies = append(bodies, b)
		}
		if len(bodies) >= 8 {
			break
		}
	}
	return bodies
}

func channelKindFromDraft(d *models.OutreachDraft) string {
	if d == nil {
		return ChannelEmailInitial
	}
	if d.Channel == models.OutreachChannelWhatsApp {
		return ChannelWhatsAppInitial
	}
	return ChannelEmailInitial
}

func maxWordsForDraft(s *service, d *models.OutreachDraft) int {
	if s == nil {
		return DefaultMaxInitialWords
	}
	if d != nil && d.Channel == models.OutreachChannelWhatsApp {
		if s.cfg.MaxWhatsAppWords > 0 {
			return s.cfg.MaxWhatsAppWords
		}
		return DefaultMaxWhatsAppWords
	}
	if s.cfg.MaxInitialEmailWords > 0 {
		return s.cfg.MaxInitialEmailWords
	}
	return DefaultMaxInitialWords
}
