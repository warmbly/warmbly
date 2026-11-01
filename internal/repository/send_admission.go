package repository

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/mail"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/warmbly/warmbly/internal/models"
)

var ErrSendAdmissionDenied = errors.New("current send authority or capacity unavailable")

var ErrCampaignDailyLimit = fmt.Errorf("%w: campaign daily limit reached for this mailbox", ErrSendAdmissionDenied)

var ErrMailboxSendingPlan = fmt.Errorf("%w: mailbox's current sending plan has no capacity", ErrSendAdmissionDenied)

var ErrManualSendLimit = fmt.Errorf("%w: manual send fair-use limit reached for this mailbox", ErrSendAdmissionDenied)

// ManualSendLimitError names the fair-use bound a manual send ran into.
type ManualSendLimitError struct {
	Window models.ManualSendWindow
	Limit  int
}

func (e *ManualSendLimitError) Error() string {
	return fmt.Sprintf("%v (%s: %d recipients)", ErrManualSendLimit, e.Window, e.Limit)
}

func (e *ManualSendLimitError) Unwrap() error { return ErrManualSendLimit }

var ErrRecipientSuppressed = fmt.Errorf("%w: a recipient is on the workspace suppression list", ErrSendAdmissionDenied)

// RecipientSuppressedError names the recipient the suppression list matched.
type RecipientSuppressedError struct {
	Address string
}

func (e *RecipientSuppressedError) Error() string {
	return fmt.Sprintf("%v (%s)", ErrRecipientSuppressed, e.Address)
}

func (e *RecipientSuppressedError) Unwrap() error { return ErrRecipientSuppressed }

func SendAdmissionCode(err error) string {
	switch {
	case errors.Is(err, ErrCampaignDailyLimit):
		return "CAMPAIGN_DAILY_LIMIT_REACHED"
	case errors.Is(err, ErrMailboxSendingPlan):
		return "MAILBOX_SENDING_PLAN_DENIED"
	case errors.Is(err, ErrManualSendLimit):
		return "MANUAL_SEND_LIMIT_REACHED"
	case errors.Is(err, ErrRecipientSuppressed):
		return "RECIPIENT_SUPPRESSED"
	case errors.Is(err, ErrSendAdmissionDenied):
		return "SEND_ADMISSION_DENIED"
	default:
		return ""
	}
}

type OutboundReservation struct {
	TaskID         uuid.UUID
	MailboxID      uuid.UUID
	OrganizationID uuid.UUID
	WorkerID       uuid.UUID
	Provider       models.InboxProvider
	Recipients     []string
	// ManualPlanAllowance bounds the workspace's manual (unibox) recipients
	// in 24 hours across its mailboxes; 0 means no plan bound.
	ManualPlanAllowance int
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
	 AND o.risk_state<>'suspended'
	 AND (NOT ea.send_recovery_hold OR ($6 AND ea.send_recovery_reason='unknown' AND ea.send_recovery_task_id=t.id))
	 AND (ea.send_cooldown_until IS NULL OR ea.send_cooldown_until<=NOW())
	 AND (($4<>`+nilUUIDSQL+` AND ea.worker_id=$4 AND NOT `+instanceExecutedSQL("ea.id")+`
	  AND EXISTS(SELECT 1 FROM fleet_nodes n WHERE n.id=$4 AND n.warmup_send_protocol>=2 AND n.active AND n.last_seen_at>NOW()-INTERVAL '10 minutes'))
	  OR ($4=`+nilUUIDSQL+` AND t.task_type='warmup' AND `+selfExecutionLiveSQL("ea.id")+`))
	 AND (t.task_type NOT IN ('warmup','placement','cloud_warmup') OR ea.test_mode IS NULL OR ea.test_mode='legacy' OR ea.test_mode='diagnostic' AND ea.test_send_enabled)
	 AND t.send_result_applied_at IS NULL AND ((NOT $6 AND t.status='active') OR ($6 AND t.status='completed'))
	 AND t.campaign_send_isolated_at IS NULL
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
	if err := lockWarmupParticipation(ctx, tx, task, mailbox); err != nil {
		return err
	}
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
	if lane != "warmup" && lane != "campaign" && lane != "email" && lane != "placement" && lane != TaskTypeCloudWarmup {
		return uuid.Nil, ErrSendAdmissionDenied
	}
	if lane == TaskTypeCloudWarmup {
		if err = checkCloudSendCommand(ctx, tx, in.TaskID, in.MailboxID); err != nil {
			return uuid.Nil, err
		}
	}
	if err = checkSendRecipients(ctx, tx, in.TaskID, in.OrganizationID, lane, recipients); err != nil {
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
	if lane == "email" {
		if err = checkManualSendFairUse(ctx, tx, in.TaskID, in.MailboxID, len(recipients)); err != nil {
			return uuid.Nil, err
		}
		if err = checkManualSendPlan(ctx, tx, in.TaskID, in.OrganizationID, len(recipients), in.ManualPlanAllowance); err != nil {
			return uuid.Nil, err
		}
	}
	// Calendar counters and rolling recipient occurrences are independent constraints.
	var attemptedRecipients, attemptedToday int
	err = tx.QueryRow(ctx, `SELECT COALESCE(SUM(recipient_count) FILTER(WHERE attempted_at>NOW()-INTERVAL '24 hours'),0),
	 COUNT(*) FILTER(WHERE (attempted_at AT TIME ZONE $2)::date=(NOW() AT TIME ZONE $2)::date)
	 FROM outbound_attempts a WHERE email_account_id=$1
	 AND NOT EXISTS(SELECT 1 FROM tasks m WHERE m.id=a.task_id AND m.task_type='email')`, in.MailboxID, timezone).Scan(&attemptedRecipients, &attemptedToday)
	if err != nil {
		return uuid.Nil, err
	}
	var reservedAttemptRecipients, reservedAttemptsToday int
	err = tx.QueryRow(ctx, `SELECT COALESCE(SUM(GREATEST(1,cardinality(send_recipients))) FILTER(WHERE COALESCE(send_reserved_at,completed_at)>NOW()-INTERVAL '24 hours'),0),
	 COUNT(*) FILTER(WHERE COALESCE(send_business_day,(completed_at AT TIME ZONE $3)::date)=(NOW() AT TIME ZONE $3)::date)
	 FROM tasks t WHERE email_account_id=$1 AND id<>$2 AND task_type IN('campaign','warmup','placement','cloud_warmup')
	 AND ((send_reserved_at IS NOT NULL AND send_released_at IS NULL) OR (send_reserved_at IS NULL AND status='completed' AND completed_at IS NOT NULL
	 AND (task_type<>'campaign' OR EXISTS(SELECT 1 FROM campaign_tasks ct WHERE ct.task_id=t.id AND ct.sequence_id IS NOT NULL))))
	 AND NOT EXISTS(SELECT 1 FROM outbound_attempts a WHERE a.nonce=t.send_executor_nonce)`, in.MailboxID, in.TaskID, timezone).Scan(&reservedAttemptRecipients, &reservedAttemptsToday)
	if err != nil {
		return uuid.Nil, err
	}
	occurrences := attemptedRecipients + reservedAttemptRecipients
	var dayTotal, dayCold, dayDiagnostic int
	err = tx.QueryRow(ctx, `SELECT COUNT(*),COUNT(*) FILTER(WHERE task_type='campaign'),COUNT(*) FILTER(WHERE task_type IN ('warmup','placement','cloud_warmup')) FROM tasks t
	 WHERE email_account_id=$1 AND id<>$2 AND task_type IN ('campaign','warmup','placement','cloud_warmup')
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
	// Manual (unibox) mail is a conversation, not outreach: it spends none of
	// these allocations and was held to its own fair use above.
	if lane != "email" && (max(dayTotal, attemptedToday+reservedAttemptsToday) >= sharedCap || occurrences+len(recipients) > rollingCap || lane == "campaign" && dayCold >= campaignCap || (lane == "warmup" || lane == "placement" || lane == TaskTypeCloudWarmup) && dayDiagnostic >= warmupCap) {
		return uuid.Nil, ErrSendAdmissionDenied
	}
	if lane == "campaign" {
		if err = checkCampaignReservation(ctx, tx, in.TaskID); err != nil {
			return uuid.Nil, err
		}
		if err = checkMailboxSendingPlan(ctx, tx, in.TaskID, in.MailboxID); err != nil {
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
	 send_released_at=NULL,send_executor_nonce=$4,send_executor_worker=NULLIF($5::uuid,`+nilUUIDSQL+`),send_executor_started_at=NULL,send_executor_result=NULL,
	 send_result_state='unknown',status='completed',completed_at=CASE WHEN $6 THEN COALESCE(completed_at,NOW()) ELSE NOW() END WHERE id=$1`, in.TaskID, timezone, recipients, nonce, in.WorkerID, lane == "warmup")
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

// checkManualSendFairUse holds a manual send to the mailbox's fair-use
// allowance, decided from its provider, connection age, warmup standing and
// workspace risk. A reply or forward spends the daily and hourly allowance; a
// send that starts a conversation also spends the new-conversation one.
func checkManualSendFairUse(ctx context.Context, tx pgx.Tx, task, mailbox uuid.UUID, n int) error {
	var standing models.ManualSendStanding
	var provider, risk string
	var connectedSeconds float64
	err := tx.QueryRow(ctx, `SELECT ea.provider::text, ea.email, GREATEST(0,EXTRACT(EPOCH FROM NOW()-ea.created_at))::float8, o.risk_state,
	 COALESCE((SELECT h.state FROM (
	   SELECT p.health_state::text AS state FROM warmup_pool_participants p WHERE p.email_account_id=ea.id
	   UNION ALL SELECT l.health_state FROM warmup_reputation_ledger l WHERE l.organization_id=ea.organization_id AND l.email=lower(btrim(ea.email))
	 ) h ORDER BY array_position(ARRAY['blocked','quarantined','throttled','watch','healthy'], h.state) NULLS LAST LIMIT 1),'healthy')
	 FROM email_accounts ea JOIN organizations o ON o.id=ea.organization_id WHERE ea.id=$1`, mailbox).
		Scan(&provider, &standing.Address, &connectedSeconds, &risk, &standing.Health)
	if err != nil {
		return err
	}
	standing.Provider = models.InboxProvider(provider)
	standing.OrgRisk = models.OrgRiskState(risk)
	standing.ConnectedFor = time.Duration(connectedSeconds * float64(time.Second))

	// No email_tasks row (a test send) is treated as starting a conversation.
	var startsConversation bool
	err = tx.QueryRow(ctx, `SELECT NOT EXISTS(SELECT 1 FROM email_tasks et WHERE et.task_id=$1
	 AND (COALESCE(cardinality(et.in_reply_to),0)>0 OR et.thread_id IS NOT NULL OR et.forward_thread_id IS NOT NULL))`, task).Scan(&startsConversation)
	if err != nil {
		return err
	}
	var used models.ManualSendUsage
	err = tx.QueryRow(ctx, `SELECT COALESCE(SUM(s.n),0), COALESCE(SUM(s.n) FILTER(WHERE s.at>NOW()-INTERVAL '1 hour'),0), COALESCE(SUM(s.n) FILTER(WHERE s.fresh),0)
	 FROM (SELECT GREATEST(1,cardinality(t.send_recipients)) AS n, t.send_reserved_at AS at,
	   NOT (COALESCE(cardinality(et.in_reply_to),0)>0 OR et.thread_id IS NOT NULL OR et.forward_thread_id IS NOT NULL) AS fresh
	   FROM tasks t LEFT JOIN email_tasks et ON et.task_id=t.id
	   WHERE t.email_account_id=$1 AND t.id<>$2 AND t.task_type='email'
	   AND t.send_reserved_at>NOW()-INTERVAL '24 hours' AND t.send_released_at IS NULL) s`, mailbox, task).
		Scan(&used.Day, &used.Hour, &used.NewConversations)
	if err != nil {
		return err
	}
	allowance := models.ManualSendAllowanceFor(standing)
	if window, limit, over := allowance.Exceeded(used, n, startsConversation); over {
		return &ManualSendLimitError{Window: window, Limit: limit}
	}
	return nil
}

// checkManualSendPlan holds a workspace's manual sends, across all its
// mailboxes, to the allowance its plan gives. The workspace lock keeps two
// mailboxes from both taking the last of it.
func checkManualSendPlan(ctx context.Context, tx pgx.Tx, task, org uuid.UUID, n, allowance int) error {
	if allowance <= 0 {
		return nil
	}
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,274))`, org.String()); err != nil {
		return err
	}
	var used int
	err := tx.QueryRow(ctx, `SELECT COALESCE(SUM(GREATEST(1,cardinality(t.send_recipients))),0)
	 FROM tasks t JOIN email_accounts ea ON ea.id=t.email_account_id
	 WHERE ea.organization_id=$1 AND t.id<>$2 AND t.task_type='email'
	 AND t.send_reserved_at>NOW()-INTERVAL '24 hours' AND t.send_released_at IS NULL`, org, task).Scan(&used)
	if err != nil {
		return err
	}
	if used+n > allowance {
		return &ManualSendLimitError{Window: models.ManualSendWindowPlan, Limit: allowance}
	}
	return nil
}

// checkSendRecipients refuses a send to a suppressed recipient, naming the
// first one. A unibox forward is exempt: it relays mail already received to
// people the sender picked by hand, so it is not outreach.
func checkSendRecipients(ctx context.Context, tx pgx.Tx, task, org uuid.UUID, lane string, recipients []string) error {
	var match string
	err := tx.QueryRow(ctx, `SELECT r.recipient FROM unnest($2::text[]) WITH ORDINALITY r(recipient,i)
	 WHERE recipient_suppressed($1,r.recipient)
	 AND NOT ($4='email' AND EXISTS(SELECT 1 FROM email_tasks et WHERE et.task_id=$3
	   AND (et.forwarded_html<>'' OR et.forwarded_plain<>'' OR et.forward_thread_id IS NOT NULL)))
	 ORDER BY r.i LIMIT 1`, org, recipients, task, lane).Scan(&match)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	return &RecipientSuppressedError{Address: match}
}

func checkMailboxSendingPlan(ctx context.Context, tx pgx.Tx, task, mailbox uuid.UUID) error {
	var allowed bool
	err := tx.QueryRow(ctx, `SELECT NOT COALESCE(b.enabled,false) OR (p.email_account_id IS NOT NULL
	 AND p.is_working_day
	 AND EXTRACT(EPOCH FROM (NOW() AT TIME ZONE p.timezone)::time)/60>=p.work_start_minute
	 AND EXTRACT(EPOCH FROM (NOW() AT TIME ZONE p.timezone)::time)/60<p.work_end_minute
	 AND (p.lunch_start_minute IS NULL OR EXTRACT(EPOCH FROM (NOW() AT TIME ZONE p.timezone)::time)/60<p.lunch_start_minute
	 OR EXTRACT(EPOCH FROM (NOW() AT TIME ZONE p.timezone)::time)/60>=p.lunch_end_minute)
	 AND (SELECT COUNT(*)<p.daily_limit AND COUNT(*) FILTER(WHERE t.completed_at>=date_trunc('hour',NOW() AT TIME ZONE p.timezone) AT TIME ZONE p.timezone)<p.hourly_limit
	 FROM tasks t WHERE t.email_account_id=ea.id AND t.id<>$2 AND t.task_type IN ('campaign','placement') AND `+taskConsumesSendBudget+`
	 AND (t.completed_at AT TIME ZONE p.timezone)::date=p.plan_date AND `+taskDispatchedEmail+`))
	 FROM email_accounts ea JOIN organizations o ON o.id=ea.organization_id
	 LEFT JOIN email_account_behavior b ON b.email_account_id=ea.id
	 LEFT JOIN email_account_daily_plan p ON p.email_account_id=ea.id AND p.plan_date=(NOW() AT TIME ZONE COALESCE(NULLIF(ea.timezone,''),NULLIF(o.timezone,''),'UTC'))::date
	 WHERE ea.id=$1`, mailbox, task).Scan(&allowed)
	if err != nil {
		return err
	}
	if !allowed {
		return ErrMailboxSendingPlan
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
	var allowed, capacity bool
	err := tx.QueryRow(ctx, `SELECT c.status='active' AND (c.end_date IS NULL OR c.end_date>=NOW())
	 AND NOT recipient_suppressed(c.organization_id,contact.email) AND contact.subscribed IS DISTINCT FROM false
	 AND NOT EXISTS(SELECT 1 FROM campaign_leads h WHERE h.campaign_id=c.id AND h.contact_id=ct.contact_id AND h.paused_at IS NOT NULL AND (h.paused_until IS NULL OR h.paused_until>NOW()))
	 AND NOT EXISTS(SELECT 1 FROM campaign_leads recovery WHERE recovery.campaign_id=c.id AND recovery.contact_id=ct.contact_id AND recovery.send_outcome_pending_at IS NOT NULL)
	 AND c.organization_id=ea.organization_id AND contact.organization_id=c.organization_id
	 AND EXISTS(SELECT 1 FROM campaign_contact_progress p WHERE p.campaign_id=c.id AND p.contact_id=ct.contact_id AND p.sequence_id=ct.sequence_id AND p.dispatch_task_id=t.id),
	 c.daily_limit<=0 OR (SELECT COUNT(*) FROM tasks t JOIN campaign_tasks sent ON sent.task_id=t.id
	 WHERE sent.campaign_id=c.id AND t.email_account_id=ea.id AND t.id<>ct.task_id AND `+campaignSendToday+`)<c.daily_limit
	 FROM campaign_tasks ct JOIN tasks t ON t.id=ct.task_id JOIN email_accounts ea ON ea.id=t.email_account_id JOIN campaigns c ON c.id=ct.campaign_id JOIN contacts contact ON contact.id=ct.contact_id WHERE ct.task_id=$1`, task).Scan(&allowed, &capacity)
	if err != nil {
		return err
	}
	if !allowed {
		return ErrSendAdmissionDenied
	}
	if !capacity {
		return ErrCampaignDailyLimit
	}
	return nil
}

func (r *taskRepository) InspectOutbound(ctx context.Context, task, mailbox, worker uuid.UUID) (*WarmupDispatchState, error) {
	var nonce *uuid.UUID
	var started *time.Time
	var released bool
	var cancelled bool
	var raw []byte
	var org *uuid.UUID
	var recipients []string
	err := r.db.QueryRow(ctx, `SELECT t.send_executor_nonce,t.send_executor_started_at,t.send_executor_result,t.send_released_at IS NOT NULL,t.status='cancelled' OR t.campaign_send_isolated_at IS NOT NULL,ea.organization_id,t.send_recipients FROM tasks t JOIN email_accounts ea ON ea.id=t.email_account_id WHERE t.id=$1 AND t.email_account_id=$2 AND (t.send_executor_nonce IS NULL OR t.send_executor_worker=$3)`, task, mailbox, worker).Scan(&nonce, &started, &raw, &released, &cancelled, &org, &recipients)
	if errors.Is(err, pgx.ErrNoRows) {
		return &WarmupDispatchState{State: "denied"}, nil
	}
	if err != nil {
		return nil, err
	}
	if cancelled || released {
		return &WarmupDispatchState{State: "denied"}, nil
	}
	if nonce == nil {
		return &WarmupDispatchState{State: "legacy"}, nil
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
	 WHERE t.id=$1 AND ea.id=$2 AND t.send_executor_worker=$3 AND t.send_released_at IS NULL FOR UPDATE OF t`, task, mailbox, worker).Scan(&org, &provider, &recipients, &current, &started)
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
		err = checkSendRecipients(ctx, tx, task, org, lane, recipients)
	}
	if err == nil && lane == "campaign" {
		err = checkCampaignReservation(ctx, tx, task)
	}
	if err == nil && lane == "campaign" {
		err = checkMailboxSendingPlan(ctx, tx, task, mailbox)
	}
	if err == nil && lane == TaskTypeCloudWarmup {
		err = checkCloudSendCommand(ctx, tx, task, mailbox)
	}
	if err != nil {
		if !errors.Is(err, ErrSendAdmissionDenied) {
			return nil, err
		}
		reason := err
		if err = tx.Commit(ctx); err != nil {
			return nil, err
		}
		if err = r.cancelOutbound(ctx, task, mailbox, worker, nonce, reason); err != nil {
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
		tag, err := tx.Exec(ctx, `UPDATE warmup_tasks SET dispatch_started_at=NOW() WHERE task_id=$1 AND dispatch_nonce=$2
		 AND dispatch_worker_id=$3 AND dispatch_started_at IS NULL AND dispatch_result IS NULL`, task, nonce, worker)
		if err != nil {
			return nil, err
		}
		if tag.RowsAffected() != 1 {
			return &WarmupDispatchState{State: "denied"}, nil
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
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,268))`, task.String()); err != nil {
		return err
	}
	var previous []byte
	err = tx.QueryRow(ctx, `SELECT send_executor_result FROM tasks WHERE id=$1 AND email_account_id=$2 AND send_executor_worker=$3 AND send_executor_started_at IS NOT NULL FOR UPDATE`, task, mailbox, worker).Scan(&previous)
	if err != nil {
		return err
	}
	if len(previous) > 0 {
		var recorded models.SendEmailResult
		if json.Unmarshal(previous, &recorded) != nil || recorded.TaskID != task {
			return errors.New("invalid stored dispatch result")
		}
		canonical, _ := json.Marshal(recorded)
		if string(canonical) == string(raw) {
			return nil
		}
		if resultState(recorded) != "unknown" || resultState(result) == "unknown" {
			return errors.New("conflicting dispatch result")
		}
	}
	if _, err = tx.Exec(ctx, `UPDATE tasks SET send_executor_result=$2 WHERE id=$1`, task, raw); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (r *taskRepository) CancelOutbound(ctx context.Context, task, mailbox, worker, nonce uuid.UUID) error {
	return r.cancelOutbound(ctx, task, mailbox, worker, nonce, nil)
}

func (r *taskRepository) cancelOutbound(ctx context.Context, task, mailbox, worker, nonce uuid.UUID, reason error) error {
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
	 WHERE id=$1 AND email_account_id=$2 AND send_executor_worker=$3 AND send_executor_nonce=$4 AND send_executor_started_at IS NULL AND send_released_at IS NULL
	 AND (task_type<>'warmup' OR EXISTS(SELECT 1 FROM warmup_tasks w WHERE w.task_id=tasks.id AND w.dispatch_nonce=$4
	 AND w.dispatch_worker_id=$3 AND w.dispatch_started_at IS NULL AND w.dispatch_result IS NULL)) RETURNING task_type`, task, mailbox, worker, nonce).Scan(&lane)
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
		if reason != nil {
			if _, err = tx.Exec(ctx, `INSERT INTO task_failures(task_id,title,message) VALUES($1,'Send admission denied',$2)
			 ON CONFLICT(task_id) DO UPDATE SET title=EXCLUDED.title,message=EXCLUDED.message`, task, reason.Error()); err != nil {
				return err
			}
			metadata := map[string]any{"level": "error", "code": SendAdmissionCode(reason), "contact_id": contact.String(), "sequence_id": sequence.String(), "account_id": mailbox.String(), "task_id": task.String(), "error": reason.Error(), "will_retry": true}
			if _, err = tx.Exec(ctx, `INSERT INTO campaign_logs(campaign_id,event_type,message,metadata) VALUES($1,'email_failed',$2,$3)`, c, "Send admission denied before worker execution, will retry: "+reason.Error(), metadata); err != nil {
				return err
			}
		}
	}
	if lane == "warmup" {
		if err = refundWarmupCharge(ctx, tx, task, mailbox); err != nil {
			return err
		}
		if _, err = tx.Exec(ctx, `DELETE FROM warmup_tokens WHERE task_id=$1`, task); err != nil {
			return err
		}
	}
	if err = refreshUnknownSendHold(ctx, tx, mailbox, task); err != nil {
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
	var receiving bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM email_accounts ea JOIN warmup_pool_participants wpp ON wpp.email_account_id=ea.id WHERE ea.id=$1 AND `+partnerEligibleSQL+`)`, recipient).Scan(&receiving); err != nil {
		return err
	}
	if !receiving {
		return ErrSendAdmissionDenied
	}
	var allowed bool
	// The send's own token is already counted, so it needs no extra room.
	err := tx.QueryRow(ctx, warmupInboxRoomSQL, recipient, inboundSharePercent(pool), 0).Scan(&allowed)
	if err != nil {
		return err
	}
	if !allowed {
		return ErrSendAdmissionDenied
	}
	return nil
}

// checkCloudSendCommand admits a cloud_warmup send only on the live Cloud
// command that created it, for a mailbox this instance executes.
func checkCloudSendCommand(ctx context.Context, tx pgx.Tx, task, mailbox uuid.UUID) error {
	var a cloudCommandAdmission
	var executor *string
	err := tx.QueryRow(ctx, `SELECT c.executor,k.kind,k.state,k.admit_until FROM cloud_link_commands k
	 LEFT JOIN cloud_link_mailboxes c ON c.email_account_id=k.email_account_id AND c.instance_id=k.instance_id
	 WHERE k.task_id=$1 AND k.email_account_id=$2`, task, mailbox).Scan(&executor, &a.Kind, &a.State, &a.AdmitUntil)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrSendAdmissionDenied
	}
	if err != nil {
		return err
	}
	if executor != nil {
		a.Linked, a.Executor = true, models.PoolLinkExecutor(*executor)
	}
	if !cloudSendAdmitted(a, time.Now()) {
		return ErrSendAdmissionDenied
	}
	return nil
}
