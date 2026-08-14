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

// Default campaign name for CONFENGE bootstrap (idempotent).
const DefaultCampaignName = "CONFENGE | Outreach consultivo inicial"

// CampaignAPI is the slice of campaign service used for bootstrap.
type CampaignAPI interface {
	Create(ctx context.Context, userID string, orgID *uuid.UUID, data *models.CreateCampaign) (*models.Campaign, *errx.Error)
	Get(ctx context.Context, orgID, id string) (*models.Campaign, *errx.Error)
}

// ContactAPI is the slice of contact service used for enrollment.
type ContactAPI interface {
	Add(ctx context.Context, userID string, orgID uuid.UUID, contacts []models.AddContact) ([]models.Contact, *errx.Error)
}

// WireExecution attaches campaign/contact execution-plane services.
func (s *service) WireExecution(campaigns CampaignAPI, contacts ContactAPI) {
	s.campaigns = campaigns
	s.contacts = contacts
}

// BootstrapCampaign finds or creates the CONFENGE campaign for the org.
// Idempotent: re-runs return the same campaign_id stored in outreach_org_settings.
func (s *service) BootstrapCampaign(ctx context.Context, orgID, userID uuid.UUID) (*models.Campaign, *errx.Error) {
	if xerr := s.requireEnabled(); xerr != nil {
		return nil, xerr
	}
	if s.campaigns == nil {
		return nil, errx.New(errx.ServiceUnavailable, "campaign service not wired")
	}

	settings, err := s.repo.GetOrgSettings(ctx, orgID)
	if err != nil {
		return nil, errx.New(errx.Internal, "failed to load org settings")
	}
	if settings != nil && settings.CampaignID != nil {
		c, xerr := s.campaigns.Get(ctx, orgID.String(), settings.CampaignID.String())
		if xerr == nil && c != nil {
			return c, nil
		}
		// Campaign was deleted; fall through to recreate.
	}

	tz := "America/Sao_Paulo"
	limit := s.cfg.DefaultDailyLimit
	if limit < 1 {
		limit = DefaultCampaignDailyLimit
	}
	stop := true
	openTrack := false
	linkTrack := false
	unsub := true
	textOnly := true
	start := "09:00"
	end := "17:00"
	// Mon-Fri bitmask: leave Days nil so repository uses DefaultDays().
	steps := defaultCadenceSteps()
	desc := "CONFENGE consultive outreach. Human-approved enrollments only. Stop on reply/bounce."

	created, xerr := s.campaigns.Create(ctx, userID.String(), &orgID, &models.CreateCampaign{
		Name:              DefaultCampaignName,
		Description:       desc,
		StopOnReply:       &stop,
		OpenTracking:      &openTrack,
		LinkTracking:      &linkTrack,
		UnsubscribeHeader: &unsub,
		TextOnly:          &textOnly,
		DailyLimit:        &limit,
		Timezone:          &tz,
		StartTime:         &start,
		EndTime:           &end,
		Sequences:         steps,
	})
	if xerr != nil {
		return nil, xerr
	}

	st := &models.OutreachOrgSettings{
		OrganizationID: orgID,
		CampaignID:     &created.ID,
		CampaignName:   DefaultCampaignName,
	}
	if err := s.repo.UpsertOrgSettings(ctx, st); err != nil {
		return nil, errx.New(errx.Internal, "failed to persist campaign pointer")
	}
	if s.audit != nil {
		s.audit.LogAction(ctx, orgID, userID, models.AuditActionCreate, models.AuditEntityCampaign, &created.ID, "", "",
			map[string]string{"name": DefaultCampaignName, "source": "confenge_bootstrap"},
			nil,
		)
	}
	return created, nil
}

// defaultCadenceSteps is a single step for the Warmbly campaign shell.
// Body/subject come from contact custom fields written at enroll time
// (approved touchpoint payload). Follow-ups authority remains outreach_touchpoints.
func defaultCadenceSteps() []models.CreateSequenceInput {
	return []models.CreateSequenceInput{{
		Name:      "CONFENGE touch (policy or human approved)",
		Subject:   "{{.confenge_subject}}",
		BodyPlain: "{{.confenge_body}}",
		BodyHTML:  "{{.confenge_body_html}}",
		WaitAfter: nil,
	}}
}

// EnrollDraft promotes an APPROVED, validated draft into a Warmbly contact
// and campaign lead. Never enrolls unverified / DNC / bounced addresses.
func (s *service) EnrollDraft(ctx context.Context, orgID, userID, draftID uuid.UUID) (*models.OutreachDraft, *errx.Error) {
	if xerr := s.requireEnabled(); xerr != nil {
		return nil, xerr
	}
	if s.cfg.AutoSendEnabled && !s.cfg.RequireHumanApproval {
		return nil, errx.New(errx.BadRequest, "refusing enroll: auto-send without human approval is not allowed")
	}
	if !s.cfg.SendingAllowed() {
		return nil, errx.New(errx.BadRequest, "sending paused (kill switch); run make confenge-resume-sending when safe")
	}
	if s.contacts == nil || s.campaigns == nil {
		return nil, errx.New(errx.ServiceUnavailable, "execution services not wired")
	}

	d, err := s.repo.GetDraft(ctx, orgID, draftID)
	if err != nil || d == nil {
		return nil, errx.New(errx.NotFound, "draft not found")
	}
	if d.Status == models.OutreachDraftEnrolled {
		return d, nil // idempotent
	}
	if d.Status != models.OutreachDraftApproved {
		return nil, errx.New(errx.BadRequest, "draft must be APPROVED before enrollment")
	}
	// Fail-closed: every CONFENGE email outbound needs a transport-valid touchpoint.
	tpGate, xerr := s.requireTouchTransport(ctx, orgID, draftID)
	if xerr != nil {
		return nil, xerr
	}
	// Ship only the approved touchpoint payload (ignore diverged draft fields).
	d.Subject = tpGate.Subject
	d.BodyText = tpGate.BodyText
	if tpGate.Recipient != "" {
		d.RecipientEmail = tpGate.Recipient
	}

	acc, err := s.repo.GetAccount(ctx, orgID, d.AccountID)
	if err != nil || acc == nil {
		return nil, errx.New(errx.NotFound, "account not found")
	}
	if acc.DoNotContact || acc.Blocked {
		return nil, errx.New(errx.BadRequest, "account is blocked or DO_NOT_CONTACT")
	}

	var cand *models.OutreachContactCandidate
	if d.ContactCandidateID != nil {
		cand, err = s.repo.GetCandidate(ctx, orgID, *d.ContactCandidateID)
		if err != nil || cand == nil {
			return nil, errx.New(errx.NotFound, "contact candidate not found")
		}
	}
	if cand == nil || !cand.CanEnroll() {
		// Re-resolve by draft recipient email (human may have edited recipient
		// after plan bound a phone-only or unverified candidate).
		email := strings.TrimSpace(strings.ToLower(d.RecipientEmail))
		if email != "" {
			if list, lerr := s.repo.ListCandidates(ctx, orgID, d.AccountID); lerr == nil {
				for i := range list {
					c := &list[i]
					if strings.EqualFold(strings.TrimSpace(c.Email), email) && c.CanEnroll() {
						cand = c
						id := c.ID
						d.ContactCandidateID = &id
						break
					}
				}
			}
			// Last resort mint of HUMAN_CONFIRMED is allowed only outside
			// production (Mailpit/sink/dev/tests). Production fail-closed:
			// never silently promote a draft address without a legitimate
			// enrollable candidate. Explicit audited manual confirm is separate.
			if (cand == nil || !cand.CanEnroll()) && strings.Contains(email, "@") && s.cfg.AllowSilentEnrollMint() {
				mint := &models.OutreachContactCandidate{
					OrganizationID:     orgID,
					AccountID:          d.AccountID,
					SourceContactID:    "enroll-mint:" + email,
					Email:              email,
					VerificationStatus: models.OutreachVerifyHumanConfirmed,
					Confidence:         "HIGH",
					Recommended:        true,
				}
				if _, uerr := s.repo.UpsertCandidate(ctx, mint); uerr == nil && mint.CanEnroll() {
					cand = mint
					id := mint.ID
					d.ContactCandidateID = &id
				}
			}
		}
	}
	if err := RequireEmailOutbound(acc, cand); err != nil {
		return nil, errx.New(errx.BadRequest, err.Error())
	}
	if cand == nil || !cand.CanEnroll() {
		return nil, errx.New(errx.BadRequest, "contact is not enrollable")
	}

	// Validate the approved copy BEFORE appending the commercial signature
	// (signature is fixed operator branding, not part of the risk/copy gates).
	out := DraftOutput{
		Subject: d.Subject, BodyText: d.BodyText, FactUsed: d.FactUsed,
		ServiceCode: d.ServiceCode, EvidenceIDs: d.EvidenceIDs,
		Channel: channelKindFromDraft(d),
	}
	ev, _ := s.repo.ListEvidence(ctx, orgID, d.AccountID)
	val := ValidateDraft(&out, acc, cand, ValidateOpts{
		MaxWords: s.cfg.MaxInitialEmailWords, Evidence: ev, Channel: out.Channel,
	})
	if !val.OK {
		return nil, errx.New(errx.BadRequest, "draft failed validation: "+joinErrs(val.Errors))
	}
	if d.RiskClass == "RED" {
		// RED may enroll only after explicit approve (already APPROVED); allowed but audited.
	}

	// First-touch close: text only (Abraço + name + title + phone). No CID image
	// so cold mail does not show a paperclip/attachment indicator in Gmail.
	d.BodyText = AppendSignaturePlain(d.BodyText)
	d.BodyHTML = BodyToHTML(d.BodyText)

	camp, xerr := s.BootstrapCampaign(ctx, orgID, userID)
	if xerr != nil {
		return nil, xerr
	}

	first, last := splitName(cand.Name)
	company := firstNonEmpty(acc.NomeFantasia, acc.RazaoSocial)
	added, xerr := s.contacts.Add(ctx, userID.String(), orgID, []models.AddContact{{
		FirstName: first,
		LastName:  last,
		Email:     strings.TrimSpace(strings.ToLower(cand.Email)),
		Company:   company,
		Phone:     cand.Phone,
		Campaigns: []string{camp.ID.String()},
		CustomFields: map[string]string{
			"cnpj14":             acc.CNPJ14,
			"confenge_subject":   d.Subject,
			"confenge_body":      d.BodyText,
			"confenge_body_html": d.BodyHTML,
			"confenge_service":   d.ServiceCode,
			"confenge_fact":      d.FactUsed,
		},
	}})
	if xerr != nil {
		return nil, xerr
	}
	if len(added) == 0 {
		return nil, errx.New(errx.Internal, "contact create returned empty")
	}
	contactID := added[0].ID

	// Mark candidate promoted
	cand.WarmblyContactID = &contactID
	now := time.Now().UTC()
	cand.PromotedAt = &now
	_, _ = s.repo.UpsertCandidate(ctx, cand)

	d.Status = models.OutreachDraftEnrolled
	d.CampaignID = &camp.ID
	d.EnrollmentContactID = &contactID
	d.EnrolledAt = &now
	if err := s.repo.UpsertDraft(ctx, d); err != nil {
		return nil, errx.New(errx.Internal, "failed to update draft enrollment")
	}
	_ = s.repo.SetAccountHumanFlags(ctx, orgID, d.AccountID, acc.Blocked, acc.DoNotContact, acc.BlockReason, models.OutreachQueueEnrolled)

	// Outcomes: approved contact + contacted (enrolled into campaign)
	_ = s.EnqueueOutcome(ctx, orgID, models.OutreachOutcome{
		IdempotencyKey: fmt.Sprintf("contact_approved:%s:%s", orgID, d.ID),
		SourceLeadID:   acc.SourceLeadID,
		CNPJ14:         acc.CNPJ14,
		ContactEmail:   cand.Email,
		EventType:      OutcomeContactApproved,
		OccurredAt:     now,
	})
	_ = s.EnqueueOutcome(ctx, orgID, models.OutreachOutcome{
		IdempotencyKey: fmt.Sprintf("contacted:%s:%s", orgID, d.ID),
		SourceLeadID:   acc.SourceLeadID,
		CNPJ14:         acc.CNPJ14,
		ContactEmail:   cand.Email,
		EventType:      OutcomeContacted,
		OccurredAt:     now,
	})

	if s.audit != nil {
		s.audit.LogAction(ctx, orgID, userID, models.AuditActionCreate, models.AuditEntityContact, &contactID, "", "",
			map[string]string{
				"draft_id":    d.ID.String(),
				"campaign_id": camp.ID.String(),
				"cnpj14":      acc.CNPJ14,
			},
			map[string]string{"source": "confenge_enroll"},
		)
	}
	return d, nil
}

func splitName(full string) (first, last string) {
	parts := strings.Fields(strings.TrimSpace(full))
	if len(parts) == 0 {
		return "", ""
	}
	if len(parts) == 1 {
		return parts[0], ""
	}
	return parts[0], strings.Join(parts[1:], " ")
}
