package handler

import (
	"math"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/models"
)

// Stored messages and unknown metadata can embed identities even without a mailbox key.
var scopedCampaignLogMessages = map[string]string{
	"created": "Campaign created", "started": "Campaign started", "stopped": "Campaign stopped",
	"completed": "Campaign completed", "idle": "Waiting for new leads",
	"continuous_on": "Keep running for new leads enabled", "restart_refused": "Campaign could not restart",
	"daily_limit_reached":        "Organization daily sending limit reached",
	"daily_cap_reached":          "Mailbox daily sending budget reached",
	"mailboxes_unavailable":      "Campaign mailboxes are unavailable",
	"mailboxes_send_recovery":    "Campaign mailboxes are awaiting send recovery",
	"mailboxes_no_worker":        "Campaign mailboxes are awaiting a sending worker",
	"mailboxes_resting":          "Campaign mailboxes are out of cold rotation",
	"sender_busy":                "Leads are waiting for their assigned sender",
	"sender_reassigned":          "A lead's sender was reassigned",
	"tracking_domain_unverified": "An unverified tracking domain was not used",
	"email_failed":               "An email could not be sent", "email_sent": "Email sent",
	"progress_write_failed":   "Recording send progress failed; the email will not be sent again",
	"provider_match_deferred": "Leads are waiting for a same-provider sender",
	"suppressed":              "A recipient was skipped", "paused_lead": "A paused lead was skipped",
	"waiting_on_paused_leads": "Waiting for paused leads to resume",
	"auto_paused":             "Campaign automatically paused", "scheduler_failed": "Scheduling failed; retrying",
	"guardrail_paused": "Campaign paused by a sending guardrail",
	"content_warning":  "Campaign content triggered a spam warning",
	"action":           "Campaign action ran", "action_skipped": "Campaign action did not run",
	"contacts_added": "Contacts added", "sequence_added": "Campaign step added",
	"contact_bounced": "A recipient bounced", "contact_replied": "A recipient replied",
}

func (h *Handler) campaignLogAddresses(c *gin.Context, orgID uuid.UUID, logs []models.CampaignLog, allowed []uuid.UUID) map[string]uuid.UUID {
	addresses := map[string]uuid.UUID{}
	wanted := map[string]bool{}
	for _, entry := range logs {
		switch entry.EventType {
		case "sender_busy", "tracking_domain_unverified":
			if address, ok := entry.Metadata["mailbox"].(string); ok && address != "" {
				wanted[address] = true
			}
		case "daily_cap_reached", "mailboxes_unavailable", "mailboxes_send_recovery":
			for _, row := range campaignLogMailboxRows(entry.Metadata["mailboxes"]) {
				if address, ok := row["mailbox"].(string); ok && address != "" {
					wanted[address] = true
				}
			}
		}
	}
	if len(wanted) == 0 || h.EmailService == nil {
		return addresses
	}
	keep := campaignLogIDSet(allowed)
	if len(keep) == 0 {
		return addresses
	}
	// Old pool-budget logs have addresses but no IDs; only an org-scoped grant lookup can authorize them.
	cursor := ""
	seen := map[string]bool{}
	for {
		page, xerr := h.EmailService.Search(c.Request.Context(), orgID.String(), "", cursor, "", "100", allowed)
		if xerr != nil || page == nil {
			return addresses
		}
		for _, account := range page.Data {
			if keep[account.ID] && wanted[account.Email] {
				addresses[account.Email] = account.ID
				delete(wanted, account.Email)
			}
		}
		if len(wanted) == 0 || !page.Pagination.HasMore || page.Pagination.NextCursor == nil {
			return addresses
		}
		next := *page.Pagination.NextCursor
		if next == "" || next == cursor || seen[next] {
			return addresses
		}
		seen[next] = true
		cursor = next
	}
}

func campaignLogIDSet(ids []uuid.UUID) map[uuid.UUID]bool {
	keep := make(map[uuid.UUID]bool, len(ids))
	for _, id := range ids {
		if id != uuid.Nil {
			keep[id] = true
		}
	}
	return keep
}

func scopedCampaignLog(entry models.CampaignLog, allowed []uuid.UUID, addresses map[string]uuid.UUID) models.CampaignLog {
	if allowed == nil {
		return entry
	}
	meta := map[string]interface{}{}
	message, known := scopedCampaignLogMessages[entry.EventType]
	if !known {
		entry.EventType, entry.Message, entry.Metadata = "activity", "Campaign activity recorded", meta
		return entry
	}
	entry.Message = message
	keep := campaignLogIDSet(allowed)
	src := entry.Metadata
	copyCampaignLogEnum(meta, src, "level", "info", "warn", "error")
	switch entry.EventType {
	case "daily_cap_reached", "mailboxes_unavailable", "mailboxes_send_recovery":
		rows := []map[string]interface{}{}
		for _, row := range campaignLogMailboxRows(src["mailboxes"]) {
			if mailbox := scopedCampaignLogMailbox(row, keep, addresses); mailbox != nil {
				rows = append(rows, mailbox)
			}
		}
		meta["mailboxes"] = rows
	case "sender_busy", "tracking_domain_unverified":
		if mailbox := scopedCampaignLogMailbox(src, keep, addresses); mailbox != nil {
			meta["mailbox"] = mailbox["mailbox"]
			if entry.EventType == "sender_busy" {
				copyCampaignLogEnum(meta, src, "reason", "budget", "hours", "no_worker", "send_recovery", "send_cooldown", "send_authority", "resting", "health", "domain_auth", "no_working_day")
			} else {
				copyCampaignLogEnum(meta, src, "scope", "mailbox", "campaign")
			}
		}
	case "email_sent", "email_failed", "progress_write_failed", "sender_reassigned":
		for _, key := range []string{"contact_id", "sequence_id", "task_id"} {
			copyCampaignLogID(meta, src, key, nil)
		}
		for _, key := range []string{"account_id", "prev_account_id"} {
			copyCampaignLogID(meta, src, key, keep)
		}
		if entry.EventType == "email_failed" {
			copyCampaignLogNumbers(meta, src, "attempts", "max_attempts")
			for _, key := range []string{"will_retry", "reopened"} {
				copyCampaignLogBool(meta, src, key)
			}
		}
	case "waiting_on_paused_leads":
		copyCampaignLogNumbers(meta, src, "paused_leads")
	case "auto_paused":
		copyCampaignLogNumbers(meta, src, "undeliverable")
	case "content_warning":
		copyCampaignLogID(meta, src, "sequence_id", nil)
		copyCampaignLogNumbers(meta, src, "score", "floor")
	case "guardrail_paused":
		copyCampaignLogEnum(meta, src, "rule", "placement", "complaint_rate", "bounce_rate", "reply_rate")
	}
	if entry.EventType == "email_failed" || entry.EventType == "scheduler_failed" || entry.EventType == "auto_paused" || entry.EventType == "progress_write_failed" {
		copyCampaignLogEnum(meta, src, "code",
			"SEND_FAILED", "WORKER_UNAVAILABLE", "SEND_DISPATCH_UNKNOWN", "SEND_OUTCOME_LOST", "PROGRESS_WRITE_FAILED",
			"CAMPAIGN_DAILY_LIMIT_REACHED", "MAILBOX_SENDING_PLAN_DENIED", "SEND_ADMISSION_DENIED",
			"RECIPIENT_REJECTED", "DOMAIN_AUTH_REJECTED", "SEND_REJECTED", "INVALID_CREDENTIALS", "AUTHENTICATION_FAILED",
			"AUTHORIZATION_FAILED", "RATE_LIMIT_EXCEEDED", "SERVER_UNREACHABLE", "CONNECTION_LOST", "QUOTA_EXCEEDED", "ACCOUNT_SUSPENDED",
			"scheduler_error", "sender_attribution_failed", "reply_to_record_failed", "send_reservation_failed", "no_accounts", "no_organization")
	}
	entry.Metadata = meta
	return entry
}

func campaignLogMailboxRows(value interface{}) []map[string]interface{} {
	switch rows := value.(type) {
	case []map[string]interface{}:
		return rows
	case []interface{}:
		out := make([]map[string]interface{}, 0, len(rows))
		for _, row := range rows {
			if m, ok := row.(map[string]interface{}); ok {
				out = append(out, m)
			}
		}
		return out
	default:
		return nil
	}
}

func scopedCampaignLogMailbox(src map[string]interface{}, keep map[uuid.UUID]bool, addresses map[string]uuid.UUID) map[string]interface{} {
	address, ok := src["mailbox"].(string)
	id := addresses[address]
	if !ok || !keep[id] {
		return nil
	}
	for _, key := range []string{"account_id", "mailbox_id"} {
		if _, exists := src[key]; exists {
			identity := map[string]interface{}{}
			copyCampaignLogID(identity, src, key, keep)
			if identity[key] != id.String() {
				return nil
			}
		}
	}
	out := map[string]interface{}{"mailbox": address}
	copyCampaignLogNumbers(out, src, "cap", "sent_today")
	copyCampaignLogBool(out, src, "sending_now")
	copyCampaignLogEnum(out, src, "limited_by", "mailbox_daily_cap", "campaign_daily_limit", "campaign_ramp", "warmup_graduation", "workspace_risk")
	copyCampaignLogEnum(out, src, "gate", "budget", "hours", "no_worker", "send_recovery", "send_cooldown", "send_authority", "resting", "health", "domain_auth", "no_working_day")
	copyCampaignLogEnum(out, src, "warmup_health", "healthy", "watch", "throttled", "quarantined", "blocked")
	return out
}

func copyCampaignLogID(dst, src map[string]interface{}, key string, keep map[uuid.UUID]bool) {
	var id uuid.UUID
	switch value := src[key].(type) {
	case string:
		id, _ = uuid.Parse(value)
	case uuid.UUID:
		id = value
	}
	if id != uuid.Nil && (keep == nil || keep[id]) {
		dst[key] = id.String()
	}
}

func copyCampaignLogNumbers(dst, src map[string]interface{}, keys ...string) {
	for _, key := range keys {
		switch value := src[key].(type) {
		case int:
			if value >= 0 {
				dst[key] = value
			}
		case float64:
			if value >= 0 && value <= float64(1<<53) && math.Trunc(value) == value {
				dst[key] = value
			}
		}
	}
}

func copyCampaignLogBool(dst, src map[string]interface{}, key string) {
	if value, ok := src[key].(bool); ok {
		dst[key] = value
	}
}

func copyCampaignLogEnum(dst, src map[string]interface{}, key string, allowed ...string) {
	value, ok := src[key].(string)
	if !ok {
		return
	}
	for _, candidate := range allowed {
		if value == candidate {
			dst[key] = value
			return
		}
	}
}
