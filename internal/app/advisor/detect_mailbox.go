package advisor

import (
	"fmt"

	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/repository"
)

// mailboxDetectors cover per-mailbox sending configuration: how much a mailbox
// is allowed to send, how fast, and whether it is healthy enough to be sending
// at all.
func mailboxDetectors() []Detector {
	return []Detector{
		{
			Key:      "mailbox_cap_too_high",
			Category: models.AdvisorCategoryMailbox,
			About:    "A mailbox's daily cold-send cap relative to the platform's safe band. The documented default is 50/day, and anything above it should require positive reputation signals and explicit review rather than being raised casually.",
			Run:      detectCapTooHigh,
		},
		{
			Key:      "mailbox_new_ramping_fast",
			Category: models.AdvisorCategoryMailbox,
			About:    "A recently connected mailbox sending at full volume. Deliverability guidance is consistent that a new sending identity should start around 10-20/day and ramp only while performance stays healthy; jumping straight to full volume is the most common way to burn a new mailbox.",
			Run:      detectNewMailboxRampingFast,
		},
		{
			Key:      "mailbox_gap_too_short",
			Category: models.AdvisorCategoryMailbox,
			About:    "The minimum gap between sends from one mailbox. Bursty sending from a single mailbox is a pattern filters recognise even when the daily total is modest.",
			Run:      detectGapTooShort,
		},
		{
			Key:      "mailbox_errors_unresolved",
			Category: models.AdvisorCategoryMailbox,
			About:    "Unresolved connection or send errors on a mailbox that campaigns still route through. A mailbox erroring silently drops volume from the campaigns depending on it.",
			Run:      detectMailboxErrors,
		},
		{
			Key:      "mailbox_concentration",
			Category: models.AdvisorCategoryMailbox,
			About:    "How many mailboxes carry the org's cold volume. Spreading sending across more mailboxes and more sending identities is the core safety property of the platform; concentrating it into one or two mailboxes gives up that protection.",
			Run:      detectMailboxConcentration,
		},
		{
			Key:      "mailbox_inactive_in_campaign",
			Category: models.AdvisorCategoryMailbox,
			About:    "A mailbox that is inactive or erroring but still attached to a running campaign, so the campaign quietly sends less than it is configured to.",
			Run:      detectInactiveMailboxInCampaign,
		},
	}
}

func detectCapTooHigh(s *repository.AdvisorSnapshot) []Finding {
	out := []Finding{}
	for _, m := range s.Mailboxes {
		if m.CampaignLimit <= safeColdCapBand {
			continue
		}
		// A high cap on a mailbox with clean signals is a judgement call the
		// user is entitled to make; a high cap on a mailbox that is already
		// bouncing or complaining is not.
		bounceRate := rate(m.Bounces30d, m.ColdSent30d)
		complaintRate := rate(m.Complaints30d, m.ColdSent30d)
		proven := m.ColdSent30d >= minSendsForComplaintRate &&
			bounceRate < bounceRateWarn && complaintRate < complaintRateWarn

		severity := models.AdvisorMedium
		detail := fmt.Sprintf(
			"%s is capped at %d cold emails a day, above the %d/day safe band. Volume above the default is meant to follow positive reputation signals, not lead them.",
			m.Email, m.CampaignLimit, safeColdCapBand)
		if proven {
			// Proven mailbox: this is a note, not an alarm.
			severity = models.AdvisorLow
			detail = fmt.Sprintf(
				"%s is capped at %d cold emails a day, above the %d/day safe band. Its bounce and complaint rates are clean over %d sends, so the volume is currently holding, but it leaves no headroom if the list quality slips.",
				m.Email, m.CampaignLimit, safeColdCapBand, m.ColdSent30d)
		}

		out = append(out, Finding{
			Key:         "mailbox_cap_too_high",
			GroupTitle:  "{count} mailboxes are capped above the safe band",
			Category:    models.AdvisorCategoryMailbox,
			Severity:    severity,
			Surface:     models.AdvisorSurfaceMailboxes,
			EntityType:  "email_account",
			EntityID:    ref(m.ID),
			EntityLabel: m.Email,
			Impact:      clampImpact(20 + (m.CampaignLimit - safeColdCapBand)),
			Title:       fmt.Sprintf("%s is capped above the safe band", m.Email),
			Detail:      detail,
			Remedy:      fmt.Sprintf("Bring the cap back to %d/day unless you have reviewed this mailbox's complaint and placement numbers specifically and decided to carry the risk.", defaultColdCap),
			Evidence: map[string]any{
				"mailbox":             m.Email,
				"daily_cap":           m.CampaignLimit,
				"safe_band":           safeColdCapBand,
				"sends_30d":           m.ColdSent30d,
				"bounce_rate_percent": band(bounceRate),
				"proven":              proven,
			},
			Action: withUndo(mailboxAction(m.ID,
				fmt.Sprintf("Set the cap to %d/day", defaultColdCap),
				map[string]any{"campaign_limit": defaultColdCap},
				change("Daily cold cap", fmt.Sprintf("%d/day", m.CampaignLimit), fmt.Sprintf("%d/day", defaultColdCap)),
			), map[string]any{"email_account_id": m.ID.String(), "campaign_limit": m.CampaignLimit}),
		})
	}
	return out
}

func detectNewMailboxRampingFast(s *repository.AdvisorSnapshot) []Finding {
	out := []Finding{}
	for _, m := range s.Mailboxes {
		if m.AgeDays >= newMailboxDays || !m.InActiveCampaign {
			continue
		}
		if m.CampaignLimit <= newMailboxSafeCap {
			continue
		}

		out = append(out, Finding{
			Key:         "mailbox_new_ramping_fast",
			GroupTitle:  "{count} new mailboxes are sending at full volume",
			Category:    models.AdvisorCategoryMailbox,
			Severity:    models.AdvisorHigh,
			Surface:     models.AdvisorSurfaceMailboxes,
			EntityType:  "email_account",
			EntityID:    ref(m.ID),
			EntityLabel: m.Email,
			Impact:      clampImpact(70 - m.AgeDays),
			Title:       fmt.Sprintf("%s is sending at full volume after %s", m.Email, plural(m.AgeDays, "day", "days")),
			Detail: fmt.Sprintf(
				"%s was connected %s ago and is already capped at %d cold emails a day. A new sending identity has no reputation to spend yet; the safe start is 10-20/day, ramping only while bounce and complaint rates stay clean.",
				m.Email, plural(m.AgeDays, "day", "days"), m.CampaignLimit),
			Remedy: fmt.Sprintf("Drop this mailbox to %d/day and raise it gradually over the next few weeks. Warmup should be running the whole time.", newMailboxSafeCap),
			Evidence: map[string]any{
				"mailbox":        m.Email,
				"age_days":       m.AgeDays,
				"daily_cap":      m.CampaignLimit,
				"safe_start_cap": newMailboxSafeCap,
				"warmup_running": m.WarmupActive,
				"cold_sends_7d":  m.ColdSent7d,
			},
			Action: withUndo(mailboxAction(m.ID,
				fmt.Sprintf("Start at %d/day instead", newMailboxSafeCap),
				map[string]any{"campaign_limit": newMailboxSafeCap},
				change("Daily cold cap", fmt.Sprintf("%d/day", m.CampaignLimit), fmt.Sprintf("%d/day", newMailboxSafeCap)),
			), map[string]any{"email_account_id": m.ID.String(), "campaign_limit": m.CampaignLimit}),
		})
	}
	return out
}

func detectGapTooShort(s *repository.AdvisorSnapshot) []Finding {
	out := []Finding{}
	for _, m := range s.Mailboxes {
		if !m.InActiveCampaign || m.MinWaitTime >= minSafeGapSeconds {
			continue
		}

		out = append(out, Finding{
			Key:         "mailbox_gap_too_short",
			GroupTitle:  "{count} mailboxes are sending in bursts",
			Category:    models.AdvisorCategoryMailbox,
			Severity:    models.AdvisorMedium,
			Surface:     models.AdvisorSurfaceMailboxes,
			EntityType:  "email_account",
			EntityID:    ref(m.ID),
			EntityLabel: m.Email,
			Impact:      clampImpact(40 + (minSafeGapSeconds-m.MinWaitTime)/10),
			Title:       fmt.Sprintf("%s sends in bursts", m.Email),
			Detail: fmt.Sprintf(
				"%s waits only %s between sends, against a platform default of %d minutes. Bursty sending from one mailbox is a pattern filters recognise even when the daily total is modest, because real people do not send twenty emails in ten minutes.",
				m.Email, humanSeconds(m.MinWaitTime), defaultMinGap/60),
			Remedy: fmt.Sprintf("Widen the gap to at least %d minutes. The daily total stays the same; it just spreads out across the sending window.", defaultMinGap/60),
			Evidence: map[string]any{
				"mailbox":             m.Email,
				"min_gap_seconds":     m.MinWaitTime,
				"recommended_seconds": defaultMinGap,
				"daily_cap":           m.CampaignLimit,
			},
			Action: withUndo(mailboxAction(m.ID,
				fmt.Sprintf("Widen the gap to %d minutes", defaultMinGap/60),
				map[string]any{"min_wait_time": defaultMinGap},
				change("Minimum gap between sends", humanSeconds(m.MinWaitTime), humanSeconds(defaultMinGap)),
			), map[string]any{"email_account_id": m.ID.String(), "min_wait_time": m.MinWaitTime}),
		})
	}
	return out
}

func detectMailboxErrors(s *repository.AdvisorSnapshot) []Finding {
	out := []Finding{}
	for _, m := range s.Mailboxes {
		if m.UnresolvedErrs == 0 {
			continue
		}
		severity := models.AdvisorMedium
		if m.InActiveCampaign {
			severity = models.AdvisorHigh
		}

		out = append(out, Finding{
			Key:         "mailbox_errors_unresolved",
			GroupTitle:  "{count} mailboxes have errors nobody has cleared",
			Category:    models.AdvisorCategoryMailbox,
			Severity:    severity,
			Surface:     models.AdvisorSurfaceMailboxes,
			EntityType:  "email_account",
			EntityID:    ref(m.ID),
			EntityLabel: m.Email,
			Impact:      clampImpact(30 + m.UnresolvedErrs*5),
			Title:       fmt.Sprintf("%s has %s that nobody has cleared", m.Email, plural(m.UnresolvedErrs, "error", "errors")),
			Detail: fmt.Sprintf(
				"%s has %s from the last 7 days still unresolved%s. Send and sync errors do not surface as failures in campaign reporting, so a mailbox in this state quietly delivers less than the campaign asked for.",
				m.Email, plural(m.UnresolvedErrs, "error", "errors"),
				map[bool]string{true: " while it is attached to a running campaign", false: ""}[m.InActiveCampaign]),
			Remedy: "Open the mailbox and work through the errors. Authentication failures usually mean a reconnect; rate-limit errors mean the provider is pushing back and the cap should come down.",
			Evidence: map[string]any{
				"mailbox":                m.Email,
				"unresolved_errors_7d":   m.UnresolvedErrs,
				"currently_sending_cold": m.InActiveCampaign,
				"status":                 m.Status,
			},
		})
	}
	return out
}

func detectMailboxConcentration(s *repository.AdvisorSnapshot) []Finding {
	// Only meaningful once the org is actually sending at volume.
	sending := 0
	totalVolume := 0
	for _, m := range s.Mailboxes {
		if m.ColdSent7d > 0 {
			sending++
			totalVolume += m.ColdSent7d
		}
	}
	if s.Org.RunningCampaigns == 0 || totalVolume < minSendsForEngagement {
		return nil
	}

	// The planning heuristic in the sending policy: a mailbox at the default
	// cap carries ~50/day, so weekly volume divided by the safe per-mailbox
	// weekly total is how many mailboxes this volume really wants.
	wantMailboxes := totalVolume / (defaultColdCap * 7)
	if wantMailboxes < 1 {
		wantMailboxes = 1
	}
	if sending >= wantMailboxes {
		return nil
	}

	perMailbox := totalVolume / maxInt(sending, 1) / 7
	return []Finding{{
		Key:        "mailbox_concentration",
		Category:   models.AdvisorCategoryMailbox,
		Severity:   models.AdvisorHigh,
		Surface:    models.AdvisorSurfaceMailboxes,
		EntityType: "",
		Impact:     clampImpact(40 + (wantMailboxes-sending)*10),
		Title:      fmt.Sprintf("%s carrying all your cold volume", plural(sending, "mailbox is", "mailboxes are")),
		Detail: fmt.Sprintf(
			"You sent %d cold emails in the last week across %s, about %d per mailbox per day. Spreading volume across more mailboxes and more sending identities is the whole safety model here: concentrating it means one reputation problem takes out all of your sending at once.",
			totalVolume, plural(sending, "mailbox", "mailboxes"), perMailbox),
		Remedy: fmt.Sprintf("Connect more mailboxes and let the campaign rotate across them. At this volume you want around %s, each staying near the default cap.", plural(wantMailboxes, "mailbox", "mailboxes")),
		Evidence: map[string]any{
			"sending_mailboxes":     sending,
			"cold_sends_7d":         totalVolume,
			"per_mailbox_per_day":   perMailbox,
			"recommended_mailboxes": wantMailboxes,
			"safe_per_mailbox_cap":  defaultColdCap,
		},
	}}
}

func detectInactiveMailboxInCampaign(s *repository.AdvisorSnapshot) []Finding {
	out := []Finding{}
	for _, m := range s.Mailboxes {
		if !m.InActiveCampaign || m.Status == "active" {
			continue
		}

		out = append(out, Finding{
			Key:         "mailbox_inactive_in_campaign",
			GroupTitle:  "{count} inactive mailboxes are attached to running campaigns",
			Category:    models.AdvisorCategoryMailbox,
			Severity:    models.AdvisorHigh,
			Surface:     models.AdvisorSurfaceCampaigns,
			EntityType:  "email_account",
			EntityID:    ref(m.ID),
			EntityLabel: m.Email,
			Impact:      60,
			Title:       fmt.Sprintf("%s is attached to a running campaign but is %s", m.Email, m.Status),
			Detail: fmt.Sprintf(
				"A running campaign routes through %s, but the mailbox is %s so nothing sends from it. The campaign is delivering less than it is configured to, and the shortfall does not show up as an error anywhere.",
				m.Email, m.Status),
			Remedy: "Either reactivate the mailbox or take it off the campaign, so the campaign's real capacity matches what it reports.",
			Evidence: map[string]any{
				"mailbox":              m.Email,
				"status":               m.Status,
				"unresolved_errors_7d": m.UnresolvedErrs,
			},
		})
	}
	return out
}

// humanSeconds renders a gap as minutes when it divides cleanly, which is how
// the setting is presented in the dashboard.
func humanSeconds(sec int) string {
	if sec >= 60 && sec%60 == 0 {
		return fmt.Sprintf("%d minutes", sec/60)
	}
	return fmt.Sprintf("%d seconds", sec)
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}
