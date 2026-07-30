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
			Key:      "warmup_ceiling_too_low",
			Category: models.AdvisorCategoryWarmup,
			About:    "A warmup ceiling far below the mailbox's cold cap. Warmup volume should be proportionate to cold volume, otherwise the positive signal is drowned out by the cold traffic it is supposed to balance.",
			Run:      detectWarmupCeilingTooLow,
		},
		{
			Key:      "warmup_reply_rate_low",
			Category: models.AdvisorCategoryWarmup,
			About:    "The share of warmup mail that gets replied to. Warmup works by looking like real correspondence; a low reply rate makes it one-way traffic, which is exactly the pattern it is supposed to counteract.",
			Run:      detectWarmupReplyRateLow,
		},
		{
			Key:      "warmup_pool_blocked",
			Category: models.AdvisorCategoryWarmup,
			About:    "A mailbox quarantined or blocked from the shared warmup pool for spam score or suspicious verification behaviour. Pool standing is the platform's own read on whether a mailbox is safe to keep in shared reputation surfaces.",
			Run:      detectWarmupPoolBlocked,
		},
	}
}

func detectWarmupOffWhileSending(s *repository.AdvisorSnapshot) []Finding {
	out := []Finding{}
	for _, m := range s.Mailboxes {
		if !m.InActiveCampaign || m.WarmupActive || m.WarmupPaused {
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
		if !m.WarmupPaused {
			continue
		}
		severity := models.AdvisorMedium
		if m.InActiveCampaign {
			severity = models.AdvisorHigh
		}

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
				map[bool]string{true: " while the mailbox is running cold campaigns", false: ""}[m.InActiveCampaign]),
			Remedy: "Resume warmup unless you paused it deliberately for a reason that still applies.",
			Evidence: map[string]any{
				"mailbox":                m.Email,
				"currently_sending_cold": m.InActiveCampaign,
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

func detectWarmupCeilingTooLow(s *repository.AdvisorSnapshot) []Finding {
	out := []Finding{}
	for _, m := range s.Mailboxes {
		if !m.WarmupActive || !m.InActiveCampaign {
			continue
		}
		want := int(float64(m.CampaignLimit) * warmupCeilingFloorRatio)
		if want < defaultWarmupBase {
			want = defaultWarmupBase
		}
		if m.WarmupMax >= want {
			continue
		}

		out = append(out, Finding{
			Key:         "warmup_ceiling_too_low",
			GroupTitle:  "Warmup is too small to balance cold volume on {count} mailboxes",
			Category:    models.AdvisorCategoryWarmup,
			Severity:    models.AdvisorMedium,
			Surface:     models.AdvisorSurfaceMailboxes,
			EntityType:  "email_account",
			EntityID:    ref(m.ID),
			EntityLabel: m.Email,
			Impact:      clampImpact(25 + (want - m.WarmupMax)),
			Title:       fmt.Sprintf("Warmup on %s is too small to balance its cold volume", m.Email),
			Detail: fmt.Sprintf(
				"%s can send %d cold emails a day but tops out at %d warmup emails a day. Warmup is meant to be a meaningful share of what the mailbox sends, otherwise the positive signal is swamped by the cold traffic it is supposed to balance.",
				m.Email, m.CampaignLimit, m.WarmupMax),
			Remedy: fmt.Sprintf("Raise the warmup ceiling to about %d/day, roughly half the cold cap. The ramp still gets there gradually.", want),
			Evidence: map[string]any{
				"mailbox":             m.Email,
				"warmup_ceiling":      m.WarmupMax,
				"cold_daily_cap":      m.CampaignLimit,
				"recommended_ceiling": want,
			},
			Action: withUndo(mailboxAction(m.ID,
				fmt.Sprintf("Raise the warmup ceiling to %d/day", want),
				map[string]any{"warmup_max": want},
				change("Warmup ceiling", fmt.Sprintf("%d/day", m.WarmupMax), fmt.Sprintf("%d/day", want)),
			), map[string]any{"email_account_id": m.ID.String(), "warmup_max": m.WarmupMax}),
		})
	}
	return out
}

func detectWarmupReplyRateLow(s *repository.AdvisorSnapshot) []Finding {
	out := []Finding{}
	for _, m := range s.Mailboxes {
		if !m.WarmupActive || m.WarmupReplyRate >= minWarmupReplyRate {
			continue
		}

		out = append(out, Finding{
			Key:         "warmup_reply_rate_low",
			GroupTitle:  "Warmup is nearly one-way on {count} mailboxes",
			Category:    models.AdvisorCategoryWarmup,
			Severity:    models.AdvisorLow,
			Surface:     models.AdvisorSurfaceMailboxes,
			EntityType:  "email_account",
			EntityID:    ref(m.ID),
			EntityLabel: m.Email,
			Impact:      20,
			Title:       fmt.Sprintf("Warmup on %s is nearly one-way", m.Email),
			Detail: fmt.Sprintf(
				"Only %d%% of warmup messages from %s get a reply. Warmup works because it looks like real correspondence; at this rate it is mostly one-way traffic, which is the pattern it is meant to counteract.",
				m.WarmupReplyRate, m.Email),
			Remedy: fmt.Sprintf("Raise the warmup reply rate to around %d%%, the platform default.", 30),
			Evidence: map[string]any{
				"mailbox":            m.Email,
				"reply_rate_percent": m.WarmupReplyRate,
				"recommended":        30,
			},
			Action: withUndo(mailboxAction(m.ID,
				"Raise the warmup reply rate to 30%",
				map[string]any{"warmup_reply_rate": 30},
				change("Warmup reply rate", fmt.Sprintf("%d%%", m.WarmupReplyRate), "30%"),
			), map[string]any{"email_account_id": m.ID.String(), "warmup_reply_rate": m.WarmupReplyRate}),
		})
	}
	return out
}

func detectWarmupPoolBlocked(s *repository.AdvisorSnapshot) []Finding {
	out := []Finding{}
	for _, m := range s.Mailboxes {
		bad := m.PoolBlocked ||
			m.PoolHealth == "quarantined" || m.PoolHealth == "blocked" || m.PoolHealth == "throttled"
		if !bad {
			continue
		}

		// A quarantined mailbox that is still running cold campaigns is the
		// dangerous case: the platform has already judged it unsafe for shared
		// reputation surfaces, and it is still mailing strangers.
		severity := models.AdvisorHigh
		if m.InActiveCampaign {
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
			Impact:      clampImpact(70 + m.PoolSpamScore/4),
			Title:       fmt.Sprintf("%s is %s in the warmup pool", m.Email, state),
			Detail: fmt.Sprintf(
				"The warmup pool has marked %s as %s (spam score %d). Pool standing is the platform's own read on whether a mailbox is safe to keep in shared reputation surfaces, and it moved before mailbox providers did%s.",
				m.Email, state, m.PoolSpamScore,
				map[bool]string{true: ". This mailbox is still running cold campaigns", false: ""}[m.InActiveCampaign]),
			Remedy: "Stop cold sending from this mailbox until it requalifies. Re-entry needs healthy authentication, no recent complaints or hard-bounce spikes, and spam placement back under 10% on a fresh sample.",
			Evidence: map[string]any{
				"mailbox":                m.Email,
				"pool_state":             state,
				"spam_score":             m.PoolSpamScore,
				"pool_type":              m.WarmupPoolType,
				"currently_sending_cold": m.InActiveCampaign,
			},
		}
		if m.InActiveCampaign {
			f.Action = withUndo(mailboxAction(m.ID,
				"Stop cold sending from this mailbox",
				map[string]any{"status": "inactive"},
				change("Mailbox status", "active", "inactive"),
				change("Cold campaigns", "sending", "paused for this mailbox"),
			), map[string]any{"email_account_id": m.ID.String(), "status": "active"})
		}
		out = append(out, f)
	}
	return out
}
