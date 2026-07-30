package advisor

import (
	"fmt"

	"github.com/warmbly/warmbly/internal/models"
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
			Run:      detectRoleAddresses,
		},
		{
			Key:      "list_free_mail_heavy",
			Category: models.AdvisorCategoryList,
			About:    "The share of a B2B campaign's audience on consumer mailbox domains. A cold list that is mostly consumer addresses is usually scraped rather than sourced, and both reply and complaint rates reflect that.",
			Run:      detectFreeMailHeavy,
		},
		{
			Key:      "list_suppressed_share",
			Category: models.AdvisorCategoryList,
			About:    "Leads already on the organization's suppression list. They are skipped at send time, so a large share means the campaign delivers far less than its numbers suggest.",
			Run:      detectSuppressedShare,
		},
		{
			Key:      "list_missing_personalization_data",
			Category: models.AdvisorCategoryList,
			About:    "Contacts missing the fields the copy personalizes on, which is how an email ends up opening with 'Hi ,'. This is a data problem that surfaces as a copy problem.",
			Run:      detectMissingPersonalizationData,
		},
		{
			Key:      "list_unsubscribed_enrolled",
			Category: models.AdvisorCategoryList,
			About:    "Contacts who have unsubscribed but are still enrolled in a campaign. Mailing someone who opted out is the fastest route to a complaint and, in several jurisdictions, is not merely impolite.",
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

func detectFreeMailHeavy(s *repository.AdvisorSnapshot) []Finding {
	out := []Finding{}
	for _, cl := range campaignsWithLists(s, 100) {
		r := rate(cl.List.FreeMail, cl.List.Total)
		if r < freeMailShareWarn {
			continue
		}

		out = append(out, Finding{
			Key:         "list_free_mail_heavy",
			GroupTitle:  "{count} campaigns are mailing mostly consumer mailboxes",
			Category:    models.AdvisorCategoryList,
			Severity:    models.AdvisorLow,
			Surface:     models.AdvisorSurfaceContacts,
			EntityType:  "campaign",
			EntityID:    ref(cl.Campaign.ID),
			EntityLabel: cl.Campaign.Name,
			Impact:      clampImpact(20 + int(r)/2),
			Title:       fmt.Sprintf("%s of %s is on consumer mailboxes", pct(r), cl.Campaign.Name),
			Detail: fmt.Sprintf(
				"%s of the contacts in %s are on gmail, outlook.com, yahoo and similar. For B2B outreach that usually means the list was scraped rather than sourced, and consumer providers are also the strictest filters you will face.",
				pct(r), cl.Campaign.Name),
			Remedy: "Check where this list came from. If the target is companies, the addresses should mostly be on company domains.",
			Steps: []string{
				"Check where the list came from. A B2B list that is mostly consumer addresses was usually scraped rather than sourced.",
				"Filter the consumer-domain contacts out and see what is left. If that is most of the list, the list is the problem, not the copy.",
				"Rebuild from a source that gives you company addresses: an export from your CRM, a provider that verifies, or manual research on a smaller set.",
				"Keep the consumer addresses out of cold campaigns. They reply less and complain more, and the complaints land on your sending domain.",
			},
			Evidence: map[string]any{
				"campaign":                cl.Campaign.Name,
				"free_mail_contacts":      cl.List.FreeMail,
				"contacts":                cl.List.Total,
				"free_mail_share_percent": band(r),
			},
		})
	}
	return out
}

func detectSuppressedShare(s *repository.AdvisorSnapshot) []Finding {
	out := []Finding{}
	for _, cl := range campaignsWithLists(s, 50) {
		r := rate(cl.List.Suppressed, cl.List.Total)
		if r < suppressedShareWarn {
			continue
		}

		out = append(out, Finding{
			Key:         "list_suppressed_share",
			GroupTitle:  "{count} campaigns have a large share they will never send to",
			Category:    models.AdvisorCategoryList,
			Severity:    models.AdvisorMedium,
			Surface:     models.AdvisorSurfaceContacts,
			EntityType:  "campaign",
			EntityID:    ref(cl.Campaign.ID),
			EntityLabel: cl.Campaign.Name,
			Impact:      clampImpact(25 + int(r)),
			Title:       fmt.Sprintf("%s of %s will never be sent to", pct(r), cl.Campaign.Name),
			Detail: fmt.Sprintf(
				"%s of the %s in %s are on your suppression list from a previous bounce, complaint, or unsubscribe. They are skipped at send time, so this campaign will reach far fewer people than its numbers suggest.",
				plural(cl.List.Suppressed, "contact", "contacts"), plural(cl.List.Total, "contact", "contacts"), cl.Campaign.Name),
			Remedy: "Clean the suppressed contacts out of the list so the campaign's reported audience matches what it can actually reach, and check where the list overlaps with ones you have already mailed.",
			Evidence: map[string]any{
				"campaign":                 cl.Campaign.Name,
				"suppressed":               cl.List.Suppressed,
				"contacts":                 cl.List.Total,
				"suppressed_share_percent": band(r),
			},
		})
	}
	return out
}

func detectMissingPersonalizationData(s *repository.AdvisorSnapshot) []Finding {
	out := []Finding{}
	for _, cl := range campaignsWithLists(s, 50) {
		r := rate(cl.List.MissingFirstName, cl.List.Total)
		if r < missingNameShareWarn {
			continue
		}

		// Only worth reporting if the copy actually uses the field.
		uses := false
		for _, st := range s.Steps {
			if st.CampaignID != cl.Campaign.ID {
				continue
			}
			if usesFirstName(st.Subject + st.BodyPlain + st.BodyHTML) {
				uses = true
				break
			}
		}
		if !uses {
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

// usesFirstName reports whether the copy actually greets people by first name.
// Warmbly templates are Go templates, so the field is written `.FirstName`
// (`{{.FirstName}}`, `{{if .FirstName}}`, `{{.FirstName | title}}`) or, for the
// index form a spaced custom key would take, `"first_name"`. Checking the
// property rather than one literal spelling keeps this from silently never
// firing the way a `{{first_name}}` assumption would.
func usesFirstName(text string) bool {
	for _, form := range []string{".FirstName", `"first_name"`} {
		if indexFold(text, form) >= 0 {
			return true
		}
	}
	return false
}

// indexFold is a case-insensitive substring search that avoids allocating a
// lowered copy of every email body on every run.
func indexFold(haystack, needle string) int {
	n, h := len(needle), len(haystack)
	if n == 0 || n > h {
		return -1
	}
	for i := 0; i+n <= h; i++ {
		match := true
		for j := 0; j < n; j++ {
			a, b := haystack[i+j], needle[j]
			if 'A' <= a && a <= 'Z' {
				a += 'a' - 'A'
			}
			if 'A' <= b && b <= 'Z' {
				b += 'a' - 'A'
			}
			if a != b {
				match = false
				break
			}
		}
		if match {
			return i
		}
	}
	return -1
}
