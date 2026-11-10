package advisor

import (
	"fmt"

	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/repository"
)

// campaignDetectors cover the campaign settings that decide whether mail goes
// out at all and whether recipients can leave.
func campaignDetectors() []Detector {
	return []Detector{
		{
			Key:      "campaign_unsubscribe_header_off",
			Category: models.AdvisorCategoryCampaign,
			About:    "Whether a campaign sends the List-Unsubscribe header. Google's bulk sender rules require one-click unsubscribe on marketing mail, and an easy unsubscribe is what stops an uninterested recipient reaching for the spam button instead.",
			Reads:    []repository.AdvisorSection{repository.AdvisorSectionCampaigns},
			Run:      detectUnsubscribeHeaderOff,
		},
		{
			Key:      "campaign_no_senders",
			Category: models.AdvisorCategoryCampaign,
			About:    "A running campaign with no active mailbox resolving for it. Its sender_selection says how it picks mailboxes: by tag (no active mailbox carries its tags), picked by hand (those mailboxes were disconnected), or every active mailbox (the workspace has none active). The campaign is live and sending nothing.",
			Reads:    []repository.AdvisorSection{repository.AdvisorSectionCampaigns},
			Run:      detectNoSenders,
		},
	}
}

// runningCampaigns filters to campaigns that are actually sending.
func runningCampaigns(s *repository.AdvisorSnapshot) []repository.AdvisorCampaign {
	out := []repository.AdvisorCampaign{}
	for _, c := range s.Campaigns {
		if c.Status == "active" {
			out = append(out, c)
		}
	}
	return out
}

func detectUnsubscribeHeaderOff(s *repository.AdvisorSnapshot) []Finding {
	out := []Finding{}
	for _, camp := range s.Campaigns {
		if camp.Status == "draft" || camp.UnsubscribeHeader {
			continue
		}

		severity := models.AdvisorMedium
		if camp.Status == "active" {
			severity = models.AdvisorHigh
		}

		out = append(out, Finding{
			Key:         "campaign_unsubscribe_header_off",
			GroupTitle:  "{count} campaigns send without a one-click unsubscribe",
			Category:    models.AdvisorCategoryCampaign,
			Severity:    severity,
			Surface:     models.AdvisorSurfaceCampaigns,
			EntityType:  "campaign",
			EntityID:    ref(camp.ID),
			EntityLabel: camp.Name,
			Impact:      clampImpact(45 + camp.Sent/100),
			Title:       fmt.Sprintf("%s sends without a one-click unsubscribe", camp.Name),
			Detail: fmt.Sprintf(
				"%s has the List-Unsubscribe header switched off. Google's bulk sender rules require one-click unsubscribe, and more practically: a recipient who cannot find an easy way out reaches for the spam button instead, which costs far more than an unsubscribe does.",
				camp.Name),
			Remedy: "Turn the unsubscribe header on. It is a header, not visible copy, so it changes nothing about how the email reads.",
			Evidence: map[string]any{
				"campaign":       camp.Name,
				"status":         camp.Status,
				"sends_30d":      camp.Sent,
				"complaints_30d": camp.Complaints,
			},
			Action: auto(withUndo(campaignAction(camp.ID,
				"Turn on one-click unsubscribe",
				map[string]any{"unsubscribe_header": true},
				change("List-Unsubscribe header", "off", "on"),
			), map[string]any{"campaign_id": camp.ID.String(), "unsubscribe_header": false})),
		})
	}
	return out
}

func detectNoSenders(s *repository.AdvisorSnapshot) []Finding {
	out := []Finding{}
	for _, camp := range runningCampaigns(s) {
		if camp.SenderCount > 0 {
			continue
		}
		sel := noSenderCopyFor(camp)

		out = append(out, Finding{
			Key:         "campaign_no_senders",
			GroupTitle:  "{count} running campaigns have no mailbox to send from",
			Category:    models.AdvisorCategoryCampaign,
			Severity:    models.AdvisorCritical,
			Surface:     models.AdvisorSurfaceCampaigns,
			EntityType:  "campaign",
			EntityID:    ref(camp.ID),
			EntityLabel: camp.Name,
			Impact:      90,
			Title:       fmt.Sprintf("%s is running but has no mailbox to send from", camp.Name),
			Detail:      fmt.Sprintf("%s is active, but %s, so it is sending nothing at all.", camp.Name, sel.cause),
			Remedy:      sel.remedy + " Nothing else in the campaign matters until this is resolved.",
			Steps: append(sel.steps,
				"Sending resumes on the next scheduling pass. Nothing queued was lost."),
			Evidence: map[string]any{
				"campaign":             camp.Name,
				"sender_selection":     sel.selection,
				"picked_mailbox_count": camp.PickedSenders,
				"tag_count":            camp.SenderTags,
				"sender_count":         0,
				"leads_remaining":      camp.LeadsRemaining,
			},
		})
	}
	return out
}

// noSenderCopy is the no-senders finding's wording for how a campaign picks
// its mailboxes, so the advice never points at tags a campaign does not use.
type noSenderCopy struct {
	selection string
	cause     string
	remedy    string
	steps     []string
}

func noSenderCopyFor(camp repository.AdvisorCampaign) noSenderCopy {
	switch {
	case camp.PickedSenders == 0 && camp.SenderTags == 0 && camp.SenderStrategy != repository.CampaignSenderStrategyExplicit:
		return noSenderCopy{
			selection: "every active mailbox",
			cause:     "it sends from every active mailbox in the workspace and none is active right now",
			remedy:    "Reconnect a disconnected mailbox or connect a new one.",
			steps: []string{
				"Open Mailboxes and look for ones that are disconnected or need re-authentication.",
				"Reconnect one, or connect a new mailbox.",
			},
		}
	case camp.SenderTags == 0:
		return noSenderCopy{
			selection: "picked by hand",
			cause:     "none of the mailboxes picked for it is connected and active",
			remedy:    "Pick at least one active mailbox in the campaign's sending accounts, or reconnect the ones it uses.",
			steps: []string{
				"Open the campaign's sending accounts.",
				"Pick at least one connected, active mailbox, or reconnect the ones already picked.",
				"Save.",
			},
		}
	case camp.PickedSenders == 0:
		return noSenderCopy{
			selection: "by tag",
			cause:     "no active mailbox carries any of its tags",
			remedy:    "Put one of the campaign's tags on an active mailbox, or change the tags the campaign selects.",
			steps: []string{
				"Open the campaign's sending accounts and note its tags.",
				"Check that an active mailbox still carries one of them. A renamed tag is a common cause.",
				"Tag a mailbox, or change the campaign's tags, and save.",
			},
		}
	default:
		return noSenderCopy{
			selection: "picked by hand and by tag",
			cause:     "none of its picked mailboxes is active and no active mailbox carries its tags",
			remedy:    "Pick an active mailbox, or tag one with a tag the campaign selects.",
			steps: []string{
				"Open the campaign's sending accounts.",
				"Pick a connected, active mailbox, or tag one with a tag the campaign selects.",
				"Save.",
			},
		}
	}
}
