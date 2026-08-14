package confenge

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/warmbly/warmbly/internal/app/whatsapp"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
)

// WhatsAppSender is the subset of whatsapp.Service used by confenge ops.
type WhatsAppSender interface {
	Config() whatsapp.Config
	Send(ctx context.Context, state whatsapp.ContactChannelState, intent whatsapp.SendIntent, textReq *whatsapp.SendTextRequest, tmplReq *whatsapp.SendTemplateRequest) (*whatsapp.SendResultExt, error)
	ProcessInbound(ctx context.Context, state *whatsapp.ContactChannelState, ev whatsapp.ChannelEvent) (whatsapp.InboundResult, error)
	ProviderName() string
}

// WhatsAppStateStore persists channel state + messages (optional; nil = in-process only).
type WhatsAppStateStore interface {
	UpsertContactState(ctx context.Context, st *models.WhatsAppContactState) *errx.Error
	GetContactStateByPhone(ctx context.Context, orgID uuid.UUID, phoneE164 string) (*models.WhatsAppContactState, *errx.Error)
	InsertMessage(ctx context.Context, msg *models.WhatsAppMessage) (bool, *errx.Error)
	GetInstanceByName(ctx context.Context, provider, instance string) (*models.WhatsAppInstance, *errx.Error)
	InsertWebhookEvent(ctx context.Context, orgID uuid.UUID, provider, idemKey, eventType, externalMsgID, payloadHash string) (bool, *errx.Error)
}

// WireWhatsApp attaches the transport service and optional state store.
func (s *service) WireWhatsApp(sender WhatsAppSender, store WhatsAppStateStore) {
	s.wa = sender
	s.waStore = store
}

// DecideChannel runs the multichannel orchestrator for an account/candidate.
func (s *service) DecideChannel(ctx context.Context, orgID, accountID uuid.UUID, contactID *uuid.UUID) (*ChannelDecision, *errx.Error) {
	if xerr := s.requireEnabled(); xerr != nil {
		return nil, xerr
	}
	acc, err := s.repo.GetAccount(ctx, orgID, accountID)
	if err != nil || acc == nil {
		return nil, errx.New(errx.NotFound, "account not found")
	}
	if err := RequireTargetFit(acc); err != nil {
		return nil, errx.New(errx.BadRequest, err.Error())
	}
	var cand *models.OutreachContactCandidate
	if contactID != nil {
		cand, err = s.repo.GetCandidate(ctx, orgID, *contactID)
		if err != nil || cand == nil {
			return nil, errx.New(errx.NotFound, "contact candidate not found")
		}
	} else {
		list, err := s.repo.ListCandidates(ctx, orgID, accountID)
		if err != nil {
			return nil, errx.New(errx.Internal, "list candidates failed")
		}
		cand = pickRecommendedAny(list)
	}
	st := candidateToChannelState(orgID, cand, acc)
	emailOK := cand != nil && cand.Email != "" && cand.CanEnroll()
	phone := ""
	if cand != nil {
		phone = cand.PhoneE164
		if phone == "" {
			phone = cand.Phone
		}
	}
	waOn := s.cfg.WhatsAppEnabled && s.wa != nil && s.wa.Config().Enabled
	intent := whatsapp.SendIntent{
		Mode:                 whatsapp.ModeFreeText,
		Automated:            true,
		FeatureEnabled:       waOn,
		AutoSendEnabled:      s.wa != nil && s.wa.Config().AutoSendEnabled,
		RequireHumanApproval: s.cfg.RequireHumanApproval,
		Now:                  time.Now().UTC(),
		CrossChannelMin:      time.Duration(s.cfg.CrossChannelHours) * time.Hour,
		ServiceWindow:        24 * time.Hour,
	}
	if s.wa != nil {
		intent.ServiceWindow = s.wa.Config().ServiceWindow
		if intent.CrossChannelMin == 0 {
			intent.CrossChannelMin = s.wa.Config().CrossChannelInterval
		}
	}
	d := OrchestrateChannel(waOn, emailOK, phone, st, intent)
	return &d, nil
}

// GenerateWhatsAppDraft creates a short WhatsApp draft for human review.
// Never auto-sends. Public phone without opt-in is blocked.
func (s *service) GenerateWhatsAppDraft(ctx context.Context, orgID, userID, accountID uuid.UUID, contactID *uuid.UUID) (*models.OutreachDraft, *errx.Error) {
	if xerr := s.requireEnabled(); xerr != nil {
		return nil, xerr
	}
	if !s.cfg.WhatsAppEnabled {
		return nil, errx.New(errx.BadRequest, "CONFENGE_WHATSAPP_ENABLED is false")
	}
	acc, err := s.repo.GetAccount(ctx, orgID, accountID)
	if err != nil || acc == nil {
		return nil, errx.New(errx.NotFound, "account not found")
	}
	if err := RequireTargetFit(acc); err != nil {
		return nil, errx.New(errx.BadRequest, err.Error())
	}
	if acc.DoNotContact || acc.Blocked {
		return nil, errx.New(errx.BadRequest, "account is blocked or DO_NOT_CONTACT")
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
		cand = pickRecommendedAny(list)
	}
	if cand == nil {
		return nil, errx.New(errx.BadRequest, "no contact candidate")
	}
	phone := cand.PhoneE164
	if phone == "" {
		norm := whatsapp.NormalizePhone(cand.Phone, "BR")
		if norm.Valid {
			phone = norm.E164
		}
	}
	if phone == "" {
		return nil, errx.New(errx.BadRequest, "candidate has no valid phone")
	}

	st := candidateToChannelState(orgID, cand, acc)
	intent := whatsapp.SendIntent{
		Mode:                 whatsapp.ModeFreeText,
		Automated:            true,
		FeatureEnabled:       true,
		AutoSendEnabled:      false, // draft path never auto-sends
		RequireHumanApproval: true,
		Now:                  time.Now().UTC(),
		CrossChannelMin:      time.Duration(s.cfg.CrossChannelHours) * time.Hour,
	}
	// Policy: must not even generate for blocked consent (still allow MANUAL_REVIEW for opted-in).
	d := whatsapp.EvaluateEligibility(st, intent)
	if d.Eligibility == whatsapp.EligBlocked && (st.ConsentStatus == whatsapp.ConsentUnknown || st.ConsentStatus == whatsapp.ConsentNoOptIn || st.ConsentStatus == whatsapp.ConsentOptedOut || st.DoNotContact) {
		return nil, errx.New(errx.BadRequest, "whatsapp blocked by consent: "+d.Reason)
	}

	evidence, _ := s.repo.ListEvidence(ctx, orgID, accountID)
	recent := recentDraftBodies(ctx, s, orgID, accountID, models.OutreachChannelWhatsApp)
	in := BuildGenerateInput(ChannelWhatsAppInitial, acc, cand, evidence, recent)
	out, provider, model, genErr := s.generator().Generate(ctx, in)
	if genErr != nil {
		out = TemplateDraftChannel(ChannelWhatsAppInitial, acc, cand, evidence)
		provider = "template"
		model = "whatsapp_short"
	}
	if out.ServiceCode == "" {
		out.ServiceCode = acc.ServiceCode
	}
	if out.Channel == "" {
		out.Channel = ChannelWhatsAppInitial
	}
	val := ValidateDraft(&out, acc, cand, ValidateOpts{
		MaxWords: s.cfg.MaxWhatsAppWords, Evidence: evidence, Channel: ChannelWhatsAppInitial,
		RecentBodies: recent, SkipEmailRecipient: true,
	})
	risk, flags := ClassifyRisk(acc, cand, &out, val)
	flags = append(flags, "whatsapp_channel", "requires_human_approval")
	if risk == "GREEN" {
		risk = "YELLOW"
	}
	val.Claims = out.Claims
	val.Rationale = out.Rationale
	val.Channel = out.Channel
	valJSON, _ := json.Marshal(val)
	ok := val.OK
	draft := &models.OutreachDraft{
		OrganizationID: orgID, AccountID: accountID, ContactCandidateID: &cand.ID,
		Channel: models.OutreachChannelWhatsApp, RecipientName: cand.Name, RecipientRole: cand.Role,
		RecipientEmail: cand.Email, RecipientPhoneE164: phone, VerificationStatus: cand.VerificationStatus,
		Subject: "", BodyText: SanitizeText(out.BodyText, 2000), ServiceCode: SanitizeText(out.ServiceCode, 100),
		FactUsed: SanitizeText(out.FactUsed, 2000), EvidenceIDs: collectEvidenceIDs(&out),
		Question: SanitizeText(out.Question, 1000), CTA: SanitizeText(out.CTA, 500),
		Provider: provider, Model: model, PromptVersion: PromptVersion, ValidationJSON: valJSON,
		ValidationOK: &ok, Status: draftStatusFromMessageability(out, val), RiskClass: risk, RiskFlags: flags,
	}
	if err := s.repo.UpsertDraft(ctx, draft); err != nil {
		return nil, errx.New(errx.Internal, "failed to save whatsapp draft: "+err.Error())
	}
	if s.audit != nil {
		s.audit.LogAction(ctx, orgID, userID, models.AuditActionCreate, models.AuditEntityOutreachAccount, &accountID, "", "",
			map[string]string{"draft_id": draft.ID.String(), "channel": "WHATSAPP", "status": draft.Status},
			map[string]string{"phone": phone},
		)
	}
	return draft, nil
}

// SendApprovedWhatsApp sends an APPROVED WhatsApp draft via the provider after policy gate.
func (s *service) SendApprovedWhatsApp(ctx context.Context, orgID, userID, draftID uuid.UUID) (*models.OutreachDraft, *errx.Error) {
	if xerr := s.requireEnabled(); xerr != nil {
		return nil, xerr
	}
	if !s.cfg.SendingAllowed() {
		return nil, errx.New(errx.BadRequest, "sending paused (kill switch); run make confenge-resume-sending when safe")
	}
	if s.wa == nil || !s.cfg.WhatsAppEnabled {
		return nil, errx.New(errx.ServiceUnavailable, "whatsapp transport not configured")
	}
	d, err := s.repo.GetDraft(ctx, orgID, draftID)
	if err != nil || d == nil {
		return nil, errx.New(errx.NotFound, "draft not found")
	}
	// Explicit WHATSAPP channel only — never send an EMAIL draft even if a phone is set.
	if d.Channel != models.OutreachChannelWhatsApp && d.Channel != "WHATSAPP" {
		return nil, errx.New(errx.BadRequest, "draft is not a WhatsApp draft")
	}
	if d.Status != models.OutreachDraftApproved {
		return nil, errx.New(errx.BadRequest, "draft must be APPROVED before WhatsApp send")
	}
	// Fail-closed: same per-touch invariant as email (draft-only APPROVED is not enough).
	tpGate, xerr := s.requireTouchTransport(ctx, orgID, draftID)
	if xerr != nil {
		return nil, xerr
	}
	// Ship only approved touchpoint payload.
	d.BodyText = tpGate.BodyText
	if tpGate.Recipient != "" {
		d.RecipientPhoneE164 = tpGate.Recipient
	}
	acc, err := s.repo.GetAccount(ctx, orgID, d.AccountID)
	if err != nil || acc == nil {
		return nil, errx.New(errx.NotFound, "account not found")
	}
	var cand *models.OutreachContactCandidate
	if d.ContactCandidateID != nil {
		cand, _ = s.repo.GetCandidate(ctx, orgID, *d.ContactCandidateID)
	}
	st := candidateToChannelState(orgID, cand, acc)
	if d.RecipientPhoneE164 != "" {
		st.PhoneE164 = d.RecipientPhoneE164
	}
	// Load live service window from store when present.
	if s.waStore != nil && st.PhoneE164 != "" {
		if live, xerr := s.waStore.GetContactStateByPhone(ctx, orgID, st.PhoneE164); xerr == nil && live != nil {
			st.LastInboundAt = live.LastInboundAt
			st.ServiceWindowUntil = live.ServiceWindowUntil
			st.OptOutAt = live.OptOutAt
			if live.DoNotContact {
				st.DoNotContact = true
			}
			if live.ConsentStatus == whatsapp.ConsentOptedOut || live.ConsentStatus == whatsapp.ConsentDoNotContact {
				st.ConsentStatus = live.ConsentStatus
			}
			if live.LastEmailOutboundAt != nil {
				st.LastEmailOutboundAt = live.LastEmailOutboundAt
			}
			if live.LastWhatsAppOutboundAt != nil {
				st.LastWhatsAppOutboundAt = live.LastWhatsAppOutboundAt
			}
		}
	}

	intent := whatsapp.SendIntent{
		Mode:                 whatsapp.ModeFreeText,
		Automated:            false, // human approved send
		FeatureEnabled:       true,
		AutoSendEnabled:      true, // human already approved; allow transport
		RequireHumanApproval: false,
		Now:                  time.Now().UTC(),
		CrossChannelMin:      time.Duration(s.cfg.CrossChannelHours) * time.Hour,
		ServiceWindow:        s.wa.Config().ServiceWindow,
	}
	// Outside service window without template: block free text.
	elig := whatsapp.EvaluateEligibility(st, intent)
	if !elig.Allowed {
		return nil, errx.New(errx.BadRequest, "whatsapp send blocked: "+elig.Reason)
	}

	// Global CONFENGE dispatch governor (shared email+WA hourly cap). Final gate.
	lease, already, xerr := s.reserveOutbound(ctx, orgID, "WHATSAPP", draftID, st.PhoneE164)
	if xerr != nil {
		return nil, xerr
	}
	if already {
		// Idempotent: successful outbound already recorded for this draft.
		if d.Status != models.OutreachDraftSent {
			d.Status = models.OutreachDraftSent
			_ = s.repo.UpsertDraft(ctx, d)
		}
		return d, nil
	}

	instance := s.wa.Config().EvolutionInstance
	res, err := s.wa.Send(ctx, st, intent, &whatsapp.SendTextRequest{
		Instance:       instance,
		ToE164:         st.PhoneE164,
		Body:           d.BodyText,
		IdempotencyKey: "wa-draft:" + draftID.String(),
		OrganizationID: orgID,
	}, nil)
	if err != nil {
		s.releaseOutbound(ctx, lease, err.Error())
		return nil, errx.New(errx.Internal, "whatsapp send failed: "+err.Error())
	}
	if res == nil || res.Skipped {
		reason := "skipped"
		if res != nil {
			reason = res.Decision.Reason
		}
		s.releaseOutbound(ctx, lease, reason)
		return nil, errx.New(errx.BadRequest, "whatsapp send skipped: "+reason)
	}

	// Successful outbound: consume success slot (idempotent on message_key).
	s.commitOutbound(ctx, lease)

	now := time.Now().UTC()
	d.Status = models.OutreachDraftSent
	if err := s.repo.UpsertDraft(ctx, d); err != nil {
		return nil, errx.New(errx.Internal, "failed to mark draft sent")
	}
	_ = s.repo.SetAccountHumanFlags(ctx, orgID, d.AccountID, acc.Blocked, acc.DoNotContact, acc.BlockReason, models.OutreachQueueSent)

	// Persist outbound message + state.
	if s.waStore != nil {
		msg := &models.WhatsAppMessage{
			OrganizationID:    orgID,
			ThreadKey:         st.PhoneE164,
			Direction:         "outbound",
			Channel:           whatsapp.ChannelWhatsApp,
			Provider:          s.wa.ProviderName(),
			ProviderMessageID: "",
			IdempotencyKey:    "wa-draft-send:" + draftID.String(),
			BodyText:          d.BodyText,
			Status:            "sent",
			DraftID:           &d.ID,
			CampaignID:        d.CampaignID,
			ReviewedBy:        &userID,
			ApprovedAt:        d.ApprovedAt,
			SentAt:            &now,
			OccurredAt:        now,
		}
		if res.Result != nil {
			msg.ProviderMessageID = res.Result.ProviderMessageID
		}
		if cand != nil && cand.WarmblyContactID != nil {
			msg.ContactID = cand.WarmblyContactID
		}
		_, _ = s.waStore.InsertMessage(ctx, msg)

		persist := models.WhatsAppContactState{
			OrganizationID:         orgID,
			PhoneE164:              st.PhoneE164,
			PhoneRaw:               st.PhoneE164,
			PhoneValid:             true,
			ConsentStatus:          st.ConsentStatus,
			ConsentSource:          st.ConsentSource,
			ConsentAt:              st.ConsentAt,
			ConsentProvenanceOK:    st.ConsentProvenanceOK,
			LastWhatsAppOutboundAt: &now,
			ContactID:              nil,
		}
		if cand != nil && cand.WarmblyContactID != nil {
			persist.ContactID = cand.WarmblyContactID
		}
		_ = s.waStore.UpsertContactState(ctx, &persist)
	}

	_ = s.EnqueueOutcome(ctx, orgID, models.OutreachOutcome{
		IdempotencyKey: fmt.Sprintf("wa-sent:%s:%s", orgID, draftID),
		SourceLeadID:   acc.SourceLeadID,
		CNPJ14:         acc.CNPJ14,
		ContactEmail:   d.RecipientEmail,
		EventType:      OutcomeContacted,
		OccurredAt:     now,
		Payload:        mustJSON(map[string]any{"channel": "WHATSAPP", "draft_id": draftID.String()}),
	})

	if s.audit != nil {
		s.audit.LogAction(ctx, orgID, userID, models.AuditActionUpdate, models.AuditEntityOutreachAccount, &d.AccountID, "", "",
			map[string]string{"draft_id": d.ID.String(), "status": d.Status, "channel": "WHATSAPP"},
			nil,
		)
	}
	return d, nil
}

// HandleWhatsAppInbound is the production path for Evolution-normalized inbound events.
// It stops follow-ups, records CRM/outcomes, and applies sticky opt-out.
func (s *service) HandleWhatsAppInbound(ctx context.Context, orgID uuid.UUID, ev whatsapp.ChannelEvent) (whatsapp.InboundResult, error) {
	empty := whatsapp.InboundResult{Event: ev}
	if s == nil || !s.cfg.Enabled {
		return empty, nil
	}
	if s.wa == nil {
		return empty, fmt.Errorf("whatsapp service not wired")
	}

	phone := ev.FromE164
	if phone == "" {
		phone = ev.ToE164
	}
	st := whatsapp.ContactChannelState{
		OrganizationID: orgID,
		PhoneE164:      phone,
		ConsentStatus:  whatsapp.ConsentUnknown,
	}
	// Resolve candidate by phone or e164.
	var cand *models.OutreachContactCandidate
	var acc *models.OutreachAccount
	if phone != "" {
		c, a, err := s.repo.FindCandidateByPhone(ctx, orgID, phone)
		if err == nil {
			cand, acc = c, a
		}
	}
	if cand != nil {
		st = candidateToChannelState(orgID, cand, acc)
	}

	// Overlay persisted channel state.
	if s.waStore != nil && phone != "" {
		if live, xerr := s.waStore.GetContactStateByPhone(ctx, orgID, phone); xerr == nil && live != nil {
			if live.ConsentStatus != "" {
				st.ConsentStatus = live.ConsentStatus
			}
			st.ConsentProvenanceOK = live.ConsentProvenanceOK
			st.OptOutAt = live.OptOutAt
			st.DoNotContact = live.DoNotContact || st.DoNotContact
			st.LastInboundAt = live.LastInboundAt
			st.ServiceWindowUntil = live.ServiceWindowUntil
			if live.ContactID != nil {
				st.ContactID = *live.ContactID
			}
		}
	}

	res, err := s.wa.ProcessInbound(ctx, &st, ev)
	if err != nil {
		return res, err
	}
	if res.Duplicate || res.Ignored {
		return res, nil
	}

	// Persist message + state.
	if s.waStore != nil && ev.EventType == whatsapp.EventMessageReceived {
		msg := &models.WhatsAppMessage{
			OrganizationID:    orgID,
			ThreadKey:         phone,
			Direction:         "inbound",
			Channel:           whatsapp.ChannelWhatsApp,
			Provider:          whatsapp.ProviderEvolution,
			ProviderMessageID: ev.ExternalMessageID,
			IdempotencyKey:    ev.IdempotencyKey(),
			BodyText:          ev.Content.Text,
			Status:            "received",
			OccurredAt:        ev.OccurredAt,
		}
		if cand != nil && cand.WarmblyContactID != nil {
			msg.ContactID = cand.WarmblyContactID
		}
		_, _ = s.waStore.InsertMessage(ctx, msg)

		persist := models.WhatsAppContactState{
			OrganizationID:      orgID,
			PhoneE164:           st.PhoneE164,
			PhoneValid:          true,
			ConsentStatus:       st.ConsentStatus,
			ConsentSource:       st.ConsentSource,
			ConsentAt:           st.ConsentAt,
			ConsentProvenanceOK: st.ConsentProvenanceOK,
			LastInboundAt:       st.LastInboundAt,
			ServiceWindowUntil:  st.ServiceWindowUntil,
			OptOutAt:            st.OptOutAt,
			DoNotContact:        st.DoNotContact,
		}
		if cand != nil && cand.WarmblyContactID != nil {
			persist.ContactID = cand.WarmblyContactID
		}
		_ = s.waStore.UpsertContactState(ctx, &persist)
	}

	// Stop follow-ups / mark replied on confenge account + drafts.
	if res.StopSequences && acc != nil {
		term, stop := models.TouchpointReplied, "REPLY"
		if res.OptOut.Matched && res.OptOut.Confident {
			term, stop = models.TouchpointDNC, "DNC"
		}
		_, _ = s.repo.CancelOpenTouchpoints(ctx, orgID, acc.ID, term, stop)
		_ = s.repo.SetAccountHumanFlags(ctx, orgID, acc.ID, acc.Blocked, acc.DoNotContact || st.DoNotContact, acc.BlockReason, models.OutreachQueueReplied)
		// Mark WhatsApp drafts replied when possible.
		if drafts, err := s.repo.ListDrafts(ctx, orgID, models.OutreachDraftEnrolled, 50, 0); err == nil {
			for i := range drafts {
				dd := drafts[i]
				if dd.AccountID == acc.ID && (dd.Channel == models.OutreachChannelWhatsApp || dd.Channel == "WHATSAPP" || dd.RecipientPhoneE164 != "") {
					dd.Status = models.OutreachDraftReplied
					_ = s.repo.UpsertDraft(ctx, &dd)
				}
			}
		}
		for _, stt := range []string{models.OutreachDraftSent, models.OutreachDraftApproved} {
			if drafts, err := s.repo.ListDrafts(ctx, orgID, stt, 50, 0); err == nil {
				for i := range drafts {
					dd := drafts[i]
					if dd.AccountID == acc.ID {
						dd.Status = models.OutreachDraftReplied
						_ = s.repo.UpsertDraft(ctx, &dd)
					}
				}
			}
		}
	}

	// Unified handoff: cancel governor queue, classify commercial intent, sticky DNC, outcomes + CRM.
	email := ""
	if cand != nil {
		email = cand.Email
	}
	if res.StopSequences || (res.OptOut.Matched && res.OptOut.Confident) {
		preClass := ""
		if res.OptOut.Matched && res.OptOut.Confident {
			preClass = "do_not_contact"
			s.cancelQueuedForRecipient(ctx, orgID, email, phone, "whatsapp_opt_out")
			if cand != nil {
				cand.DoNotContact = true
				cand.WhatsAppConsentStatus = whatsapp.ConsentOptedOut
				now := ev.OccurredAt
				cand.WhatsAppConsentAt = &now
				_, _ = s.repo.UpsertCandidate(ctx, cand)
			}
		} else {
			s.cancelQueuedForRecipient(ctx, orgID, email, phone, "reply")
		}
		var warmblyID *uuid.UUID
		if cand != nil {
			warmblyID = cand.WarmblyContactID
		}
		var accID uuid.UUID
		if acc != nil {
			accID = acc.ID
		}
		_, _ = s.ProcessInboundHandoff(ctx, orgID, InboundHandoff{
			Channel:           models.OutreachChannelWhatsApp,
			ContactEmail:      email,
			ContactPhone:      phone,
			BodyText:          ev.Content.Text,
			PreClass:          preClass,
			IdempotencyKey:    fmt.Sprintf("wa:%s:%s", orgID, ev.IdempotencyKey()),
			ExternalMessageID: ev.ExternalMessageID,
			OccurredAt:        ev.OccurredAt,
			WarmblyContactID:  warmblyID,
			AccountID:         accID,
		})
	}

	return res, nil
}

func candidateToChannelState(orgID uuid.UUID, cand *models.OutreachContactCandidate, acc *models.OutreachAccount) whatsapp.ContactChannelState {
	st := whatsapp.ContactChannelState{
		OrganizationID: orgID,
		ConsentStatus:  whatsapp.ConsentUnknown,
	}
	if acc != nil && acc.DoNotContact {
		st.DoNotContact = true
		st.ConsentStatus = whatsapp.ConsentDoNotContact
	}
	if cand == nil {
		return st
	}
	if cand.WarmblyContactID != nil {
		st.ContactID = *cand.WarmblyContactID
	}
	st.PhoneE164 = cand.PhoneE164
	if st.PhoneE164 == "" && cand.Phone != "" {
		n := whatsapp.NormalizePhone(cand.Phone, "BR")
		if n.Valid {
			st.PhoneE164 = n.E164
		}
	}
	st.PhoneSource = cand.PhoneSource
	st.PhoneSourceURL = cand.PhoneSourceURL
	if cand.WhatsAppConsentStatus != "" {
		st.ConsentStatus = cand.WhatsAppConsentStatus
	}
	st.ConsentSource = cand.WhatsAppConsentSource
	st.ConsentAt = cand.WhatsAppConsentAt
	st.ConsentProvenanceOK = cand.WhatsAppConsentProvenanceOK
	if cand.DoNotContact {
		st.DoNotContact = true
		st.ConsentStatus = whatsapp.ConsentDoNotContact
	}
	return st
}

// pickRecommendedAny prefers enrollable email contacts but falls back to any non-DNC with phone.
func pickRecommendedAny(list []models.OutreachContactCandidate) *models.OutreachContactCandidate {
	if c := pickRecommended(list); c != nil {
		return c
	}
	var fallback *models.OutreachContactCandidate
	for i := range list {
		c := &list[i]
		if c.DoNotContact || c.Bounced || c.Blocked {
			continue
		}
		if c.Phone == "" && c.PhoneE164 == "" {
			continue
		}
		if c.Recommended {
			return c
		}
		if fallback == nil {
			fallback = c
		}
	}
	return fallback
}

// Ensure compile-time use of strings in this package for truncate helpers.
var _ = strings.TrimSpace
