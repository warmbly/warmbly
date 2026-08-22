package advisor

import (
	"fmt"
	"strings"

	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/repository"
)

// deliverabilityDetectors cover the signals that decide whether mail reaches an
// inbox at all: complaints, bounces, spam placement, and sending-domain
// authentication.
func deliverabilityDetectors() []Detector {
	return []Detector{
		{
			Key:      "mailbox_complaint_rate",
			Category: models.AdvisorCategoryDeliverability,
			About:    "A mailbox's recipient spam-complaint rate over 30 days. Google asks senders to stay under 0.10% and never reach 0.30%; Amazon SES puts an account under review at 0.1% and can pause it at 0.5%. Complaints are the single strongest negative reputation signal a sender can generate.",
			Run:      detectComplaintRate,
		},
		{
			Key:      "mailbox_bounce_rate",
			Category: models.AdvisorCategoryDeliverability,
			About:    "A mailbox's bounce rate over 30 days. Amazon SES asks senders to stay under 5% and can pause sending at 10%. A high bounce rate almost always means the list was never verified, and mailbox providers read it as a signal the sender does not know who they are mailing.",
			Run:      detectBounceRate,
		},
		{
			Key:      "mailbox_spam_placement",
			Category: models.AdvisorCategoryDeliverability,
			About:    "The share of a mailbox's warmup mail that partner inboxes filed into spam. This is the earliest honest read on inbox placement, because it is measured on mail the platform controls end to end rather than inferred from opens.",
			Run:      detectSpamPlacement,
		},
		{
			Key:      "mailbox_domain_auth",
			Category: models.AdvisorCategoryDeliverability,
			About:    "SPF, DKIM, and DMARC on a mailbox's sending domain. Google's bulk sender rules require all three to be aligned. Without them, cold mail is filtered on arrival no matter how good the copy is.",
			Run:      detectDomainAuth,
		},
		{
			Key:      "mailbox_shared_tracking_domain",
			Category: models.AdvisorCategoryDeliverability,
			About:    "Whether a sending mailbox uses its own verified tracking domain. Open pixels and click links on a shared domain inherit every other sender's reputation on it, which is a self-inflicted deliverability drag.",
			Run:      detectSharedTrackingDomain,
		},
	}
}

func detectComplaintRate(s *repository.AdvisorSnapshot) []Finding {
	out := []Finding{}
	for _, m := range s.Mailboxes {
		if m.ColdSent30d < minSendsForComplaintRate {
			continue
		}
		r := rate(m.Complaints30d, m.ColdSent30d)
		if r < complaintRateWarn {
			continue
		}

		severity := models.AdvisorHigh
		if r >= complaintRateCritical {
			severity = models.AdvisorCritical
		}

		f := Finding{
			Key:         "mailbox_complaint_rate",
			GroupTitle:  "{count} mailboxes are drawing spam complaints",
			Category:    models.AdvisorCategoryDeliverability,
			Severity:    severity,
			Surface:     models.AdvisorSurfaceDeliverability,
			EntityType:  "email_account",
			EntityID:    ref(m.ID),
			EntityLabel: m.Email,
			Impact:      clampImpact(50 + m.ColdSent30d/50),
			Title:       fmt.Sprintf("%s is drawing spam complaints", m.Email),
			Detail: fmt.Sprintf(
				"%s recipients marked mail from %s as spam in the last 30 days, a rate of %s across %d sends. Google asks senders to stay under 0.10%% and never reach 0.30%%.",
				plural(m.Complaints30d, "recipient", "recipients"), m.Email, pct(r), m.ColdSent30d),
			Remedy: "Pause cold sending from this mailbox, cut the daily cap, and review which campaign the complaints came from before resuming. Complaints hurt the sending domain, not just this mailbox.",
			Evidence: map[string]any{
				"mailbox":                m.Email,
				"complaints_30d":         m.Complaints30d,
				"sends_30d":              m.ColdSent30d,
				"complaint_rate_percent": band(r),
				"google_limit_percent":   complaintRateCritical,
				"current_daily_cap":      m.CampaignLimit,
			},
		}

		// The fix is always the same shape: cut volume hard and immediately.
		// Halving is the conservative move that keeps the mailbox alive; the
		// user can pause it entirely from the mailbox page if they prefer.
		target := m.CampaignLimit / 2
		if target < 5 {
			target = 5
		}
		if target < m.CampaignLimit {
			f.Action = auto(withUndo(mailboxAction(m.ID,
				fmt.Sprintf("Cut the daily cap to %d", target),
				map[string]any{"campaign_limit": target},
				change("Daily cold cap", fmt.Sprintf("%d/day", m.CampaignLimit), fmt.Sprintf("%d/day", target)),
			), map[string]any{"email_account_id": m.ID.String(), "campaign_limit": m.CampaignLimit}))
		}
		out = append(out, f)
	}
	return out
}

func detectBounceRate(s *repository.AdvisorSnapshot) []Finding {
	out := []Finding{}
	for _, m := range s.Mailboxes {
		if m.ColdSent30d < minSendsForBounceRate {
			continue
		}
		r := rate(m.Bounces30d, m.ColdSent30d)
		if r < bounceRateWarn {
			continue
		}

		severity := models.AdvisorHigh
		if r >= bounceRateCritical {
			severity = models.AdvisorCritical
		}

		out = append(out, Finding{
			Key:         "mailbox_bounce_rate",
			GroupTitle:  "{count} mailboxes are bouncing too much mail",
			Category:    models.AdvisorCategoryDeliverability,
			Severity:    severity,
			Surface:     models.AdvisorSurfaceDeliverability,
			EntityType:  "email_account",
			EntityID:    ref(m.ID),
			EntityLabel: m.Email,
			Impact:      clampImpact(40 + int(r)),
			Title:       fmt.Sprintf("%s is bouncing %s of its sends", m.Email, pct(r)),
			Detail: fmt.Sprintf(
				"%d of %d sends from %s bounced in the last 30 days. Amazon SES asks senders to stay under 5%% and can pause sending at 10%%; mailbox providers read a high bounce rate as a sender who does not know who they are mailing.",
				m.Bounces30d, m.ColdSent30d, m.Email),
			Remedy: "This is a list problem, not a mailbox problem. Verify the contacts in the campaigns this mailbox sends for before sending more, and remove the domains that bounce repeatedly.",
			Steps: []string{
				"Pause the campaigns this mailbox sends for, so the rate stops climbing while you work.",
				"Open Contacts and filter to the lists those campaigns use.",
				"Run verification on the list, or re-import it from a verified source.",
				"Delete or suppress every address that hard-bounced. Those never come back.",
				"Resume sending, and watch the bounce rate on this page for a few days before raising volume again.",
			},
			Evidence: map[string]any{
				"mailbox":             m.Email,
				"bounces_30d":         m.Bounces30d,
				"sends_30d":           m.ColdSent30d,
				"bounce_rate_percent": band(r),
				"ses_review_percent":  bounceRateCritical,
			},
		})
	}
	return out
}

func detectSpamPlacement(s *repository.AdvisorSnapshot) []Finding {
	out := []Finding{}
	for _, m := range s.Mailboxes {
		// Warmup deliveries are the denominator: the mail we know landed
		// somewhere, and could observe the folder for.
		delivered := m.WarmupSent7d
		if delivered < minWarmupDeliveriesForPlacement || m.WarmupSpam7d == 0 {
			continue
		}
		r := rate(m.WarmupSpam7d, delivered)
		if r < spamPlacementWarn {
			continue
		}

		severity := models.AdvisorMedium
		remedy := "Slow this mailbox down: lower the daily cap and widen the gap between sends while placement recovers. Warmup should keep running throughout."
		switch {
		case r >= spamPlacementBlock:
			severity = models.AdvisorCritical
			remedy = "Stop cold sending from this mailbox now. At this placement rate almost nothing is reaching an inbox, and every further send deepens the reputation hole. Leave warmup running and let it recover before resuming."
		case r >= spamPlacementQuarantine:
			severity = models.AdvisorHigh
			remedy = "Take this mailbox out of cold rotation for a week. Keep warmup running, check SPF/DKIM/DMARC on the domain, and resume at a low cap once placement is back under 10%."
		}

		f := Finding{
			Key:         "mailbox_spam_placement",
			GroupTitle:  "{count} mailboxes are landing in spam",
			Category:    models.AdvisorCategoryDeliverability,
			Severity:    severity,
			Surface:     models.AdvisorSurfaceDeliverability,
			EntityType:  "email_account",
			EntityID:    ref(m.ID),
			EntityLabel: m.Email,
			Impact:      clampImpact(45 + int(r)),
			Title:       fmt.Sprintf("%s is landing in spam %s of the time", m.Email, pct(r)),
			Detail: fmt.Sprintf(
				"%d of %d warmup messages from %s were filed into spam by the receiving inbox in the last 7 days. Warmup placement is the earliest honest read on where cold mail is landing, because it is measured on mail the platform controls end to end.",
				m.WarmupSpam7d, delivered, m.Email),
			Remedy: remedy,
			Steps: []string{
				"Check SPF, DKIM and DMARC on this domain first. Authentication is the single biggest cause of spam placement, and no amount of volume tuning compensates for it.",
				"Lower this mailbox's daily cap and widen the gap between sends. Placement recovers on calm, spaced-out sending.",
				"Leave warmup running. It is what rebuilds the reputation, and stopping it now removes the only positive signal the mailbox is generating.",
				"Read the last few campaigns this mailbox sent for spam-trigger phrasing, heavy link counts, and missing unsubscribe headers.",
				"Give it a week and check this number again before putting the mailbox back into full rotation.",
			},
			Evidence: map[string]any{
				"mailbox":                 m.Email,
				"warmup_spam_7d":          m.WarmupSpam7d,
				"warmup_delivered_7d":     delivered,
				"spam_placement_percent":  band(r),
				"quarantine_band_percent": spamPlacementQuarantine,
				"currently_sending_cold":  m.InActiveCampaign,
				"current_daily_cap":       m.CampaignLimit,
			},
		}

		if m.InActiveCampaign && r >= spamPlacementQuarantine {
			f.Action = withUndo(mailboxAction(m.ID,
				"Pause cold sending from this mailbox",
				map[string]any{"status": "inactive"},
				change("Mailbox status", "active", "inactive"),
				change("Warmup", "running", "running (unchanged)"),
			), map[string]any{"email_account_id": m.ID.String(), "status": "active"})
		}
		out = append(out, f)
	}
	return out
}

func detectDomainAuth(s *repository.AdvisorSnapshot) []Finding {
	out := []Finding{}
	for _, m := range s.Mailboxes {
		// "unknown" means the sweep has not resolved this domain yet (or DNS
		// failed transiently). Reporting it as failing would cry wolf on every
		// freshly-connected mailbox.
		if m.AuthState == "unknown" || m.AuthState == "" {
			continue
		}
		if m.AuthSPF && m.AuthDKIM && m.AuthDMARC {
			continue
		}

		missing := []string{}
		if !m.AuthSPF {
			missing = append(missing, "SPF")
		}
		if !m.AuthDKIM {
			missing = append(missing, "DKIM")
		}
		if !m.AuthDMARC {
			missing = append(missing, "DMARC")
		}

		// Unauthenticated mail that is actively going out is a live problem;
		// on an idle mailbox it is a setup task.
		severity := models.AdvisorMedium
		if m.InActiveCampaign {
			severity = models.AdvisorCritical
			if len(missing) == 1 && !m.AuthDMARC {
				// SPF+DKIM present, DMARC missing: filtered less aggressively,
				// but still short of Google's bulk sender requirements.
				severity = models.AdvisorHigh
			}
		}

		// What the platform itself is about to do about it. A mailbox the gate
		// has already stopped is critical whatever else is true of it: nothing
		// is going out, and only a DNS change starts it again.
		gate, blocked := domainAuthGateNote(s, m)
		if blocked {
			severity = models.AdvisorCritical
		}

		out = append(out, Finding{
			Key:         "mailbox_domain_auth",
			GroupTitle:  "{count} sending domains are missing authentication records",
			Category:    models.AdvisorCategoryDeliverability,
			Severity:    severity,
			Surface:     models.AdvisorSurfaceMailboxes,
			EntityType:  "email_account",
			EntityID:    ref(m.ID),
			EntityLabel: m.Email,
			Impact:      clampImpact(60 + 10*len(missing)),
			Title:       fmt.Sprintf("%s is missing %s", m.Email, joinWords(missing)),
			Detail: fmt.Sprintf(
				"The sending domain for %s has no valid %s record. Google's bulk sender rules require SPF, DKIM, and DMARC to be aligned, so unauthenticated cold mail is filtered on arrival regardless of how good the copy is.%s",
				m.Email, joinWords(missing), gate),
			Remedy:   "Add the missing DNS records at the domain's registrar, then re-check the domain from the mailbox to clear it immediately. This is the highest-leverage deliverability fix available and it costs nothing.",
			Steps:    domainAuthSteps(missing),
			Snippets: domainAuthSnippets(m, missing),
			Evidence: map[string]any{
				"mailbox":                m.Email,
				"missing":                missing,
				"spf":                    m.AuthSPF,
				"dkim":                   m.AuthDKIM,
				"dmarc":                  m.AuthDMARC,
				"dmarc_policy":           m.AuthDMARCPolicy,
				"currently_sending_cold": m.InActiveCampaign,
				"sending_blocked":        blocked,
			},
		})
	}
	return out
}

// domainAuthGateNote describes what the send gate is doing to this mailbox
// right now, as a sentence to append to the finding, plus whether it is already
// blocked. Only a domain in the "failing" state has a gate story at all: a
// domain merely missing DKIM still passes, because DKIM selectors are not
// discoverable from DNS and never gate on their own.
func domainAuthGateNote(s *repository.AdvisorSnapshot, m repository.AdvisorMailbox) (string, bool) {
	if !s.DomainAuthEnforced || m.AuthState != models.AuthStateFailing || m.AuthFailingSince == nil {
		return "", false
	}
	blockedAt := m.AuthFailingSince.Add(s.DomainAuthGrace)
	if !s.Now.Before(blockedAt) {
		return " Cold sending and warmup from this mailbox are stopped until the records are in place.", true
	}
	return fmt.Sprintf(
		" Cold sending and warmup from this mailbox stop on %s unless the records are in place by then.",
		blockedAt.UTC().Format("2 January at 15:04 MST")), false
}

func detectSharedTrackingDomain(s *repository.AdvisorSnapshot) []Finding {
	out := []Finding{}
	for _, m := range s.Mailboxes {
		if !m.InActiveCampaign {
			continue
		}
		if m.TrackingDomain != "" && m.TrackingDomainVerified {
			continue
		}

		title := fmt.Sprintf("%s has no verified tracking domain", m.Email)
		detail := fmt.Sprintf(
			"%s is sending cold campaigns without its own verified tracking domain, so open pixels and click links go out on a shared host. That host's reputation is the sum of everyone else using it, which is a deliverability risk you do not control.",
			m.Email)
		if m.TrackingDomain != "" && !m.TrackingDomainVerified {
			title = fmt.Sprintf("%s has an unverified tracking domain", m.Email)
			detail = fmt.Sprintf(
				"%s is set to use %s for tracking, but the CNAME has not verified, so links fall back to the shared host. Tracked links on a shared domain inherit every other sender's reputation on it.",
				m.Email, m.TrackingDomain)
		}

		out = append(out, Finding{
			Key:         "mailbox_shared_tracking_domain",
			GroupTitle:  "{count} mailboxes are tracking on a shared domain",
			Category:    models.AdvisorCategoryDeliverability,
			Severity:    models.AdvisorMedium,
			Surface:     models.AdvisorSurfaceMailboxes,
			EntityType:  "email_account",
			EntityID:    ref(m.ID),
			EntityLabel: m.Email,
			Impact:      clampImpact(30 + m.ColdSent7d/10),
			Title:       title,
			Detail:      detail,
			Remedy:      "Point a subdomain of your sending domain at the tracking host with a CNAME, then set it as this mailbox's tracking domain.",
			Steps:       trackingDomainSteps(),
			Snippets:    trackingDomainSnippets(m, s.TrackingHost),
			Evidence: map[string]any{
				"mailbox":         m.Email,
				"tracking_domain": m.TrackingDomain,
				"verified":        m.TrackingDomainVerified,
				"cold_sends_7d":   m.ColdSent7d,
			},
		})
	}
	return out
}

// domainAuthSteps builds the how-to for the specific records that are missing.
// A generic "set up SPF, DKIM and DMARC" is exactly the advice someone who is
// already stuck has read five times.
func domainAuthSteps(missing []string) []string {
	steps := []string{"Open your DNS provider for the domain this mailbox sends from. The records to add are below, ready to paste."}
	for _, record := range missing {
		switch record {
		case "SPF":
			steps = append(steps, "Add the SPF record as a TXT record at the root of the domain. If an SPF record already exists, edit that one instead of adding a second: two SPF records is a failure, not a backup.")
		case "DKIM":
			steps = append(steps, "Generate a DKIM key in your mail provider's admin console, publish the record it gives you at the host below, then turn signing on. Publishing the key and enabling signing are two separate switches and missing the second is the usual cause.")
		case "DMARC":
			steps = append(steps, "Add the DMARC record as a TXT record at the _dmarc host. It starts at p=none, which monitors without affecting delivery; tighten it to quarantine once a few weeks of reports look clean.")
		}
	}
	return append(steps,
		"Give DNS up to a few hours to propagate. Most providers are much faster.",
		"Send yourself a test message and check the headers show a pass for each record. Warmbly re-checks the domain on its own schedule and this clears itself once it does.",
	)
}

func trackingDomainSteps() []string {
	return []string{
		"Pick a subdomain of the domain you send from. It has to be that domain: a tracking subdomain on an unrelated domain inherits none of its reputation and buys you nothing.",
		"At your DNS provider, add the CNAME below.",
		"Open the mailbox's settings, put the subdomain in the tracking domain field, and save.",
		"Wait for it to verify. DNS usually propagates in minutes, but give it up to an hour before assuming it failed.",
		"Links already sent keep working. New sends pick up the new domain automatically.",
	}
}

// trackingDomainSnippets renders the CNAME. The target is this install's own
// tracking host, so a self-hosted deployment gets its own value rather than a
// hardcoded cloud one.
func trackingDomainSnippets(m repository.AdvisorMailbox, trackingHost string) []models.AdvisorSnippet {
	host := m.TrackingDomain
	if host == "" {
		host = "track." + emailDomain(m.Email)
	}
	out := []models.AdvisorSnippet{
		{Label: "Record type", Value: "CNAME"},
		{Label: "Host", Value: host, Note: "A subdomain of the domain this mailbox sends from."},
	}
	if trackingHost != "" {
		out = append(out, models.AdvisorSnippet{Label: "Points to", Value: trackingHost})
	} else {
		out = append(out, models.AdvisorSnippet{
			Label: "Points to",
			Value: "",
			Note:  "This install has no tracking host configured, so there is nothing to point at yet. Set TRACKING_DOMAIN on the server first.",
		})
	}
	return out
}

// emailDomain is the sending domain for a mailbox address.
func emailDomain(email string) string {
	if i := strings.LastIndex(email, "@"); i >= 0 && i+1 < len(email) {
		return email[i+1:]
	}
	return "yourdomain.com"
}

// spfInclude is the provider's SPF include mechanism. An unrecognised provider
// gets no guess: a wrong include is worse than an obvious blank, because it
// looks finished.
func spfInclude(provider string) (string, bool) {
	switch provider {
	case "gmail", "google":
		return "include:_spf.google.com", true
	case "outlook", "office365", "microsoft":
		return "include:spf.protection.outlook.com", true
	}
	return "", false
}

// dkimHost is where the provider expects its DKIM record. The value itself is
// generated per domain in the provider's console, so it is never templated here.
func dkimHost(provider, domain string) (string, string) {
	switch provider {
	case "gmail", "google":
		return "google._domainkey." + domain, "Google Admin > Apps > Google Workspace > Gmail > Authenticate email generates the TXT value."
	case "outlook", "office365", "microsoft":
		return "selector1._domainkey." + domain, "Microsoft 365 Defender > Policies > DKIM gives you two CNAME records (selector1 and selector2) pointing at your tenant."
	}
	return "yourselector._domainkey." + domain, "Your provider's admin console generates both the selector name and the value."
}

// domainAuthSnippets renders the records to paste for whatever is missing.
func domainAuthSnippets(m repository.AdvisorMailbox, missing []string) []models.AdvisorSnippet {
	domain := emailDomain(m.Email)
	out := []models.AdvisorSnippet{}

	for _, record := range missing {
		switch record {
		case "SPF":
			include, known := spfInclude(m.Provider)
			value := "v=spf1 " + include + " ~all"
			note := "Host is the domain root, which some DNS providers write as @ and others leave blank."
			if !known {
				value = "v=spf1 include:YOUR-PROVIDER-SPF-HOST ~all"
				note = "Replace the include with your mail provider's published SPF host. Host is the domain root, written as @ or left blank depending on your DNS provider."
			}
			out = append(out,
				models.AdvisorSnippet{Label: "SPF record type", Value: "TXT"},
				models.AdvisorSnippet{Label: "SPF host", Value: domain, Note: note},
				models.AdvisorSnippet{Label: "SPF value", Value: value},
			)
		case "DKIM":
			host, note := dkimHost(m.Provider, domain)
			out = append(out,
				models.AdvisorSnippet{Label: "DKIM host", Value: host, Note: note},
			)
		case "DMARC":
			// p=none deliberately: a first DMARC record that quarantines can
			// silently bin legitimate mail from a service nobody remembered was
			// sending as this domain. Monitor first, tighten later.
			out = append(out,
				models.AdvisorSnippet{Label: "DMARC record type", Value: "TXT"},
				models.AdvisorSnippet{Label: "DMARC host", Value: "_dmarc." + domain},
				models.AdvisorSnippet{
					Label: "DMARC value",
					Value: fmt.Sprintf("v=DMARC1; p=none; rua=mailto:dmarc@%s; adkim=r; aspf=r; pct=100", domain),
					Note:  "Starts in monitor-only mode. Change p=none to p=quarantine once the reports show your legitimate mail passing.",
				},
			)
		}
	}
	return out
}

// --- small text helpers ---------------------------------------------------

func plural(n int, one, many string) string {
	if n == 1 {
		return fmt.Sprintf("%d %s", n, one)
	}
	return fmt.Sprintf("%d %s", n, many)
}

// joinWords renders a list as "SPF, DKIM and DMARC".
func joinWords(items []string) string {
	switch len(items) {
	case 0:
		return ""
	case 1:
		return items[0]
	case 2:
		return items[0] + " and " + items[1]
	}
	out := ""
	for i, it := range items[:len(items)-1] {
		if i > 0 {
			out += ", "
		}
		out += it
	}
	return out + " and " + items[len(items)-1]
}
