package repository

import (
	"context"
	"encoding/json"
	"errors"
	"net/mail"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/warmbly/warmbly/internal/config"
	"github.com/warmbly/warmbly/internal/models"
)

var ErrSendAdmissionDenied = errors.New("current send authority or capacity unavailable")

type OutboundReservation struct {
	TaskID         uuid.UUID
	MailboxID      uuid.UUID
	OrganizationID uuid.UUID
	WorkerID       uuid.UUID
	Provider       models.InboxProvider
	Recipients     []string
}

type OutboundAdmissionRepository interface {
	ReserveOutbound(context.Context, OutboundReservation) (uuid.UUID, error)
	InspectOutbound(context.Context, uuid.UUID, uuid.UUID, uuid.UUID) (*WarmupDispatchState, error)
	BeginOutbound(context.Context, uuid.UUID, uuid.UUID, uuid.UUID, uuid.UUID) (*WarmupDispatchState, error)
	FinishOutbound(context.Context, uuid.UUID, uuid.UUID, uuid.UUID, models.SendEmailResult) error
	CancelOutbound(context.Context, uuid.UUID, uuid.UUID, uuid.UUID, uuid.UUID) error
}

func recipientOccurrences(in []string) ([]string, error) {
	if len(in) == 0 || len(in) > 1000 {
		return nil, ErrSendAdmissionDenied
	}
	out := make([]string, 0, len(in))
	for _, raw := range in {
		if strings.ContainsAny(raw, "\r\n\x00") {
			return nil, ErrSendAdmissionDenied
		}
		a, err := mail.ParseAddress(raw)
		if err != nil {
			return nil, ErrSendAdmissionDenied
		}
		out = append(out, strings.ToLower(strings.TrimSpace(a.Address)))
	}
	return out, nil
}

func MatchOutboundRecipients(expected, actual []string) bool {
	want, err := recipientOccurrences(expected)
	if err != nil {
		return false
	}
	got, err := recipientOccurrences(actual)
	if err != nil {
		return false
	}
	sort.Strings(want)
	sort.Strings(got)
	return strings.Join(want, "\x00") == strings.Join(got, "\x00")
}

func sendAuthority(ctx context.Context, tx pgx.Tx, task, mailbox, org, worker uuid.UUID, provider string, ownReservation bool) (string, error) {
	var lane string
	var allowed bool
	err := tx.QueryRow(ctx, `SELECT t.task_type, ea.status='active' AND ea.organization_id=$3 AND ea.provider::text=$5
	 AND ea.worker_id=$4 AND o.risk_state<>'suspended'
	 AND (NOT ea.send_recovery_hold OR ($6 AND ea.send_recovery_reason='unknown' AND ea.send_recovery_task_id=t.id))
	 AND (ea.send_cooldown_until IS NULL OR ea.send_cooldown_until<=NOW())
	 AND EXISTS(SELECT 1 FROM fleet_nodes n WHERE n.id=$4 AND n.warmup_send_protocol>=2 AND n.active AND n.last_seen_at>NOW()-INTERVAL '10 minutes')
	 AND (t.task_type NOT IN ('warmup','placement') OR ea.test_mode IS NULL OR ea.test_mode='legacy' OR ea.test_mode='diagnostic' AND ea.test_send_enabled)
	 AND t.send_result_applied_at IS NULL AND ((NOT $6 AND t.status='active') OR ($6 AND t.status='completed'))
	 FROM tasks t JOIN email_accounts ea ON ea.id=t.email_account_id JOIN organizations o ON o.id=ea.organization_id
	 WHERE t.id=$1 AND ea.id=$2`, task, mailbox, org, worker, provider, ownReservation).Scan(&lane, &allowed)
	if err != nil {
		return "", err
	}
	if !allowed {
		return "", ErrSendAdmissionDenied
	}
	return lane, nil
}

func lockSend(ctx context.Context, tx pgx.Tx, task, mailbox uuid.UUID) error {
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,268))`, task.String()); err != nil {
		return err
	}
	_, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,269))`, mailbox.String())
	return err
}

func (r *taskRepository) ReserveOutbound(ctx context.Context, in OutboundReservation) (uuid.UUID, error) {
	recipients, err := recipientOccurrences(in.Recipients)
	if err != nil {
		return uuid.Nil, err
	}
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return uuid.Nil, err
	}
	defer tx.Rollback(ctx)
	if err = lockSend(ctx, tx, in.TaskID, in.MailboxID); err != nil {
		return uuid.Nil, err
	}
	var existing *uuid.UUID
	var released *time.Time
	err = tx.QueryRow(ctx, `SELECT send_executor_nonce,send_released_at FROM tasks WHERE id=$1 AND email_account_id=$2`, in.TaskID, in.MailboxID).Scan(&existing, &released)
	if err != nil {
		return uuid.Nil, err
	}
	if existing != nil && released == nil {
		return uuid.Nil, ErrSendAdmissionDenied
	}
	var warmupNonce *uuid.UUID
	err = tx.QueryRow(ctx, `SELECT dispatch_nonce FROM warmup_tasks WHERE task_id=$1`, in.TaskID).Scan(&warmupNonce)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return uuid.Nil, err
	}
	lane, err := sendAuthority(ctx, tx, in.TaskID, in.MailboxID, in.OrganizationID, in.WorkerID, string(in.Provider), warmupNonce != nil)
	if err != nil {
		return uuid.Nil, err
	}
	if lane != "warmup" && lane != "campaign" && lane != "email" && lane != "placement" {
		return uuid.Nil, ErrSendAdmissionDenied
	}
	if err = checkSendRecipients(ctx, tx, in.OrganizationID, recipients); err != nil {
		return uuid.Nil, err
	}
	var timezone string
	var campaignCap, warmupCap int
	var shared, rolling *int
	err = tx.QueryRow(ctx, `SELECT COALESCE(NULLIF(ea.timezone,''),NULLIF(o.timezone,''),'UTC'),ea.campaign_limit,ea.warmup_max,ea.shared_daily_limit,ea.rolling_recipient_limit
	 FROM email_accounts ea JOIN organizations o ON o.id=ea.organization_id WHERE ea.id=$1 FOR UPDATE OF ea`, in.MailboxID).Scan(&timezone, &campaignCap, &warmupCap, &shared, &rolling)
	if err != nil {
		return uuid.Nil, err
	}
	var occurrences int
	err = tx.QueryRow(ctx, `SELECT COALESCE(SUM(COALESCE(cardinality(send_recipients),GREATEST(1,cardinality(et.to_addrs)+COALESCE(cardinality(et.cc),0)+COALESCE(cardinality(et.bcc),0)))) FILTER(WHERE COALESCE(send_reserved_at,completed_at)>NOW()-INTERVAL '24 hours'),0)
	 FROM tasks t LEFT JOIN email_tasks et ON et.task_id=t.id
	 WHERE t.email_account_id=$1 AND t.id<>$2 AND task_type IN ('campaign','email','warmup','placement')
	 AND ((send_reserved_at IS NOT NULL AND send_released_at IS NULL) OR (send_reserved_at IS NULL AND status='completed' AND completed_at IS NOT NULL
	 AND (task_type<>'campaign' OR EXISTS(SELECT 1 FROM campaign_tasks ct WHERE ct.task_id=t.id AND ct.sequence_id IS NOT NULL))))
	 AND COALESCE(send_reserved_at,completed_at)>NOW()-INTERVAL '24 hours'`, in.MailboxID, in.TaskID).Scan(&occurrences)
	if err != nil {
		return uuid.Nil, err
	}
	// Calendar counters and rolling recipient occurrences are independent constraints.
	var attemptedRecipients, attemptedToday int
	err = tx.QueryRow(ctx, `SELECT COALESCE(SUM(recipient_count) FILTER(WHERE attempted_at>NOW()-INTERVAL '24 hours'),0),
	 COUNT(*) FILTER(WHERE (attempted_at AT TIME ZONE $2)::date=(NOW() AT TIME ZONE $2)::date)
	 FROM outbound_attempts WHERE email_account_id=$1`, in.MailboxID, timezone).Scan(&attemptedRecipients, &attemptedToday)
	if err != nil {
		return uuid.Nil, err
	}
	var reservedAttemptRecipients, reservedAttemptsToday int
	err = tx.QueryRow(ctx, `SELECT COALESCE(SUM(GREATEST(1,cardinality(send_recipients))) FILTER(WHERE COALESCE(send_reserved_at,completed_at)>NOW()-INTERVAL '24 hours'),0),
	 COUNT(*) FILTER(WHERE COALESCE(send_business_day,(completed_at AT TIME ZONE $3)::date)=(NOW() AT TIME ZONE $3)::date)
	 FROM tasks t WHERE email_account_id=$1 AND id<>$2 AND task_type IN('email','campaign','warmup','placement')
	 AND ((send_reserved_at IS NOT NULL AND send_released_at IS NULL) OR (send_reserved_at IS NULL AND status='completed' AND completed_at IS NOT NULL
	 AND (task_type<>'campaign' OR EXISTS(SELECT 1 FROM campaign_tasks ct WHERE ct.task_id=t.id AND ct.sequence_id IS NOT NULL))))
	 AND NOT EXISTS(SELECT 1 FROM outbound_attempts a WHERE a.nonce=t.send_executor_nonce)`, in.MailboxID, in.TaskID, timezone).Scan(&reservedAttemptRecipients, &reservedAttemptsToday)
	if err != nil {
		return uuid.Nil, err
	}
	occurrences = attemptedRecipients + reservedAttemptRecipients
	var dayTotal, dayCold, dayDiagnostic int
	err = tx.QueryRow(ctx, `SELECT COUNT(*),COUNT(*) FILTER(WHERE task_type IN ('campaign','email')),COUNT(*) FILTER(WHERE task_type IN ('warmup','placement')) FROM tasks t
	 WHERE email_account_id=$1 AND id<>$2 AND task_type IN ('campaign','email','warmup','placement')
	 AND COALESCE(send_business_day,(completed_at AT TIME ZONE $3)::date)=(NOW() AT TIME ZONE $3)::date
	 AND ((send_reserved_at IS NOT NULL AND send_released_at IS NULL) OR (send_reserved_at IS NULL AND status='completed' AND completed_at IS NOT NULL
	 AND (task_type<>'campaign' OR EXISTS(SELECT 1 FROM campaign_tasks ct WHERE ct.task_id=t.id AND ct.sequence_id IS NOT NULL))))`, in.MailboxID, in.TaskID, timezone).Scan(&dayTotal, &dayCold, &dayDiagnostic)
	if err != nil {
		return uuid.Nil, err
	}
	sharedCap, rollingCap := max(1, campaignCap+warmupCap), max(1, campaignCap+warmupCap)
	if shared != nil {
		sharedCap = *shared
	}
	if rolling != nil {
		rollingCap = *rolling
	}
	if max(dayTotal, attemptedToday+reservedAttemptsToday) >= sharedCap || occurrences+len(recipients) > rollingCap || (lane == "campaign" || lane == "email") && dayCold >= campaignCap || (lane == "warmup" || lane == "placement") && dayDiagnostic >= warmupCap {
		return uuid.Nil, ErrSendAdmissionDenied
	}
	if lane == "campaign" {
		if err = checkCampaignReservation(ctx, tx, in.TaskID); err != nil {
			return uuid.Nil, err
		}
	}
	if lane == "warmup" && warmupNonce == nil {
		return uuid.Nil, ErrSendAdmissionDenied
	}
	if lane == "warmup" {
		if err = checkWarmupRecipientReservation(ctx, tx, in.TaskID, in.MailboxID); err != nil {
			return uuid.Nil, err
		}
	}
	nonce := uuid.New()
	if warmupNonce != nil {
		nonce = *warmupNonce
	}
	_, err = tx.Exec(ctx, `UPDATE tasks SET send_reserved_at=NOW(),send_business_day=(NOW() AT TIME ZONE $2)::date,send_recipients=$3,
	 send_released_at=NULL,send_executor_nonce=$4,send_executor_worker=$5,send_executor_started_at=NULL,send_executor_result=NULL,
	 send_result_state='unknown',status='completed',completed_at=NOW() WHERE id=$1`, in.TaskID, timezone, recipients, nonce, in.WorkerID)
	if err != nil {
		return uuid.Nil, err
	}
	_, err = tx.Exec(ctx, `UPDATE email_accounts SET send_recovery_hold=true,send_recovery_reason='unknown',send_recovery_task_id=$2 WHERE id=$1`, in.MailboxID, in.TaskID)
	if err != nil {
		return uuid.Nil, err
	}
	if err = tx.Commit(ctx); err != nil {
		return uuid.Nil, err
	}
	return nonce, nil
}

func checkSendRecipients(ctx context.Context, tx pgx.Tx, org uuid.UUID, recipients []string) error {
	var denied bool
	err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM unnest($2::text[]) recipient WHERE recipient_suppressed($1,recipient))`, org, recipients).Scan(&denied)
	if err != nil {
		return err
	}
	if denied {
		return ErrSendAdmissionDenied
	}
	return nil
}

func checkCampaignReservation(ctx context.Context, tx pgx.Tx, task uuid.UUID) error {
	var campaign uuid.UUID
	if err := tx.QueryRow(ctx, `SELECT campaign_id FROM campaign_tasks WHERE task_id=$1`, task).Scan(&campaign); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,272))`, campaign.String()); err != nil {
		return err
	}
	var allowed bool
	err := tx.QueryRow(ctx, `SELECT c.status='active' AND (c.end_date IS NULL OR c.end_date>=NOW())
	 AND NOT recipient_suppressed(c.organization_id,contact.email) AND contact.subscribed IS DISTINCT FROM false
	 AND NOT EXISTS(SELECT 1 FROM campaign_leads h WHERE h.campaign_id=c.id AND h.contact_id=ct.contact_id AND h.paused_at IS NOT NULL AND (h.paused_until IS NULL OR h.paused_until>NOW()))
	 AND (c.daily_limit<=0 OR (SELECT COUNT(*) FROM campaign_contact_progress p WHERE p.campaign_id=c.id
	 AND COALESCE(p.sent_at,p.dispatched_at)>=date_trunc('day',NOW() AT TIME ZONE COALESCE(NULLIF(c.timezone,''),NULLIF(o.timezone,''),'UTC')) AT TIME ZONE COALESCE(NULLIF(c.timezone,''),NULLIF(o.timezone,''),'UTC'))<=c.daily_limit)
	 AND c.organization_id=ea.organization_id AND contact.organization_id=c.organization_id
	 AND EXISTS(SELECT 1 FROM campaign_contact_progress p WHERE p.campaign_id=c.id AND p.contact_id=ct.contact_id AND p.sequence_id=ct.sequence_id AND p.dispatch_task_id=t.id)
	 FROM campaign_tasks ct JOIN tasks t ON t.id=ct.task_id JOIN email_accounts ea ON ea.id=t.email_account_id JOIN campaigns c ON c.id=ct.campaign_id JOIN organizations o ON o.id=c.organization_id JOIN contacts contact ON contact.id=ct.contact_id WHERE ct.task_id=$1`, task).Scan(&allowed)
	if err != nil {
		return err
	}
	if !allowed {
		return ErrSendAdmissionDenied
	}
	return nil
}

func (r *taskRepository) InspectOutbound(ctx context.Context, task, mailbox, worker uuid.UUID) (*WarmupDispatchState, error) {
	var nonce *uuid.UUID
	var started *time.Time
	var released bool
	var raw []byte
	var org *uuid.UUID
	var recipients []string
	err := r.db.QueryRow(ctx, `SELECT t.send_executor_nonce,t.send_executor_started_at,t.send_executor_result,t.send_released_at IS NOT NULL,ea.organization_id,t.send_recipients FROM tasks t JOIN email_accounts ea ON ea.id=t.email_account_id WHERE t.id=$1 AND t.email_account_id=$2 AND (t.send_executor_nonce IS NULL OR t.send_executor_worker=$3)`, task, mailbox, worker).Scan(&nonce, &started, &raw, &released, &org, &recipients)
	if errors.Is(err, pgx.ErrNoRows) {
		return &WarmupDispatchState{State: "denied"}, nil
	}
	if err != nil {
		return nil, err
	}
	if nonce == nil {
		return &WarmupDispatchState{State: "legacy"}, nil
	}
	if released {
		return &WarmupDispatchState{State: "denied"}, nil
	}
	if len(raw) > 0 {
		var result models.SendEmailResult
		if err = json.Unmarshal(raw, &result); err != nil {
			return nil, err
		}
		return &WarmupDispatchState{State: "finished", Result: &result}, nil
	}
	if started != nil {
		return &WarmupDispatchState{State: "started"}, nil
	}
	return &WarmupDispatchState{State: "authorized", OrganizationID: org, Recipients: recipients}, nil
}

func (r *taskRepository) BeginOutbound(ctx context.Context, task, mailbox, worker, nonce uuid.UUID) (*WarmupDispatchState, error) {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)
	if err = lockSend(ctx, tx, task, mailbox); err != nil {
		return nil, err
	}
	var org uuid.UUID
	var provider string
	var recipients []string
	var current *uuid.UUID
	var started *time.Time
	err = tx.QueryRow(ctx, `SELECT ea.organization_id,ea.provider::text,t.send_recipients,t.send_executor_nonce,t.send_executor_started_at FROM tasks t JOIN email_accounts ea ON ea.id=t.email_account_id
	 WHERE t.id=$1 AND ea.id=$2 AND t.send_executor_worker=$3 AND t.send_released_at IS NULL`, task, mailbox, worker).Scan(&org, &provider, &recipients, &current, &started)
	if err != nil {
		return nil, err
	}
	if current == nil || *current != nonce {
		return &WarmupDispatchState{State: "denied"}, nil
	}
	if started != nil {
		if err = tx.Commit(ctx); err != nil {
			return nil, err
		}
		return r.InspectOutbound(ctx, task, mailbox, worker)
	}
	lane, err := sendAuthority(ctx, tx, task, mailbox, org, worker, provider, true)
	if err == nil {
		err = checkSendRecipients(ctx, tx, org, recipients)
	}
	if err == nil && lane == "campaign" {
		err = checkCampaignReservation(ctx, tx, task)
	}
	if err != nil {
		if !errors.Is(err, ErrSendAdmissionDenied) {
			return nil, err
		}
		if err = tx.Commit(ctx); err != nil {
			return nil, err
		}
		if err = r.CancelOutbound(ctx, task, mailbox, worker, nonce); err != nil {
			return nil, err
		}
		return &WarmupDispatchState{State: "denied"}, nil
	}
	if lane == "warmup" {
		var allowed bool
		err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM warmup_pool_participants wpp JOIN email_accounts ea ON ea.id=wpp.email_account_id WHERE ea.id=$2 AND wpp.participant_role='sender_receiver' AND `+testSenderSQL+` AND `+poolEligibleSQL+`)
		AND EXISTS(SELECT 1 FROM warmup_tokens wt JOIN email_accounts ea ON ea.id=wt.recipient_account_id JOIN warmup_pool_participants wpp ON wpp.email_account_id=ea.id WHERE wt.task_id=$1 AND `+partnerEligibleSQL+`)`, task, mailbox).Scan(&allowed)
		if err != nil {
			return nil, err
		}
		if !allowed {
			if err = tx.Commit(ctx); err != nil {
				return nil, err
			}
			if err = r.CancelOutbound(ctx, task, mailbox, worker, nonce); err != nil {
				return nil, err
			}
			return &WarmupDispatchState{State: "denied"}, nil
		}
		if _, err = tx.Exec(ctx, `UPDATE warmup_tasks SET dispatch_started_at=NOW() WHERE task_id=$1 AND dispatch_nonce=$2`, task, nonce); err != nil {
			return nil, err
		}
	}
	_, err = tx.Exec(ctx, `UPDATE tasks SET send_executor_started_at=NOW() WHERE id=$1`, task)
	if err != nil {
		return nil, err
	}
	if _, err = tx.Exec(ctx, `INSERT INTO outbound_attempts(nonce,task_id,email_account_id,provider,attempted_at,recipient_count) VALUES($1,$2,$3,$4,NOW(),$5)`, nonce, task, mailbox, provider, len(recipients)); err != nil {
		return nil, err
	}
	if err = tx.Commit(ctx); err != nil {
		return nil, err
	}
	return &WarmupDispatchState{State: "execute"}, nil
}

func (r *taskRepository) FinishOutbound(ctx context.Context, task, mailbox, worker uuid.UUID, result models.SendEmailResult) error {
	if result.TaskID != task {
		return errors.New("dispatch task mismatch")
	}
	raw, err := json.Marshal(result)
	if err != nil {
		return err
	}
	tag, err := r.db.Exec(ctx, `UPDATE tasks SET send_executor_result=$4 WHERE id=$1 AND email_account_id=$2 AND send_executor_worker=$3 AND send_executor_started_at IS NOT NULL AND send_executor_result IS NULL`, task, mailbox, worker, raw)
	if err != nil {
		return err
	}
	if tag.RowsAffected() > 0 {
		return nil
	}
	state, err := r.InspectOutbound(ctx, task, mailbox, worker)
	if err != nil {
		return err
	}
	previous, _ := json.Marshal(state.Result)
	if state.State != "finished" || string(previous) != string(raw) {
		return errors.New("conflicting or unstarted dispatch result")
	}
	return nil
}

func (r *taskRepository) CancelOutbound(ctx context.Context, task, mailbox, worker, nonce uuid.UUID) error {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if err = lockSend(ctx, tx, task, mailbox); err != nil {
		return err
	}
	var lane string
	err = tx.QueryRow(ctx, `UPDATE tasks SET send_released_at=NOW(),send_result_state='failed',send_result_applied_at=NOW(),status='cancelled'
	 WHERE id=$1 AND email_account_id=$2 AND send_executor_worker=$3 AND send_executor_nonce=$4 AND send_executor_started_at IS NULL AND send_released_at IS NULL RETURNING task_type`, task, mailbox, worker, nonce).Scan(&lane)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	if lane == "campaign" {
		var c, contact, sequence uuid.UUID
		var newLead bool
		err = tx.QueryRow(ctx, `SELECT ct.campaign_id,ct.contact_id,ct.sequence_id,NOT EXISTS(SELECT 1 FROM campaign_contact_progress p WHERE p.campaign_id=ct.campaign_id AND p.contact_id=ct.contact_id AND p.sent_at IS NOT NULL) FROM campaign_tasks ct WHERE ct.task_id=$1`, task).Scan(&c, &contact, &sequence, &newLead)
		if err != nil {
			return err
		}
		inner := context.WithValue(ctx, sendResultKey{}, sendResultContext{tx: tx, taskID: task})
		if err = NewCampaignProgressRepository(r.db).ReleaseSend(inner, c, contact, sequence, newLead); err != nil {
			return err
		}
	}
	if lane == "warmup" {
		if _, err = tx.Exec(ctx, `UPDATE warmup_statistics SET emails_sent=GREATEST(emails_sent-1,0),emails_replied=GREATEST(emails_replied-CASE WHEN w.parent_task_id IS NULL THEN 0 ELSE 1 END,0) FROM warmup_tasks w JOIN tasks t ON t.id=w.task_id WHERE w.task_id=$2 AND warmup_statistics.email_account_id=$1 AND date=DATE(t.completed_at)`, mailbox, task); err != nil {
			return err
		}
		if _, err = tx.Exec(ctx, `DELETE FROM warmup_tokens WHERE task_id=$1`, task); err != nil {
			return err
		}
	}
	if _, err = tx.Exec(ctx, `UPDATE email_accounts SET send_recovery_hold=false,send_recovery_task_id=NULL,send_recovery_reason=NULL WHERE id=$1 AND send_recovery_reason='unknown' AND send_recovery_task_id=$2 AND NOT EXISTS(SELECT 1 FROM tasks WHERE email_account_id=$1 AND send_result_state='unknown')`, mailbox, task); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func checkWarmupRecipientReservation(ctx context.Context, tx pgx.Tx, task, sender uuid.UUID) error {
	var recipient uuid.UUID
	var pool string
	if err := tx.QueryRow(ctx, `SELECT wt.recipient_account_id,wp.pool_type FROM warmup_tokens wt JOIN warmup_pool_participants wpp ON wpp.email_account_id=wt.sender_account_id JOIN warmup_pools wp ON wp.id=wpp.pool_id WHERE wt.task_id=$1 AND wt.sender_account_id=$2`, task, sender).Scan(&recipient, &pool); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,273))`, recipient.String()); err != nil {
		return err
	}
	share := 100
	if pool != "premium" {
		share = config.WarmupFreeInboundSharePercent
	}
	var allowed bool
	err := tx.QueryRow(ctx, `SELECT GREATEST(
	 (SELECT COUNT(*) FROM warmup_received wr WHERE wr.email_account_id=ea.id AND wr.created_at >= date_trunc('day',NOW() AT TIME ZONE COALESCE(NULLIF(ea.timezone,''),NULLIF(o.timezone,''),'UTC')) AT TIME ZONE COALESCE(NULLIF(ea.timezone,''),NULLIF(o.timezone,''),'UTC')),
	 (SELECT COUNT(*) FROM warmup_tokens wt WHERE wt.recipient_account_id=ea.id AND wt.created_at >= date_trunc('day',NOW() AT TIME ZONE COALESCE(NULLIF(ea.timezone,''),NULLIF(o.timezone,''),'UTC')) AT TIME ZONE COALESCE(NULLIF(ea.timezone,''),NULLIF(o.timezone,''),'UTC')))
	 <= LEAST(GREATEST((((SELECT COUNT(*) FROM warmup_tokens wt WHERE wt.sender_account_id=ea.id AND wt.sent_message_id<>'' AND wt.created_at>=NOW()-INTERVAL '7 days')+6)/7)*`+inboundDailyMultipleSQL+`,`+inboundDailyFloorSQL+`),`+inboundDailyCeilingSQL+`)*$2/100
	 FROM email_accounts ea JOIN organizations o ON o.id=ea.organization_id WHERE ea.id=$1`, recipient, share).Scan(&allowed)
	if err != nil {
		return err
	}
	if !allowed {
		return ErrSendAdmissionDenied
	}
	return nil
}
