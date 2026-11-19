package repository

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/warmbly/warmbly/internal/models"
)

// partnerLaneSQL holds when a partner-managed workspace lets mailbox ea send on lane: campaigns need
// sending_warmup, warmup and placement need any enablement, a test-mode workspace sends nothing, and a
// mailbox with no partner row reads as off. A workspace no partner manages is unaffected. It is the one
// definition the dispatch gate and the scheduler's admission read; models.PartnerEnablement mirrors it.
func partnerLaneSQL(ea, lane string) string {
	return fmt.Sprintf(`NOT EXISTS (SELECT 1 FROM partner_organizations po WHERE po.organization_id = %[1]s.organization_id
	 AND (po.mode = 'test' OR (%[2]s IN ('campaign', 'warmup', 'placement') AND NOT EXISTS (
	   SELECT 1 FROM partner_mailboxes pm WHERE pm.email_account_id = %[1]s.id AND pm.detached_at IS NULL
	   AND (pm.enablement = 'sending_warmup' OR (pm.enablement = 'warmup_only' AND %[2]s <> 'campaign'))))))`, ea, lane)
}

// utcDayStartSQL is the start of the current UTC day, the day a partner's per-mailbox cap is counted on.
const utcDayStartSQL = `(date_trunc('day', now() AT TIME ZONE 'UTC') AT TIME ZONE 'UTC')`

// partnerCampaignSentTodaySQL counts a mailbox's campaign sends this UTC day, held or done, as the
// daily counters count them; exclude is a task id left out, or NULL.
func partnerCampaignSentTodaySQL(mailbox, exclude string) string {
	return fmt.Sprintf(`(SELECT count(*) FROM tasks t WHERE t.email_account_id = %[1]s AND t.id IS DISTINCT FROM %[2]s AND t.task_type = 'campaign'
	 AND ((t.send_reserved_at IS NOT NULL AND t.send_released_at IS NULL AND t.send_reserved_at >= %[3]s)
	   OR (t.send_reserved_at IS NULL AND t.status = 'completed' AND t.completed_at >= %[3]s
	       AND EXISTS (SELECT 1 FROM campaign_tasks ct WHERE ct.task_id = t.id AND ct.sequence_id IS NOT NULL))))`, mailbox, exclude, utcDayStartSQL)
}

// checkPartnerDailyCap refuses a campaign send from a mailbox at its partner's cap for the UTC day.
func checkPartnerDailyCap(ctx context.Context, tx pgx.Tx, task, mailbox uuid.UUID) error {
	var capv, sent int
	err := tx.QueryRow(ctx, `SELECT po.campaign_sends_per_mailbox_per_day, `+partnerCampaignSentTodaySQL("ea.id", "$2::uuid")+`
	 FROM email_accounts ea JOIN partner_organizations po ON po.organization_id = ea.organization_id WHERE ea.id = $1`, mailbox, task).Scan(&capv, &sent)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	if sent >= capv {
		return ErrCampaignDailyLimit
	}
	return nil
}

// Why a partner holds a mailbox's campaign sends, read by the scheduler from GetSendAdmission.
const (
	ColdHoldPartnerEnablement = "partner_enablement"
	ColdHoldPartnerTestMode   = "partner_test_mode"
	ColdHoldPartnerDailyCap   = "partner_daily_cap"
)

// partnerColdHold fills a's partner fields: why campaigns may not use the mailbox, or what is left of its cap today.
func (r *taskRepository) partnerColdHold(ctx context.Context, a *SendAdmission, orgID, mailboxID uuid.UUID, now time.Time) error {
	var mode, enablement string
	var capv, sent int
	err := r.db.QueryRow(ctx, `SELECT po.mode, COALESCE(pm.enablement, 'off'), po.campaign_sends_per_mailbox_per_day, `+partnerCampaignSentTodaySQL("$1::uuid", "NULL::uuid")+`
	 FROM partner_organizations po LEFT JOIN partner_mailboxes pm ON pm.email_account_id = $1 AND pm.detached_at IS NULL
	 WHERE po.organization_id = $2`, mailboxID, orgID).Scan(&mode, &enablement, &capv, &sent)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	switch {
	case mode == string(models.PartnerModeTest):
		a.ColdHold = ColdHoldPartnerTestMode
	case !models.PartnerEnablement(enablement).Campaigns():
		a.ColdHold = ColdHoldPartnerEnablement
	default:
		room := max(0, capv-sent)
		a.ColdRoom = &room
		if room == 0 {
			a.ColdHold = ColdHoldPartnerDailyCap
			next := now.UTC().Truncate(24 * time.Hour).Add(24 * time.Hour)
			a.ColdReopensAt = &next
		}
	}
	return nil
}
