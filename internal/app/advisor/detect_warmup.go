package advisor

import (
	"fmt"

	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/repository"
)

// warmupDetectors cover the positive-signal side of reputation: whether a
// mailbox is generating normal, conversational traffic alongside its cold
// sending, and whether it is still welcome in the shared pool.
func warmupDetectors() []Detector {
	return []Detector{
		{
			Key:      "warmup_off_while_sending",
			Category: models.AdvisorCategoryWarmup,
			About:    "A mailbox running cold campaigns with warmup switched off. Warmup is what generates the positive engagement that offsets cold traffic; without it a mailbox's only signal to providers is unsolicited mail from strangers.",
			Run:      detectWarmupOffWhileSending,
		},
		{
			Key:      "warmup_paused",
			Category: models.AdvisorCategoryWarmup,
			About:    "Warmup left paused on a mailbox. Pausing is a deliberate, usually temporary action, and a pause nobody resumed removes the mailbox's positive signal without anyone noticing.",
			Run:      detectWarmupPaused,
		},
		{
			Key:      "cloud_standing_unavailable",
			Category: models.AdvisorCategoryWarmup,
			About:    "A Cloud-linked mailbox with missing or stale status evidence. New sends are held until a recognized Cloud observation arrives; this is an availability hold, not a reputation ban or a provider authentication failure.",
			Run:      detectCloudStandingUnavailable,
		},
		{
			Key:      "warmup_pool_throttled",
			Category: models.AdvisorCategoryWarmup,
			About:    "A mailbox whose warmup and cold sending are temporarily reduced by its health band. Throttling keeps the mailbox in the pool, including during re-entry probation. Placement throttles recover automatically through watch as placement improves. Throttling is not pool exclusion, a connection failure, or a reason on its own to hold campaigns or require manual re-entry. Follow the recommended action in the evidence.",
			Run:      detectWarmupPoolThrottled,
		},
		{
			Key:      "warmup_pool_blocked",
			Category: models.AdvisorCategoryWarmup,
			About:    "A mailbox quarantined or blocked from the shared warmup pool for its spam placement, complaint or bounce rate, or for tampering with warmup mail it received. Pool standing is the platform's own read on whether a mailbox is safe to keep in shared reputation surfaces.",
			Run:      detectWarmupPoolBlocked,
		},
		{
			Key:      "warmup_mail_not_arriving",
			Category: models.AdvisorCategoryWarmup,
			About:    "A mailbox the pool's warmup mail no longer reaches. When none of a week's warmup mail from several workspaces arrives where Warmbly can see it, the pool stops sending to the mailbox, so it receives no warmup until mail gets through again.",
			Run:      detectWarmupMailNotArriving,
		},
	}
}

func detectWarmupMailNotArriving(s *repository.AdvisorSnapshot) []Finding {
	out := []Finding{}
	for _, m := range s.Mailboxes {
		if !m.WarmupActive || !m.WarmupUnreachable() {
			continue
		}
		out = append(out, Finding{
			Key:         "warmup_mail_not_arriving",
			GroupTitle:  "{count} mailboxes are not receiving warmup mail",
			Category:    models.AdvisorCategoryWarmup,
			Severity:    models.AdvisorHigh,
			Surface:     models.AdvisorSurfaceMailboxes,
			EntityType:  "email_account",
			EntityID:    ref(m.ID),
			EntityLabel: m.Email,
			Impact:      clampImpact(60 + m.WarmupDelivered7d),
			Title:       fmt.Sprintf("%s is not receiving warmup mail", m.Email),
			Detail: fmt.Sprintf(
				"None of the %d warmup emails other mailboxes sent to %s in the last week arrived where Warmbly could see them. The pool has stopped sending to it, so it gets no warmup until mail reaches it again.",
				m.WarmupDelivered7d, m.Email),
			Remedy: "Make sure mail for this address reaches the inbox Warmbly is connected to. It rejoins the pool on its own as soon as one warmup email arrives.",
			Steps: []string{
				"Check the domain's MX records. A domain with no MX record, or one pointing at another server, never receives mail at this mailbox.",
				"Send it a test email from an outside account and confirm it lands in the inbox Warmbly syncs.",
				"Look for a forwarding rule, a filter that moves mail into an unsynced folder, or a server that refuses mail from new domains.",
				"Open the mailbox's settings in Warmbly and confirm the IMAP server is the one this address's mail is delivered to.",
			},
			Evidence: map[string]any{
				"mailbox":           m.Email,
				"warmup_delivered":  m.WarmupDelivered7d,
				"warmup_arrived":    m.WarmupArrived7d,
				"sender_workspaces": m.WarmupLostBy7d,
				"window_days":       7,
				"pool_type":         m.WarmupPoolType,
			},
		})
	}
	return out
}

func detectWarmupOffWhileSending(s *repository.AdvisorSnapshot) []Finding {
	out := []Finding{}
	for _, m := range s.Mailboxes {
		if !m.SendingCold() || m.ColdSent7d == 0 || m.WarmupActive || m.WarmupPaused || s.WarmupUnavailable {
			continue
		}

		// A brand-new mailbox sending cold with no warmup at all is the worst
		// version of this: no reputation, and nothing building one.
		severity := models.AdvisorHigh
		if m.AgeDays < newMailboxDays {
			severity = models.AdvisorCritical
		}

		out = append(out, Finding{
			Key:         "warmup_off_while_sending",
			GroupTitle:  "{count} mailboxes are sending cold mail with no warmup",
			Category:    models.AdvisorCategoryWarmup,
			Severity:    severity,
			Surface:     models.AdvisorSurfaceMailboxes,
			EntityType:  "email_account",
			EntityID:    ref(m.ID),
			EntityLabel: m.Email,
			Impact:      clampImpact(65 + m.ColdSent7d/10),
			Title:       fmt.Sprintf("%s is sending cold mail with no warmup", m.Email),
			Detail: fmt.Sprintf(
				"%s sent %s in the last week with warmup switched off. Warmup is what produces the opens, replies, and inbox moves that offset cold traffic; without it, the only thing providers see from this mailbox is unsolicited mail to strangers.",
				m.Email, plural(m.ColdSent7d, "cold email", "cold emails")),
			Remedy: "Turn warmup on and leave it on. It should keep running after campaigns start, not stop once the mailbox is considered ready.",
			Evidence: map[string]any{
				"mailbox":       m.Email,
				"cold_sends_7d": m.ColdSent7d,
				"age_days":      m.AgeDays,
				"daily_cap":     m.CampaignLimit,
			},
			Action: withUndo(toolAction("set_mailbox_warmup",
				"Turn warmup on",
				map[string]any{"email_account_id": m.ID.String(), "action": "start"},
				change("Warmup", "off", fmt.Sprintf("on, starting at %d/day", m.WarmupBase)),
			), map[string]any{"email_account_id": m.ID.String(), "action": "stop"}),
		})
	}
	return out
}

func detectWarmupPaused(s *repository.AdvisorSnapshot) []Finding {
	out := []Finding{}
	for _, m := range s.Mailboxes {
		// Resuming is not an option the plan offers, as after a trial ends.
		// A pause is deliberate; it only costs anything while the mailbox sends cold mail without it.
		if !m.WarmupPaused || s.WarmupUnavailable || !m.SendingCold() || m.ColdSent7d == 0 {
			continue
		}
		severity := models.AdvisorHigh

		out = append(out, Finding{
			Key:         "warmup_paused",
			GroupTitle:  "Warmup is still paused on {count} mailboxes",
			Category:    models.AdvisorCategoryWarmup,
			Severity:    severity,
			Surface:     models.AdvisorSurfaceMailboxes,
			EntityType:  "email_account",
			EntityID:    ref(m.ID),
			EntityLabel: m.Email,
			Impact:      clampImpact(35 + m.ColdSent7d/10),
			Title:       fmt.Sprintf("Warmup is still paused on %s", m.Email),
			Detail: fmt.Sprintf(
				"Warmup on %s is paused%s. Pausing keeps the ramp progress, so resuming picks up where it left off rather than starting over.",
				m.Email,
				map[bool]string{true: " while the mailbox is running cold campaigns", false: ""}[m.SendingCold()]),
			Remedy: "Resume warmup unless you paused it deliberately for a reason that still applies.",
			Evidence: map[string]any{
				"mailbox":                m.Email,
				"currently_sending_cold": m.SendingCold(),
				"cold_sends_7d":          m.ColdSent7d,
			},
			Action: withUndo(toolAction("set_mailbox_warmup",
				"Resume warmup",
				map[string]any{"email_account_id": m.ID.String(), "action": "resume"},
				change("Warmup", "paused", "running (ramp progress preserved)"),
			), map[string]any{"email_account_id": m.ID.String(), "action": "pause"}),
		})
	}
	return out
}

func detectCloudStandingUnavailable(s *repository.AdvisorSnapshot) []Finding {
	out := []Finding{}
	for _, m := range s.Mailboxes {
		if m.PoolHealthReason == models.WarmupReasonCloudEvidenceUnavailable {
			out = append(out, Finding{
				Key: "cloud_standing_unavailable", GroupTitle: "{count} mailboxes need a fresh Cloud pool status",
				Category: models.AdvisorCategoryWarmup, Severity: models.AdvisorHigh, Surface: models.AdvisorSurfaceMailboxes,
				EntityType: "email_account", EntityID: ref(m.ID), EntityLabel: m.Email, Impact: 70,
				Title:    fmt.Sprintf("Cloud pool status for %s is unavailable", m.Email),
				Detail:   "This instance has no current, recognized Cloud pool status for this mailbox. New sending is held until the status is refreshed. This is not evidence of spam, rejected credentials, or a Cloud pool ban; Cloud warmup may still be running independently.",
				Remedy:   "Open Settings > Warmbly Cloud to refresh the mailbox status. If Cloud cannot be reached, check the connection and the consumer's cloud_standing_sync job. Keep the backend and consumer on the same release. Do not disconnect or re-enroll the mailbox.",
				Steps:    []string{"Open Settings > Warmbly Cloud and reload the mailbox list to obtain a current status.", "If the status stays unavailable, check Cloud connectivity and the consumer's cloud_standing_sync job. Do not disconnect or re-enroll the mailbox."},
				Evidence: map[string]any{"mailbox": m.Email, "pool_state": m.PoolHealth, "pool_health_reason": m.PoolHealthReason},
			})
		}
	}
	return out
}

func detectWarmupPoolThrottled(s *repository.AdvisorSnapshot) []Finding {
	out := []Finding{}
	for _, m := range s.Mailboxes {
		if m.PoolHealth != string(models.WarmupHealthThrottled) || m.PoolHealthReason == models.WarmupReasonCloudEvidenceUnavailable {
			continue
		}
		remedy := "Keep warmup running and let Warmbly manage the reduced sending pace. Do not raise the daily cap to bypass the slowdown. Recovery follows the reason shown, without manual pool re-entry; separate ramp limits still apply."
		out = append(out, Finding{
			Key:         "warmup_pool_throttled",
			GroupTitle:  "{count} mailboxes have temporarily reduced sending",
			Category:    models.AdvisorCategoryWarmup,
			Severity:    models.AdvisorHigh,
			Surface:     models.AdvisorSurfaceMailboxes,
			EntityType:  "email_account",
			EntityID:    ref(m.ID),
			EntityLabel: m.Email,
			Impact:      clampImpact(40 + int(m.PoolHealthScore)/4),
			Title:       fmt.Sprintf("%s has temporarily reduced sending", m.Email),
			Detail: fmt.Sprintf(
				"Warmbly has reduced warmup and cold sending for %s to half volume with wider spacing. This status describes a reduced sending pace, not pool exclusion or a connection failure. %s",
				m.Email, m.PoolHealthReason),
			Remedy: remedy,
			Steps: []string{
				"Keep warmup enabled so recent placement evidence can show recovery.",
				"Leave the daily cap at or below its current setting while Warmbly manages the reduced pace.",
				"Review recent campaign targeting and copy, and investigate any confirmed authentication, complaint or bounce issues separately.",
				"For a placement throttle, the half-volume limit steps down to watch below 15% spam placement, and watch lifts below 7.5%. Other cooldowns follow the reason shown. Warmbly re-evaluates these limits automatically.",
			},
			Evidence: map[string]any{
				"mailbox":                m.Email,
				"pool_state":             m.PoolHealth,
				"pool_health_reason":     m.PoolHealthReason,
				"pool_type":              m.WarmupPoolType,
				"currently_sending_cold": m.SendingCold(),
				"recommended_action":     remedy,
			},
		})
	}
	return out
}

func detectWarmupPoolBlocked(s *repository.AdvisorSnapshot) []Finding {
	out := []Finding{}
	for _, m := range s.Mailboxes {
		if m.PoolHealthReason == models.WarmupReasonCloudEvidenceUnavailable {
			continue
		}
		// Recoverable bands also carry blocked_until; their timer is not pool exclusion.
		switch models.WarmupHealthState(m.PoolHealth) {
		case models.WarmupHealthHealthy, models.WarmupHealthWatch, models.WarmupHealthThrottled:
			continue
		}
		bad := m.PoolBlocked ||
			m.PoolHealth == "quarantined" || m.PoolHealth == "blocked"
		if !bad {
			continue
		}

		// A quarantined mailbox that is still running cold campaigns is the
		// dangerous case: the platform has already judged it unsafe for shared
		// reputation surfaces, and it is still mailing strangers.
		severity := models.AdvisorHigh
		if m.SendingCold() {
			severity = models.AdvisorCritical
		}

		state := m.PoolHealth
		if state == "" {
			state = "blocked"
		}

		f := Finding{
			Key:         "warmup_pool_blocked",
			GroupTitle:  "{count} mailboxes have lost their warmup pool standing",
			Category:    models.AdvisorCategoryWarmup,
			Severity:    severity,
			Surface:     models.AdvisorSurfaceMailboxes,
			EntityType:  "email_account",
			EntityID:    ref(m.ID),
			EntityLabel: m.Email,
			Impact:      clampImpact(70 + int(m.PoolHealthScore)/4),
			Title:       fmt.Sprintf("%s is %s in the warmup pool", m.Email, state),
			Detail: fmt.Sprintf(
				"The warmup pool has marked %s as %s%s. Pool standing is the platform's own read on whether a mailbox is safe to keep in shared reputation surfaces, and it moved before mailbox providers did%s.",
				m.Email, state,
				map[bool]string{true: " (" + m.PoolHealthReason + ")", false: ""}[m.PoolHealthReason != ""],
				map[bool]string{true: ". This mailbox is still running cold campaigns", false: ""}[m.SendingCold()]),
			Remedy: "Stop cold sending from this mailbox until it requalifies. Re-entry needs healthy authentication, no recent complaints or hard-bounce spikes, and spam placement back under 10% on a fresh sample.",
			Steps: []string{
				"Take the mailbox off every active campaign. Nothing else matters while it is still mailing strangers.",
				"Confirm SPF, DKIM and DMARC all pass on its sending domain. A failing record is the most common reason a mailbox reaches this state.",
				"Look at what it sent recently. Pool standing drops on complaints, spam placement and hard bounces, and one bad list usually explains all three.",
				"Leave warmup running. It is what rebuilds standing, and the cooldown is measured on behaviour, not just elapsed time.",
				"When it requalifies, bring it back at 5 to 10 sends a day and hold there for a week before going further.",
			},
			Evidence: map[string]any{
				"mailbox":                m.Email,
				"pool_state":             state,
				"pool_health_reason":     m.PoolHealthReason,
				"pool_type":              m.WarmupPoolType,
				"currently_sending_cold": m.SendingCold(),
			},
		}
		if m.SendingCold() {
			f.Action = mailboxHoldAction(m.ID, "Hold this mailbox out of campaigns")
		}
		out = append(out, f)
	}
	return out
}
