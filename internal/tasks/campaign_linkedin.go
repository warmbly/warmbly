package tasks

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog/log"

	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/repository"
)

// LinkedInSteps queues a campaign's LinkedIn steps at GraphKit.
type LinkedInSteps interface {
	RunCampaignStep(ctx context.Context, req models.LinkedInStepRequest) models.LinkedInStepOutcome
}

// LinkedInAware is the optional capability the caller uses to attach LinkedIn steps after construction.
type LinkedInAware interface {
	WireLinkedIn(l LinkedInSteps)
}

// WireLinkedIn attaches the LinkedIn add-on; without it a LinkedIn step is skipped and logged.
func (s *tasksService) WireLinkedIn(l LinkedInSteps) {
	s.linkedIn = l
}

// linkedInRetry is how soon a LinkedIn step GraphKit did not take is tried again.
const linkedInRetry = 2 * time.Minute

// runLinkedInStep queues one due LinkedIn step and routes the contact on. GraphKit holds the step under a key
// derived from it, so a pass that repeats after a crash queues nothing twice.
func (s *tasksService) runLinkedInStep(ctx context.Context, campaign *models.Campaign, contact *models.Contact, sequence *Sequence, cfg *models.ActionConfig, taskID, accountID uuid.UUID, nextTime time.Time) {
	out := models.LinkedInStepOutcome{State: models.LinkedInStepSkipped, Reason: "LinkedIn is not available on this instance", Code: "linkedin_unavailable"}
	if s.linkedIn != nil {
		url := ""
		if contact.LinkedInURL != nil {
			url = *contact.LinkedInURL
		}
		out = s.linkedIn.RunCampaignStep(ctx, models.LinkedInStepRequest{
			OrganizationID: *campaign.OrganizationID, CampaignID: campaign.ID, ContactID: contact.ID, SequenceID: sequence.ID,
			Config: *cfg, LinkedInURL: url, Render: linkedInRenderer(campaign.ID, sequence.ID, *contact),
		})
	}
	who := contactLabel(contact)
	if out.State == models.LinkedInStepRetry {
		wait := out.RetryAfter
		if wait <= 0 {
			wait = linkedInRetry
		}
		if s.campaignLogRepo != nil {
			day := time.Now().UTC().Truncate(24 * time.Hour)
			_, _ = s.campaignLogRepo.CreateLogOnce(ctx, &repository.CampaignLogEntry{CampaignID: campaign.ID, EventType: "linkedin_waiting",
				Message:  fmt.Sprintf("LinkedIn step for %s is waiting: %s", who, out.Reason),
				Metadata: map[string]interface{}{"contact_id": contact.ID.String(), "day": day.Format("2006-01-02")}}, "day", day.Format("2006-01-02"), day)
		}
		if cerr := s.createCampaignTask(ctx, campaign.ID, accountID, time.Now().UTC().Add(wait)); cerr != nil {
			log.Warn().Err(cerr).Str("campaign_id", campaign.ID.String()).Msg("Failed to schedule a retry of a LinkedIn step")
		}
		s.taskRepo.UpdateTaskStatus(ctx, taskID, "completed")
		return
	}
	var rerr error
	if out.State == models.LinkedInStepQueued {
		rerr = s.campaignProgressRepo.RecordEmailSent(ctx, campaign.ID, contact.ID, sequence.ID)
	} else {
		rerr = s.campaignProgressRepo.RecordStepSkipped(ctx, campaign.ID, contact.ID, sequence.ID, "linkedin_"+out.State+":"+out.Code)
	}
	resumeAt := nextTime
	if rerr != nil {
		// The step's action row holds it; the next pass finds it settled and records it.
		log.Warn().Err(rerr).Str("campaign_id", campaign.ID.String()).Msg("Failed to record LinkedIn step progress")
		resumeAt = time.Now().UTC().Add(linkedInRetry)
	}
	if cerr := s.createCampaignTask(ctx, campaign.ID, accountID, resumeAt); cerr != nil {
		log.Warn().Err(cerr).Str("campaign_id", campaign.ID.String()).Msg("Failed to schedule next task after a LinkedIn step")
	}
	if s.campaignLogRepo != nil && rerr == nil {
		evt, msg := "linkedin_queued", fmt.Sprintf("%s for %s", out.Reason, who)
		switch out.State {
		case models.LinkedInStepSkipped:
			evt, msg = "linkedin_skipped", fmt.Sprintf("LinkedIn step for %s was skipped: %s", who, out.Reason)
		case models.LinkedInStepFailed:
			evt, msg = "linkedin_failed", fmt.Sprintf("LinkedIn step for %s failed: %s", who, out.Reason)
		}
		_ = s.campaignLogRepo.CreateLog(ctx, &repository.CampaignLogEntry{CampaignID: campaign.ID, EventType: evt, Message: msg,
			Metadata: map[string]interface{}{"contact_id": contact.ID.String(), "step_id": sequence.ID.String(), "action_type": cfg.Type, "code": out.Code}})
	}
	s.taskRepo.UpdateTaskStatus(ctx, taskID, "completed")
}

// linkedInRenderer renders LinkedIn copy with the email engine: merge fields, then the step's spintax for that part.
func linkedInRenderer(campaignID, stepID uuid.UUID, contact models.Contact) func(text, part, senderName string) string {
	return func(text, part, senderName string) string {
		first, last := splitSenderName(senderName)
		extra := TemplateContext{Sender: TemplateSender{Name: strings.TrimSpace(senderName), FirstName: first, LastName: last}}
		return expandSpintaxWith(RenderTemplateWith(text, contact, extra), spintaxRand(campaignID, contact.ID, stepID, "linkedin_"+part))
	}
}

// skipEmaillessStep passes an email step for a contact with no address and routes them on, logged once per contact.
func (s *tasksService) skipEmaillessStep(ctx context.Context, campaign *models.Campaign, contact *models.Contact, sequence *Sequence, taskID, accountID uuid.UUID, nextTime time.Time) {
	resumeAt := nextTime
	if err := s.campaignProgressRepo.RecordStepSkipped(ctx, campaign.ID, contact.ID, sequence.ID, "no_email"); err != nil {
		log.Warn().Err(err).Str("campaign_id", campaign.ID.String()).Msg("Failed to record a skipped email step")
		resumeAt = time.Now().UTC().Add(linkedInRetry)
	}
	if s.campaignLogRepo != nil {
		_, _ = s.campaignLogRepo.CreateLogOnce(ctx, &repository.CampaignLogEntry{CampaignID: campaign.ID, EventType: "no_email",
			Message:  fmt.Sprintf("Email steps for %s are skipped: the contact has no email address", contactLabel(contact)),
			Metadata: map[string]interface{}{"contact_id": contact.ID.String()}}, "contact_id", contact.ID.String(), time.Time{})
	}
	if cerr := s.createCampaignTask(ctx, campaign.ID, accountID, resumeAt); cerr != nil {
		log.Warn().Err(cerr).Str("campaign_id", campaign.ID.String()).Msg("Failed to schedule next task after skipping an email step")
	}
	s.taskRepo.UpdateTaskStatus(ctx, taskID, "completed")
}

func contactLabel(c *models.Contact) string {
	if c.Email != "" {
		return c.Email
	}
	if n := strings.TrimSpace(c.FirstName + " " + c.LastName); n != "" {
		return n
	}
	return "a contact"
}
