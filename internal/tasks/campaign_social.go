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

// SocialSteps queues a campaign's social steps at GraphKit.
type SocialSteps interface {
	RunCampaignStep(ctx context.Context, req models.SocialStepRequest) models.SocialStepOutcome
}

// SocialAware is the optional capability the caller uses to attach social steps after construction.
type SocialAware interface {
	WireSocial(l SocialSteps)
}

// WireSocial attaches the social add-on; without it a social step is skipped and logged.
func (s *tasksService) WireSocial(l SocialSteps) {
	s.social = l
}

// socialRetry is how soon a social step GraphKit did not take is tried again.
const socialRetry = 2 * time.Minute

// runSocialStep queues one due social step and routes the contact on. GraphKit holds the step under a key
// derived from it, so a pass that repeats after a crash queues nothing twice.
func (s *tasksService) runSocialStep(ctx context.Context, campaign *models.Campaign, contact *models.Contact, sequence *Sequence, cfg *models.ActionConfig, taskID, accountID uuid.UUID, nextTime time.Time) {
	out := models.SocialStepOutcome{State: models.SocialStepSkipped, Reason: "Social accounts are not available on this instance", Code: "social_unavailable"}
	if s.social != nil {
		var step models.SocialStep
		if cfg.Social != nil {
			step = *cfg.Social
		}
		out = s.social.RunCampaignStep(ctx, models.SocialStepRequest{
			OrganizationID: *campaign.OrganizationID, CampaignID: campaign.ID, ContactID: contact.ID, SequenceID: sequence.ID,
			Step: step, Render: socialRenderer(campaign.ID, sequence.ID, *contact),
		})
	}
	label := "Social"
	if cfg.Social != nil && models.IsSocialChannel(cfg.Social.Channel) {
		label = models.SocialChannelLabel(cfg.Social.Channel)
	}
	who := contactLabel(contact)
	if out.State == models.SocialStepRetry {
		wait := out.RetryAfter
		if wait <= 0 {
			wait = socialRetry
		}
		if s.campaignLogRepo != nil {
			day := time.Now().UTC().Truncate(24 * time.Hour)
			_, _ = s.campaignLogRepo.CreateLogOnce(ctx, &repository.CampaignLogEntry{CampaignID: campaign.ID, EventType: "social_waiting",
				Message:  fmt.Sprintf("%s step for %s is waiting: %s", label, who, out.Reason),
				Metadata: map[string]interface{}{"contact_id": contact.ID.String(), "day": day.Format("2006-01-02")}}, "day", day.Format("2006-01-02"), day)
		}
		// Only this contact waits; the pass goes on to the next one.
		resumeAt := nextTime
		if derr := s.campaignProgressRepo.DeferStep(ctx, campaign.ID, contact.ID, sequence.ID, time.Now().UTC().Add(wait)); derr != nil {
			log.Warn().Err(derr).Str("campaign_id", campaign.ID.String()).Msg("Failed to defer a social step")
			resumeAt = time.Now().UTC().Add(wait)
		}
		if cerr := s.createCampaignTask(ctx, campaign.ID, accountID, resumeAt); cerr != nil {
			log.Warn().Err(cerr).Str("campaign_id", campaign.ID.String()).Msg("Failed to schedule a retry of a social step")
		}
		s.taskRepo.UpdateTaskStatus(ctx, taskID, "completed")
		return
	}
	var rerr error
	switch out.State {
	case models.SocialStepQueued:
		rerr = s.campaignProgressRepo.RecordEmailSent(ctx, campaign.ID, contact.ID, sequence.ID)
	case models.SocialStepStop:
		rerr = s.campaignProgressRepo.RecordStepStopped(ctx, campaign.ID, contact.ID, sequence.ID, "social_stop:"+out.Code)
	default:
		rerr = s.campaignProgressRepo.RecordStepSkipped(ctx, campaign.ID, contact.ID, sequence.ID, "social_"+out.State+":"+out.Code)
	}
	resumeAt := nextTime
	if rerr != nil {
		// The step's action row holds it; the next pass finds it settled and records it.
		log.Warn().Err(rerr).Str("campaign_id", campaign.ID.String()).Msg("Failed to record social step progress")
		resumeAt = time.Now().UTC().Add(socialRetry)
	}
	if cerr := s.createCampaignTask(ctx, campaign.ID, accountID, resumeAt); cerr != nil {
		log.Warn().Err(cerr).Str("campaign_id", campaign.ID.String()).Msg("Failed to schedule next task after a social step")
	}
	if s.campaignLogRepo != nil && rerr == nil {
		evt, msg := "social_queued", fmt.Sprintf("%s for %s", out.Reason, who)
		switch out.State {
		case models.SocialStepSkipped:
			evt, msg = "social_skipped", fmt.Sprintf("%s step for %s was skipped: %s", label, who, out.Reason)
		case models.SocialStepFailed:
			evt, msg = "social_failed", fmt.Sprintf("%s step for %s failed: %s", label, who, out.Reason)
		case models.SocialStepStop:
			evt, msg = "social_stopped", fmt.Sprintf("%s left the campaign at a %s step: %s", who, label, out.Reason)
		}
		_ = s.campaignLogRepo.CreateLog(ctx, &repository.CampaignLogEntry{CampaignID: campaign.ID, EventType: evt, Message: msg,
			Metadata: map[string]interface{}{"contact_id": contact.ID.String(), "step_id": sequence.ID.String(), "action_type": socialAction(cfg), "channel": socialChannel(cfg), "code": out.Code}})
	}
	s.taskRepo.UpdateTaskStatus(ctx, taskID, "completed")
}

// holdUnknownStep keeps a contact on a step type a newer release added, so a backend on this release never
// marks it visited; the step is offered again later, by then perhaps to a backend that runs it.
func (s *tasksService) holdUnknownStep(ctx context.Context, campaign *models.Campaign, contact *models.Contact, sequenceID uuid.UUID, stepType string, taskID, accountID uuid.UUID, nextTime time.Time) {
	now := time.Now().UTC()
	resumeAt := nextTime
	if err := s.campaignProgressRepo.DeferStep(ctx, campaign.ID, contact.ID, sequenceID, now.Add(unknownActionRetry)); err != nil {
		log.Warn().Err(err).Str("campaign_id", campaign.ID.String()).Msg("Failed to hold a contact on an unknown step")
		resumeAt = now.Add(unknownActionRetry)
	}
	if s.campaignLogRepo != nil {
		day := now.Truncate(24 * time.Hour).Format("2006-01-02")
		_, _ = s.campaignLogRepo.CreateLogOnce(ctx, &repository.CampaignLogEntry{CampaignID: campaign.ID, EventType: "action_waiting",
			Message:  fmt.Sprintf("Step '%s' for %s is waiting: it needs every Warmbly backend on a newer release", stepType, contactLabel(contact)),
			Metadata: map[string]interface{}{"contact_id": contact.ID.String(), "step_id": sequenceID.String(), "day": day}}, "day", day, now.Truncate(24*time.Hour))
	}
	if err := s.createCampaignTask(ctx, campaign.ID, accountID, resumeAt); err != nil {
		log.Warn().Err(err).Str("campaign_id", campaign.ID.String()).Msg("Failed to schedule the next task after an unknown step")
	}
	s.taskRepo.UpdateTaskStatus(ctx, taskID, "completed")
}

// socialRenderer renders social copy with the email engine: merge fields, then the step's spintax for that part.
// missing is true when the copy prints a contact field the contact has empty.
func socialRenderer(campaignID, stepID uuid.UUID, contact models.Contact) func(text, part, senderName string) (string, bool) {
	values := map[string]string{"FirstName": contact.FirstName, "LastName": contact.LastName, "Company": contact.Company,
		"Phone": contact.Phone, "Email": contact.Email}
	return func(text, part, senderName string) (string, bool) {
		first, last := splitSenderName(senderName)
		extra := TemplateContext{Sender: TemplateSender{Name: strings.TrimSpace(senderName), FirstName: first, LastName: last}}
		out := expandSpintaxWith(RenderTemplateWith(text, contact, extra), spintaxRand(campaignID, contact.ID, stepID, "social_"+part))
		return out, len(missingFor(values, text)) > 0
	}
}

func socialAction(cfg *models.ActionConfig) string {
	if cfg.Social == nil {
		return ""
	}
	return cfg.Social.Action
}

func socialChannel(cfg *models.ActionConfig) string {
	if cfg.Social == nil {
		return ""
	}
	return cfg.Social.Channel
}

// skipEmaillessStep passes an email step for a contact with no address and routes them on, logged once per contact.
func (s *tasksService) skipEmaillessStep(ctx context.Context, campaign *models.Campaign, contact *models.Contact, sequence *Sequence, taskID, accountID uuid.UUID, nextTime time.Time) {
	resumeAt := nextTime
	if err := s.campaignProgressRepo.RecordStepSkipped(ctx, campaign.ID, contact.ID, sequence.ID, "no_email"); err != nil {
		log.Warn().Err(err).Str("campaign_id", campaign.ID.String()).Msg("Failed to record a skipped email step")
		resumeAt = time.Now().UTC().Add(socialRetry)
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
