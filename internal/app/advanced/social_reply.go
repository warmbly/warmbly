package advanced

import (
	"context"
	"strings"

	"github.com/warmbly/warmbly/internal/app/replyclassify"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
)

// ProcessSocialReply is the email reply path for an inbound social message: classified the same way, it
// stamps the campaign step it answers, fires the same instant branches, webhooks and notification.
func (s *service) ProcessSocialReply(ctx context.Context, in models.SocialReply) *errx.Error {
	settings, err := s.repo.GetOutreachSettings(ctx, in.OrganizationID)
	if err != nil {
		return toErrx(err)
	}
	cin := replyclassify.Input{BodyText: in.Body, OrganizationID: in.OrganizationID, Languages: settings.InboxTagging.Languages}
	var verdict replyclassify.Result
	if in.CampaignID != nil && in.ContactID != nil && in.SequenceID != nil {
		cID, ctID, sID := *in.CampaignID, *in.ContactID, *in.SequenceID
		verdict = replyclassify.ClassifyGated(ctx, cin, func() bool {
			if !replyclassify.WorthModeling(cin) {
				return false
			}
			prior, perr := s.campaignProgressRepo.GetLatestReplyClass(ctx, ctID, cID)
			return perr != nil || prior == "" || prior == replyclassify.ClassUnknown
		})
		_ = s.campaignProgressRepo.RecordReplyClassification(ctx, cID, ctID, sID, verdict.Class, verdict.Source, verdict.Confidence)
		if !replyclassify.IsAutomated(verdict.Class) {
			accepted, err := s.campaignProgressRepo.RecordSocialReplied(ctx, cID, ctID, sID)
			if err != nil {
				return toErrx(err)
			}
			if accepted {
				if settings.ReplyIntent.Enabled && settings.ReplyIntent.StopAllCampaignsOnReply {
					if _, err := s.holdOtherCampaigns(ctx, ctID, cID); err != nil {
						return toErrx(err)
					}
				}
				if s.realtime != nil {
					userID := ""
					if in.NotifyUserID != nil {
						userID = in.NotifyUserID.String()
					}
					s.realtime.PublishEmailReplied(ctx, in.OrganizationID.String(), userID, cID.String(), ctID.String(), in.Sender, sID.String())
				}
			}
		}
		s.fireInstantActions(ctx, cID, ctID, sID, "reply")
	} else {
		verdict = replyclassify.ClassifyKnown(ctx, cin)
	}

	if !settings.ReplyIntent.Enabled {
		return nil
	}
	intent, confidence, decided := verdictIntent(verdict)
	if !decided {
		intent, confidence = classifyReply(strings.TrimSpace(in.Body), settings.ReplyIntent)
	}
	channel := in.Channel
	if channel == "" {
		channel = models.SocialLinkedIn
	}
	label := models.SocialChannelLabel(channel)
	link := "/app/unibox/social/" + in.ConversationID.String()
	if in.CampaignID != nil {
		payload := map[string]any{
			"channel":           channel,
			"contact_name":      in.Sender,
			"intent":            string(intent),
			"confidence":        confidence,
			"snippet":           snippet(in.Body),
			"trigger":           "campaign_reply",
			"campaign_id":       in.CampaignID.String(),
			"social_account_id": in.AccountID.String(),
			"conversation_id":   in.ConversationID.String(),
			"_body_text":        in.Body,
		}
		if in.ContactID != nil {
			payload["contact_id"] = in.ContactID.String()
		}
		s.emit(ctx, in.OrganizationID, models.WebhookEventCampaignReplyReceived, payload)
	}
	if s.notifier == nil || in.NotifyUserID == nil {
		return nil
	}
	cat, title := models.NotifInboundReply, "New "+label+" reply from "+in.Sender
	switch {
	case models.IsAutomatedIntent(intent):
		cat, title = models.NotifInboundOOO, "Automatic "+label+" reply from "+in.Sender
	case intent == models.ReplyIntentPositive:
		cat, title = models.NotifInboundPositiveReply, "Positive "+label+" reply from "+in.Sender
	}
	orgID := in.OrganizationID
	s.notifier.Notify(context.WithoutCancel(ctx), *in.NotifyUserID, &orgID, cat, title, snippet(in.Body), link, map[string]any{
		"intent": string(intent), "channel": channel, "conversation_id": in.ConversationID.String(),
		"social_account_id": in.AccountID.String(),
	})
	return nil
}

func snippet(body string) string {
	body = strings.Join(strings.Fields(body), " ")
	if r := []rune(body); len(r) > 200 {
		return string(r[:199]) + "…"
	}
	return body
}
