package advisor

import (
	"fmt"

	"github.com/google/uuid"

	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/pkg/tmplvars"
	"github.com/warmbly/warmbly/internal/repository"
)

// listDetectors read the audience rather than the message. Most campaign
// failures are list failures wearing a copy costume, so these matter more than
// their severity suggests.
func listDetectors() []Detector {
	return []Detector{
		{
			Key:      "list_role_addresses",
			Category: models.AdvisorCategoryList,
			About:    "The share of a campaign's audience that is a shared inbox (info@, sales@, support@). These rarely reply, are frequently monitored by people with no interest in the offer, and complain at a higher rate than named recipients.",
			Reads:    []repository.AdvisorSection{repository.AdvisorSectionCampaigns, repository.AdvisorSectionLists},
			Run:      detectRoleAddresses,
		},
		{
			Key:      "list_missing_personalization_data",
			Category: models.AdvisorCategoryList,
			About:    "Contacts missing the fields the copy personalizes on, which is how an email ends up opening with 'Hi ,'. This is a data problem that surfaces as a copy problem.",
			Reads:    []repository.AdvisorSection{repository.AdvisorSectionCampaigns, repository.AdvisorSectionLists, repository.AdvisorSectionSteps},
			Run:      detectMissingPersonalizationData,
		},
		{
			Key:      "list_unsubscribed_enrolled",
			Category: models.AdvisorCategoryList,
			About:    "Contacts who have unsubscribed but are still enrolled in a campaign. Mailing someone who opted out is the fastest route to a complaint and, in several jurisdictions, is not merely impolite.",
			Reads:    []repository.AdvisorSection{repository.AdvisorSectionCampaigns, repository.AdvisorSectionLists},
			Run:      detectUnsubscribedEnrolled,
		},
	}
}

// campaignsWithLists pairs each non-draft campaign with its list stats, skipping
// audiences too small for a share to mean anything.
func campaignsWithLists(s *repository.AdvisorSnapshot, minSize int) []struct {
	Campaign repository.AdvisorCampaign
	List     repository.AdvisorListStats
} {
	out := []struct {
		Campaign repository.AdvisorCampaign
		List     repository.AdvisorListStats
	}{}
	for _, c := range s.Campaigns {
		if c.Status == "draft" {
			continue
		}
		l, ok := s.Lists[c.ID]
		if !ok || l.Total < minSize {
			continue
		}
		out = append(out, struct {
			Campaign repository.AdvisorCampaign
			List     repository.AdvisorListStats
		}{c, l})
	}
	return out
}

func detectRoleAddresses(s *repository.AdvisorSnapshot) []Finding {
	out := []Finding{}
	for _, cl := range campaignsWithLists(s, 100) {
		r := rate(cl.List.RoleAddresses, cl.List.Total)
		if r < roleAddressShareWarn {
			continue
		}

		out = append(out, Finding{
			Key:         "list_role_addresses",
			GroupTitle:  "{count} campaigns are mailing mostly shared inboxes",
			Category:    models.AdvisorCategoryList,
			Severity:    models.AdvisorMedium,
			Surface:     models.AdvisorSurfaceContacts,
			EntityType:  "campaign",
			EntityID:    ref(cl.Campaign.ID),
			EntityLabel: cl.Campaign.Name,
			Impact:      clampImpact(30 + int(r)),
			Title:       fmt.Sprintf("%s of %s is shared inboxes", pct(r), cl.Campaign.Name),
			Detail: fmt.Sprintf(
				"%s of the %s in %s are addresses like info@ or sales@. Shared inboxes rarely reply, are usually read by someone with no interest in the offer, and complain at a higher rate than a named recipient does.",
				pct(r), plural(cl.List.Total, "contact", "contacts"), cl.Campaign.Name),
			Remedy: "Filter role addresses out of the list and find the named person instead. A smaller list of real people outperforms a larger one of shared inboxes on every metric that matters.",
			Evidence: map[string]any{
				"campaign":           cl.Campaign.Name,
				"role_addresses":     cl.List.RoleAddresses,
				"contacts":           cl.List.Total,
				"role_share_percent": band(r),
				"reply_rate_percent": band(rate(cl.Campaign.Replied, cl.Campaign.Sent)),
			},
		})
	}
	return out
}

func detectMissingPersonalizationData(s *repository.AdvisorSnapshot) []Finding {
	// Only a step that prints the name with no fallback leaves a gap: a
	// {{if .FirstName}} or a default covers the contacts without one.
	gapped := map[uuid.UUID]bool{}
	for _, sc := range emailSteps(s) {
		parts := []string{sc.step.BodyPlain, sc.step.BodyHTML}
		if !sc.threaded {
			parts = append(parts, sc.step.Subject)
		}
		if printsBlankFirstName(parts...) {
			gapped[sc.step.CampaignID] = true
		}
	}

	out := []Finding{}
	for _, cl := range campaignsWithLists(s, 50) {
		r := rate(cl.List.MissingFirstName, cl.List.Total)
		if r < missingNameShareWarn || !gapped[cl.Campaign.ID] {
			continue
		}

		out = append(out, Finding{
			Key:         "list_missing_personalization_data",
			GroupTitle:  "{count} campaigns greet contacts that have no first name",
			Category:    models.AdvisorCategoryList,
			Severity:    models.AdvisorMedium,
			Surface:     models.AdvisorSurfaceContacts,
			EntityType:  "campaign",
			EntityID:    ref(cl.Campaign.ID),
			EntityLabel: cl.Campaign.Name,
			Impact:      clampImpact(40 + int(r)),
			Title:       fmt.Sprintf("%s of %s has no first name to greet", pct(r), cl.Campaign.Name),
			Detail: fmt.Sprintf(
				"%s greets contacts by first name, but %s in the list have that field empty. Those emails go out with a gap where the name should be, which is the most recognisable tell of automated outreach there is.",
				cl.Campaign.Name, plural(cl.List.MissingFirstName, "contact", "contacts")),
			Remedy: "Give the variable a fallback, or fill the missing names before the campaign reaches those contacts.",
			Steps: []string{
				"The quickest fix is a fallback in the copy. Replace {{.FirstName}} with {{if .FirstName}}{{.FirstName}}{{else}}there{{end}} so the greeting still reads properly when the field is empty.",
				"The better fix is the data. Open Contacts, filter the campaign's list to contacts with no first name, and fill them in or remove them.",
				"If you cannot source the names, drop the greeting from this campaign entirely. An email that opens on the reason you are writing beats one that opens on a guessed name.",
				"Preview against one of the contacts that was missing a name before you resume.",
			},
			Evidence: map[string]any{
				"campaign":                    cl.Campaign.Name,
				"contacts_missing_first_name": cl.List.MissingFirstName,
				"contacts":                    cl.List.Total,
				"missing_share_percent":       band(r),
			},
		})
	}
	return out
}

func detectUnsubscribedEnrolled(s *repository.AdvisorSnapshot) []Finding {
	out := []Finding{}
	for _, cl := range campaignsWithLists(s, 1) {
		if cl.List.Unsubscribed == 0 || cl.Campaign.Status != "active" {
			continue
		}

		out = append(out, Finding{
			Key:         "list_unsubscribed_enrolled",
			GroupTitle:  "{count} campaigns still have unsubscribed contacts enrolled",
			Category:    models.AdvisorCategoryList,
			Severity:    models.AdvisorHigh,
			Surface:     models.AdvisorSurfaceContacts,
			EntityType:  "campaign",
			EntityID:    ref(cl.Campaign.ID),
			EntityLabel: cl.Campaign.Name,
			Impact:      clampImpact(55 + cl.List.Unsubscribed),
			Title:       fmt.Sprintf("%s who unsubscribed are still in %s", plural(cl.List.Unsubscribed, "contact", "contacts"), cl.Campaign.Name),
			Detail: fmt.Sprintf(
				"%s in %s have unsubscribed but are still enrolled. Mailing someone who opted out is the fastest way to earn a spam complaint, and in several jurisdictions it is not merely impolite.",
				plural(cl.List.Unsubscribed, "contact", "contacts"), cl.Campaign.Name),
			Remedy: "Remove them from the campaign. If they are still receiving mail, that is a routing problem worth understanding before anything else in this campaign.",
			Steps: []string{
				"Open the campaign's contacts and filter to unsubscribed.",
				"Remove them from the campaign. Suppression stops future sends, but leaving them enrolled keeps the campaign reporting an audience it must not mail.",
				"Check whether any of them were sent to after they unsubscribed. If so, stop the campaign: that is a routing problem, and every further send compounds it.",
				"Check where the list came from. Unsubscribed contacts reappearing usually means a re-import overwrote their status.",
			},
			Evidence: map[string]any{
				"campaign":     cl.Campaign.Name,
				"unsubscribed": cl.List.Unsubscribed,
				"contacts":     cl.List.Total,
			},
		})
	}
	return out
}

// printsBlankFirstName reports whether any part prints the contact's first
// name where an empty one leaves a gap. A part that does not parse is sent by
// plain replacement, so any reference to the name counts.
func printsBlankFirstName(parts ...string) bool {
	for _, p := range parts {
		gaps, ok := tmplvars.BlankGaps(p)
		if !ok {
			gaps = tmplvars.References(p)
		}
		if gaps["FirstName"] {
			return true
		}
	}
	return false
}
