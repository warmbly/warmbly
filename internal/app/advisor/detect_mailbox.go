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
		if proven {
			continue
		}
		// Unproven is not harm: it matters once the mailbox is bouncing or
		// complaining, or actually sending above the band without a record.
		struggling := m.ColdSent30d >= minSendsForBounceRate && (bounceRate >= bounceRateWarn || complaintRate >= complaintRateWarn)
		if !m.SendingCold() || (!struggling && m.ColdSent1d <= safeColdCapBand) {
			continue
		}

		severity := models.AdvisorMedium
		detail := fmt.Sprintf(
			"%s is capped at %d cold emails a day, above the %d/day safe band. Volume above the default is meant to follow positive reputation signals, not lead them.",
			m.Email, m.CampaignLimit, safeColdCapBand)
		if struggling {
			severity = models.AdvisorHigh
			detail = fmt.Sprintf(
				"%s is capped at %d cold emails a day, above the %d/day safe band, while its bounce or complaint rate is already past the warning line. More volume from it spends reputation it is already losing.",
				m.Email, m.CampaignLimit, safeColdCapBand)
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
				"struggling":          struggling,
			},
			Action: auto(withUndo(mailboxAction(m.ID,
				fmt.Sprintf("Set the cap to %d/day", defaultColdCap),
				map[string]any{"campaign_limit": defaultColdCap},
				change("Daily cold cap", fmt.Sprintf("%d/day", m.CampaignLimit), fmt.Sprintf("%d/day", defaultColdCap)),
			), map[string]any{"email_account_id": m.ID.String(), "campaign_limit": m.CampaignLimit})),
		})
	}
	return out
}

func detectNewMailboxRampingFast(s *repository.AdvisorSnapshot) []Finding {
	out := []Finding{}
	for _, m := range s.Mailboxes {
		// The cap alone is not volume: a warming mailbox is held to the graduation ramp below it.
		if m.AgeDays >= newMailboxDays || !m.SendingCold() || m.CampaignLimit <= newMailboxSafeCap || m.ColdSent1d <= newMailboxSafeCap {
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
			Title:       fmt.Sprintf("%s sent %d cold emails in a day after %s", m.Email, m.ColdSent1d, plural(m.AgeDays, "day", "days")),
			Detail: fmt.Sprintf(
				"%s was connected %s ago and sent %d cold emails in the last day, on a cap of %d. A new sending identity has no reputation to spend yet; the safe start is 10-20/day, ramping only while bounce and complaint rates stay clean.",
				m.Email, plural(m.AgeDays, "day", "days"), m.ColdSent1d, m.CampaignLimit),
			Remedy: fmt.Sprintf("Drop this mailbox to %d/day and raise it gradually over the next few weeks. Warmup should be running the whole time.", newMailboxSafeCap),
			Evidence: map[string]any{
				"mailbox":        m.Email,
				"age_days":       m.AgeDays,
				"daily_cap":      m.CampaignLimit,
				"safe_start_cap": newMailboxSafeCap,
				"warmup_running": m.WarmupActive,
				"cold_sends_7d":  m.ColdSent7d,
			},
			Action: auto(withUndo(mailboxAction(m.ID,
				fmt.Sprintf("Start at %d/day instead", newMailboxSafeCap),
				map[string]any{"campaign_limit": newMailboxSafeCap},
				change("Daily cold cap", fmt.Sprintf("%d/day", m.CampaignLimit), fmt.Sprintf("%d/day", newMailboxSafeCap)),
			), map[string]any{"email_account_id": m.ID.String(), "campaign_limit": m.CampaignLimit})),
		})
	}
	return out
}

func detectGapTooShort(s *repository.AdvisorSnapshot) []Finding {
	out := []Finding{}
	for _, m := range s.Mailboxes {
		if !m.SendingCold() || m.MinWaitTime >= minSafeGapSeconds {
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
			Action: auto(withUndo(mailboxAction(m.ID,
				fmt.Sprintf("Widen the gap to %d minutes", defaultMinGap/60),
				map[string]any{"min_wait_time": defaultMinGap},
				change("Minimum gap between sends", humanSeconds(m.MinWaitTime), humanSeconds(defaultMinGap)),
			), map[string]any{"email_account_id": m.ID.String(), "min_wait_time": m.MinWaitTime})),
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
		if m.SendingCold() {
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
				map[bool]string{true: " while it is attached to a running campaign", false: ""}[m.SendingCold()]),
			Remedy: "Open the mailbox and work through the errors. Authentication failures usually mean a reconnect; rate-limit errors mean the provider is pushing back and the cap should come down.",
			Steps: []string{
				"Open the mailbox and read the errors. They are grouped by kind, and the kind tells you the fix.",
				"Authentication or credential errors mean a reconnect. An expired OAuth token or a changed password will not recover on its own.",
				"Rate-limit or throttling errors mean the provider is pushing back. Lower the daily cap and widen the send gap rather than retrying into it.",
				"Connection or timeout errors are usually the host or port. Check them against your provider's current documented values.",
				"Once the cause is fixed, the errors stop accumulating and this clears on the next check.",
			},
			Evidence: map[string]any{
				"mailbox":                m.Email,
				"unresolved_errors_7d":   m.UnresolvedErrs,
				"currently_sending_cold": m.SendingCold(),
				"status":                 m.Status,
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
