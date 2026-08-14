package confenge

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
)

type InboundHandoff struct {
	Channel, ContactEmail, ContactPhone, Subject, BodyText string
	Headers                                                map[string][]string
	PreClass                                               string
	PreConfidence                                          float64
	PreSource, IdempotencyKey, ExternalMessageID           string
	OccurredAt                                             time.Time
	WarmblyContactID                                       *uuid.UUID
	ActorID, AccountID                                     uuid.UUID
}

type HandoffResult struct {
	Duplicate, NotConfenge, StoppedCadence bool
	AccountID                              uuid.UUID
	QueueState, IdempotencyKey             string
	Intent                                 CommercialIntent
	CancelledDrafts                        int
}

func (s *service) ProcessInboundHandoff(ctx context.Context, orgID uuid.UUID, in InboundHandoff) (*HandoffResult, *errx.Error) {
	if xerr := s.requireEnabled(); xerr != nil {
		return &HandoffResult{NotConfenge: true}, nil
	}
	if in.OccurredAt.IsZero() {
		in.OccurredAt = time.Now().UTC()
	}
	channel := strings.ToUpper(strings.TrimSpace(in.Channel))
	if channel == "" {
		channel = models.OutreachChannelEmail
	}
	email := strings.TrimSpace(strings.ToLower(in.ContactEmail))
	phone := strings.TrimSpace(in.ContactPhone)
	idem := strings.TrimSpace(in.IdempotencyKey)
	if idem == "" {
		if in.ExternalMessageID != "" {
			idem = fmt.Sprintf("handoff:%s:%s:%s", channel, orgID, in.ExternalMessageID)
		} else {
			idem = fmt.Sprintf("handoff:%s:%s:%s:%d", channel, email, phone, in.OccurredAt.UTC().Truncate(time.Minute).Unix())
		}
	} else if !strings.HasPrefix(idem, "handoff:") {
		idem = "handoff:" + idem
	}
	if existing, err := s.repo.GetOutcomeByIdempotency(ctx, orgID, idem); err == nil && existing != nil {
		return &HandoffResult{Duplicate: true, Intent: ClassifyCommercialIntent(in.Subject, in.BodyText, in.PreClass, in.Headers), IdempotencyKey: idem, AccountID: in.AccountID}, nil
	}
	var cand *models.OutreachContactCandidate
	var acc *models.OutreachAccount
	var err error
	if in.AccountID != uuid.Nil {
		acc, err = s.repo.GetAccount(ctx, orgID, in.AccountID)
		if err != nil {
			return nil, errx.New(errx.Internal, "account lookup: "+err.Error())
		}
	}
	if acc == nil && email != "" {
		cand, acc, err = s.repo.FindCandidateByEmail(ctx, orgID, email)
		if err != nil {
			return nil, errx.New(errx.Internal, "email lookup: "+err.Error())
		}
	}
	if acc == nil && phone != "" {
		cand, acc, err = s.repo.FindCandidateByPhone(ctx, orgID, phone)
		if err != nil {
			return nil, errx.New(errx.Internal, "phone lookup: "+err.Error())
		}
	}
	if acc == nil {
		return &HandoffResult{NotConfenge: true, Intent: ClassifyCommercialIntent(in.Subject, in.BodyText, in.PreClass, in.Headers), IdempotencyKey: idem}, nil
	}
	intent := ClassifyCommercialIntent(in.Subject, in.BodyText, in.PreClass, in.Headers)
	if intent.Intent == IntentUnknown && looksLikeOOO(in.Subject, in.BodyText) {
		ooo := ExtractOOODate(in.Subject + "\n" + in.BodyText)
		intent = CommercialIntent{Intent: IntentOutOfOffice, Confidence: 0.8, Source: "lexicon", OOOReturnDate: ooo, SuggestedAction: SuggestNextAction(IntentOutOfOffice, ooo, "")}
	}
	cancelled := s.cancelPendingOutbound(ctx, orgID, acc.ID)
	queueState, blocked, dnc := queueStateForIntent(intent.Intent, acc)
	reason := fmt.Sprintf("reply:%s:%s", channel, intent.Intent)
	// Pause/cancel open touchpoint cadence (dominant with draft stop).
	term, stop := models.TouchpointReplied, "REPLY"
	if intent.Intent == IntentDoNotContact {
		reason = "reply:DO_NOT_CONTACT"
		term, stop = models.TouchpointDNC, "DNC"
		if cand != nil {
			cand.DoNotContact = true
			cand.VerificationStatus = models.OutreachVerifyDoNotContact
			_, _ = s.repo.UpsertCandidate(ctx, cand)
		}
	}
	if nTP, err := s.repo.CancelOpenTouchpoints(ctx, orgID, acc.ID, term, stop); err == nil {
		cancelled += nTP
	}
	// Drop governor-queued outbound for this recipient (email + phone).
	phoneRef := phone
	if phoneRef == "" && cand != nil {
		phoneRef = cand.PhoneE164
		if phoneRef == "" {
			phoneRef = cand.Phone
		}
	}
	s.cancelQueuedForRecipient(ctx, orgID, email, phoneRef, stop)
	if intent.Intent == IntentOutOfOffice && queueState == "" {
		queueState = acc.QueueState
		if queueState == "" {
			queueState = models.OutreachQueueSent
		}
	}
	_ = s.repo.SetAccountHumanFlags(ctx, orgID, acc.ID, blocked, dnc, reason, queueState)
	if fresh, gerr := s.repo.GetAccount(ctx, orgID, acc.ID); gerr == nil && fresh != nil {
		fresh.CommercialState = intent.Intent
		fresh.QueueState = queueState
		fresh.Blocked = blocked
		fresh.DoNotContact = dnc
		fresh.BlockReason = reason
		_, _ = s.repo.UpsertAccount(ctx, fresh)
		acc = fresh
	}
	contactEmail := email
	if contactEmail == "" && cand != nil {
		contactEmail = cand.Email
	}
	payload := map[string]any{"channel": channel, "intent": intent.Intent, "confidence": intent.Confidence, "source": intent.Source, "suggested_action": intent.SuggestedAction, "subject": truncateRunes(in.Subject, 300), "snippet": truncateRunes(in.BodyText, 1000), "referral_hint": intent.ReferralHint, "external_message_id": in.ExternalMessageID, "cancelled_drafts": cancelled, "stopped_cadence": true}
	if intent.OOOReturnDate != nil {
		payload["ooo_return_date"] = intent.OOOReturnDate.UTC().Format("2006-01-02")
	}
	eventType := OutcomeReplied
	if intent.Intent == IntentDoNotContact {
		eventType = OutcomeDoNotContact
	}
	_ = s.EnqueueOutcome(ctx, orgID, models.OutreachOutcome{IdempotencyKey: idem, SourceLeadID: acc.SourceLeadID, CNPJ14: acc.CNPJ14, ContactEmail: contactEmail, EventType: eventType, OccurredAt: in.OccurredAt, Payload: mustJSON(payload)})
	s.applyReplyCRM(ctx, orgID, in.ActorID, contactEmail, intentToCRMClass(intent.Intent), in.WarmblyContactID, cand, acc)
	s.markAccountDraftsReplied(ctx, orgID, acc.ID)
	return &HandoffResult{AccountID: acc.ID, QueueState: queueState, Intent: intent, CancelledDrafts: cancelled, StoppedCadence: true, IdempotencyKey: idem}, nil
}

func queueStateForIntent(intent string, acc *models.OutreachAccount) (string, bool, bool) {
	blocked := acc != nil && acc.Blocked
	dnc := acc != nil && acc.DoNotContact
	// Sticky DNC: never reopen cadence or clear DO_NOT_CONTACT from a later reply.
	if dnc || intent == IntentDoNotContact {
		return models.OutreachQueueDoNotContact, true, true
	}
	switch intent {
	case IntentOutOfOffice:
		return "", blocked, false
	default:
		return models.OutreachQueueReplied, blocked, false
	}
}

func intentToCRMClass(intent string) string {
	switch intent {
	case IntentPositiveInterest:
		return "positive_interest"
	case IntentReferral:
		return "referral"
	case IntentQuestion:
		return "question"
	case IntentObjection:
		return "objection"
	case IntentNotNow:
		return "not_now"
	case IntentNegative:
		return "no_interest"
	case IntentDoNotContact:
		return "do_not_contact"
	case IntentOutOfOffice:
		return "ooo"
	default:
		return "unknown"
	}
}

func looksLikeOOO(subject, body string) bool {
	t := strings.ToLower(subject + " " + body)
	for _, m := range []string{"out of office", "automatic reply", "auto-reply", "autoreply", "fora do escritorio", "fora do escritório", "estou de ferias", "estou de férias", "vacation", "away from office", "ooo:"} {
		if strings.Contains(t, m) {
			return true
		}
	}
	return false
}

func (s *service) cancelPendingOutbound(ctx context.Context, orgID, accountID uuid.UUID) int {
	n := 0
	for _, st := range []string{models.OutreachDraftNeedsReview, models.OutreachDraftApproved, models.OutreachDraftEnrolled, models.OutreachDraftGenerating} {
		list, err := s.repo.ListDrafts(ctx, orgID, st, 100, 0)
		if err != nil {
			continue
		}
		for i := range list {
			d := list[i]
			if d.AccountID != accountID {
				continue
			}
			d.Status = models.OutreachDraftSkipped
			d.RiskFlags = appendUniqueFlag(d.RiskFlags, "stopped_on_reply")
			if err := s.repo.UpsertDraft(ctx, &d); err == nil {
				n++
			}
		}
	}
	return n
}

func (s *service) markAccountDraftsReplied(ctx context.Context, orgID, accountID uuid.UUID) {
	list, err := s.repo.ListDrafts(ctx, orgID, models.OutreachDraftSent, 100, 0)
	if err != nil {
		return
	}
	for i := range list {
		if list[i].AccountID != accountID {
			continue
		}
		d := list[i]
		d.Status = models.OutreachDraftReplied
		_ = s.repo.UpsertDraft(ctx, &d)
	}
}

func appendUniqueFlag(flags []string, f string) []string {
	for _, x := range flags {
		if x == f {
			return flags
		}
	}
	return append(flags, f)
}

func NeverAutoReopenCadence() bool { return true }

func (s *service) ResumeAtDate(ctx context.Context, orgID, userID, accountID uuid.UUID, resumeAt time.Time, note string) (*models.OutreachAccount, *errx.Error) {
	if xerr := s.requireEnabled(); xerr != nil {
		return nil, xerr
	}
	if resumeAt.IsZero() || resumeAt.Before(time.Now().UTC().Add(-time.Hour)) {
		return nil, errx.New(errx.BadRequest, "resume_at must be a clear future (or near-future) date")
	}
	acc, err := s.repo.GetAccount(ctx, orgID, accountID)
	if err != nil || acc == nil {
		return nil, errx.New(errx.NotFound, "account not found")
	}
	if acc.DoNotContact {
		return nil, errx.New(errx.BadRequest, "account is DO_NOT_CONTACT; cannot schedule resume")
	}
	acc.CommercialState = IntentNotNow
	acc.BlockReason = fmt.Sprintf("resume_at:%s", resumeAt.UTC().Format("2006-01-02"))
	acc.HumanOverride = true
	// Explicit future touch stays approval-gated: account enters NEEDS_REVIEW with a draft.
	if acc.QueueState != models.OutreachQueueDoNotContact {
		acc.QueueState = models.OutreachQueueNeedsReview
	}
	if _, err := s.repo.UpsertAccount(ctx, acc); err != nil {
		return nil, errx.New(errx.Internal, "failed to update account")
	}

	// Build an explicit future touchpoint draft (never auto-send / auto-reopen cadence).
	var cand *models.OutreachContactCandidate
	if list, lerr := s.repo.ListCandidates(ctx, orgID, accountID); lerr == nil {
		cand = pickRecommendedAny(list)
		if cand == nil && len(list) > 0 {
			cand = &list[0]
		}
	}
	dateStr := resumeAt.UTC().Format("2006-01-02")
	subject := "Retomar contato em " + dateStr
	body := "Toque futuro agendado para " + dateStr + ".\n\n"
	body += "Este rascunho exige aprovacao humana antes de qualquer envio. A cadencia automatica NAO foi reaberta.\n"
	if strings.TrimSpace(note) != "" {
		body += "\nNota do operador: " + SanitizeText(note, 500) + "\n"
	}
	if acc.FactToMention != "" {
		body += "\nFato do dossie: " + SanitizeText(acc.FactToMention, 500) + "\n"
	}
	draft := &models.OutreachDraft{
		OrganizationID: orgID, AccountID: accountID,
		Channel: models.OutreachChannelEmail,
		Subject: subject, BodyText: body,
		ServiceCode: acc.ServiceCode, FactUsed: SanitizeText(acc.FactToMention, 2000),
		Provider: "template", Model: "resume_at", PromptVersion: PromptVersion + ".resume",
		StrategyCode: "RESUME_AT:" + dateStr,
		Status:       models.OutreachDraftNeedsReview,
		RiskClass:    "YELLOW",
		RiskFlags:    []string{"resume_scheduled", "requires_human_approval", "never_auto_send", "no_auto_reopen"},
	}
	ok := true
	draft.ValidationOK = &ok
	if cand != nil {
		draft.ContactCandidateID = &cand.ID
		draft.RecipientName = cand.Name
		draft.RecipientRole = cand.Role
		draft.RecipientEmail = cand.Email
		draft.RecipientPhoneE164 = firstNonEmpty(cand.PhoneE164, cand.Phone)
		draft.VerificationStatus = cand.VerificationStatus
	}
	_ = s.repo.UpsertDraft(ctx, draft)

	_ = s.EnqueueOutcome(ctx, orgID, models.OutreachOutcome{
		IdempotencyKey: fmt.Sprintf("resume:%s:%s:%s", orgID, accountID, dateStr),
		SourceLeadID:   acc.SourceLeadID, CNPJ14: acc.CNPJ14, EventType: OutcomeReplied,
		OccurredAt: time.Now().UTC(),
		Payload: mustJSON(map[string]any{
			"action": "resume_at", "resume_at": resumeAt.UTC().Format(time.RFC3339),
			"note": truncateRunes(note, 500), "requires": "human_approval", "auto_reopen": false,
			"draft_id": draft.ID.String(), "explicit_touchpoint": true,
		}),
	})
	if s.audit != nil {
		s.audit.LogAction(ctx, orgID, userID, models.AuditActionUpdate, models.AuditEntityOutreachAccount, &accountID, "", "",
			map[string]string{"action": "resume_at", "resume_at": dateStr, "draft_id": draft.ID.String()},
			map[string]string{"note": truncateRunes(note, 200)},
		)
	}
	return acc, nil
}

func (s *service) ChangeReferralRecipient(ctx context.Context, orgID, userID, accountID uuid.UUID, name, email, role, phone string) (*models.OutreachContactCandidate, *errx.Error) {
	if xerr := s.requireEnabled(); xerr != nil {
		return nil, xerr
	}
	acc, err := s.repo.GetAccount(ctx, orgID, accountID)
	if err != nil || acc == nil {
		return nil, errx.New(errx.NotFound, "account not found")
	}
	if acc.DoNotContact {
		return nil, errx.New(errx.BadRequest, "account is DO_NOT_CONTACT")
	}
	email = strings.TrimSpace(strings.ToLower(email))
	if email == "" && strings.TrimSpace(phone) == "" {
		return nil, errx.New(errx.BadRequest, "referral requires email or phone")
	}
	cand := &models.OutreachContactCandidate{OrganizationID: orgID, AccountID: accountID, SourceContactID: fmt.Sprintf("referral:%s", uuid.New().String()), Name: SanitizeText(name, 200), Role: SanitizeText(role, 120), Email: email, Phone: strings.TrimSpace(phone), VerificationStatus: models.OutreachVerifyCandidateUnverified, Confidence: "LOW", Recommended: true}
	if _, err := s.repo.UpsertCandidate(ctx, cand); err != nil {
		return nil, errx.New(errx.Internal, "failed to save referral contact")
	}
	if list, lerr := s.repo.ListCandidates(ctx, orgID, accountID); lerr == nil {
		for i := range list {
			if list[i].ID == cand.ID {
				continue
			}
			if list[i].Recommended {
				c := list[i]
				c.Recommended = false
				_, _ = s.repo.UpsertCandidate(ctx, &c)
			}
		}
	}
	acc.CommercialState = IntentReferral
	acc.QueueState = models.OutreachQueueNeedsContact
	acc.HumanOverride = true
	_, _ = s.repo.UpsertAccount(ctx, acc)
	_ = s.EnqueueOutcome(ctx, orgID, models.OutreachOutcome{IdempotencyKey: fmt.Sprintf("referral:%s:%s:%s", orgID, accountID, cand.ID), SourceLeadID: acc.SourceLeadID, CNPJ14: acc.CNPJ14, ContactEmail: email, EventType: OutcomeReplied, OccurredAt: time.Now().UTC(), Payload: mustJSON(map[string]any{"action": "referral_recipient", "candidate_id": cand.ID.String(), "name": cand.Name, "email": email, "timeline_kept": true})})
	if s.audit != nil {
		s.audit.LogAction(ctx, orgID, userID, models.AuditActionUpdate, models.AuditEntityOutreachAccount, &accountID, "", "", map[string]string{"action": "referral_recipient", "candidate_id": cand.ID.String()}, map[string]string{"email": email, "name": cand.Name})
	}
	return cand, nil
}
