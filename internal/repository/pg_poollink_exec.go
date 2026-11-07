package repository

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/warmbly/warmbly/internal/config"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/infrastructure/db"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/utils"
)

// nilUUIDSQL is the worker id a self-executed warmup send is reserved under.
const nilUUIDSQL = `'00000000-0000-0000-0000-000000000000'::uuid`

var executorLivenessSQL = `make_interval(mins => ` + strconv.Itoa(config.PoolLinkExecutorLivenessMinutes) + `)`

// instanceExecutedSQL holds for a mailbox a linked instance executes.
func instanceExecutedSQL(account string) string {
	return `EXISTS (SELECT 1 FROM pool_link_mailboxes plx WHERE plx.email_account_id = ` + account + ` AND plx.executor = 'instance')`
}

// selfExecutionLiveSQL holds for an instance-executed mailbox whose link is
// unrevoked, leased recently, and whose workspace holds an active grant.
func selfExecutionLiveSQL(account string) string {
	return `EXISTS (SELECT 1 FROM pool_link_mailboxes plx
		JOIN pool_link_instances pli ON pli.id = plx.instance_id
		JOIN email_accounts plea ON plea.id = plx.email_account_id AND plea.organization_id = pli.organization_id
		JOIN pool_link_self_execution_grants plg ON plg.organization_id = pli.organization_id
		WHERE plx.email_account_id = ` + account + ` AND plx.executor = 'instance'
		  AND pli.revoked_at IS NULL AND pli.executor_seen_at > NOW() - ` + executorLivenessSQL + `
		  AND (plg.expires_at IS NULL OR plg.expires_at > NOW()))`
}

// selfExecutionGateSQL lets a Cloud-executed mailbox through and an
// instance-executed one only while its executor is live and granted.
func selfExecutionGateSQL(account string) string {
	return `(NOT ` + instanceExecutedSQL(account) + ` OR ` + selfExecutionLiveSQL(account) + `)`
}

// selfExecPausedSQL names why the instance-executed mailbox row m is paused, or ”.
func selfExecPausedSQL(m string) string {
	return `CASE WHEN ` + m + `.executor <> 'instance' THEN ''
		WHEN NOT EXISTS (SELECT 1 FROM pool_link_instances ppi
			JOIN pool_link_self_execution_grants ppg ON ppg.organization_id = ppi.organization_id
			WHERE ppi.id = ` + m + `.instance_id AND (ppg.expires_at IS NULL OR ppg.expires_at > NOW())) THEN '` + models.PoolLinkPausedNotGranted + `'
		WHEN NOT EXISTS (SELECT 1 FROM pool_link_instances ppi WHERE ppi.id = ` + m + `.instance_id
			AND ppi.executor_seen_at > NOW() - ` + executorLivenessSQL + `) THEN '` + models.PoolLinkPausedExecutorOffline + `'
		ELSE '' END`
}

// trustedReporterSQL holds unless the receiving mailbox is an instance-executed, untrusted placement reporter.
func trustedReporterSQL(account string) string {
	return `NOT (` + instanceExecutedSQL(account) + ` AND EXISTS (SELECT 1 FROM pool_link_reporter_trust prt WHERE prt.email_account_id = ` + account + ` AND prt.trust_state = '` + models.PoolLinkReportUntrusted + `'))`
}

// actionWorkerSQL is where a mailbox's warmup actions go: the nil worker when an instance executes it, else its worker.
func actionWorkerSQL(ea string) string {
	return `CASE WHEN ` + instanceExecutedSQL(ea+".id") + ` THEN ` + nilUUIDSQL + ` ELSE ` + ea + `.worker_id END`
}

// actionRoutableSQL holds when a mailbox's warmup actions have somewhere to go.
func actionRoutableSQL(ea string) string {
	return `((` + ea + `.worker_id IS NOT NULL AND NOT ` + instanceExecutedSQL(ea+".id") + `) OR ` + selfExecutionLiveSQL(ea+".id") + `)`
}

var (
	ErrPoolLinkCommandNotFound = errors.New("pool link command not found")
	ErrPoolLinkNotSelfExecuted = errors.New("mailbox is not executed by this instance")
)

// PoolLinkExecMailbox is an instance-executed mailbox and the link that runs it.
type PoolLinkExecMailbox struct {
	InstanceID     uuid.UUID
	RemoteID       uuid.UUID
	EmailAccountID uuid.UUID
	OrganizationID uuid.UUID
	UserID         uuid.UUID
	Email          string
	Provider       string
}

// InstanceExecutedEnrollment creates a Cloud mailbox that holds no credential.
type InstanceExecutedEnrollment struct {
	InstanceID     uuid.UUID
	RemoteID       uuid.UUID
	OrganizationID uuid.UUID
	UserID         string
	Email          string
	Name           string
	Provider       models.InboxProvider
	MailHost       string
}

// QueuedPoolLinkCommand is a command about to join a link's queue; Payload is sealed.
type QueuedPoolLinkCommand struct {
	ID             uuid.UUID
	InstanceID     uuid.UUID
	EmailAccountID uuid.UUID
	OrganizationID uuid.UUID
	Kind           string
	TaskID         *uuid.UUID
	DispatchNonce  *uuid.UUID
	DedupeKey      string
	Payload        string
	NotAfter       time.Time
}

// StoredPoolLinkCommand is a queued command as stored.
type StoredPoolLinkCommand struct {
	ID             uuid.UUID
	InstanceID     uuid.UUID
	RemoteID       uuid.UUID
	EmailAccountID uuid.UUID
	OrganizationID uuid.UUID
	Kind           string
	TaskID         *uuid.UUID
	DispatchNonce  *uuid.UUID
	Payload        string
	Status         string
	NotAfter       time.Time
	LeasedUntil    *time.Time
	LeaseCount     int
	Outcome        *string
	Result         []byte
	CreatedAt      time.Time
}

// PoolLinkCommandAnswer is what an answer stores; Publish runs before commit.
type PoolLinkCommandAnswer struct {
	Status     string
	Outcome    string
	Result     json.RawMessage
	SendResult *models.SendEmailResult
	Publish    func(context.Context) error
}

// PoolLinkArrival is reported warmup mail Cloud accepted.
type PoolLinkArrival struct {
	EmailAccountID  uuid.UUID
	MessageHandle   string
	MessageID       string
	InternalID      uuid.UUID
	SenderAccountID *uuid.UUID
	LandedSpam      bool
	ReceivedAt      time.Time
}

// ReporterTrustSample is one mailbox's placement reports against what Cloud saw for the same senders.
type ReporterTrustSample struct {
	Reported     int
	ReportedSpam int
	Observed     int
	ExpectedSpam float64
}

// PoolLinkExecRepository is the Cloud side of instance-executed warmup.
type PoolLinkExecRepository interface {
	InstanceExecuted(ctx context.Context, accountID uuid.UUID) (*PoolLinkExecMailbox, error)
	SelfExecutionLive(ctx context.Context, accountID uuid.UUID) (bool, error)
	EnrollInstanceExecuted(ctx context.Context, in InstanceExecutedEnrollment) (*models.PoolLinkMailbox, error)
	ConvertToInstance(ctx context.Context, instanceID, remoteID uuid.UUID) (*uuid.UUID, error)
	ConvertToCloud(ctx context.Context, instanceID, remoteID uuid.UUID) error
	CancelOpenCommands(ctx context.Context, accountID uuid.UUID) error

	EnqueueCommand(ctx context.Context, c QueuedPoolLinkCommand) (bool, error)
	LeaseCommands(ctx context.Context, instanceID uuid.UUID, limit int, until time.Time) ([]StoredPoolLinkCommand, error)
	ReleaseCommandLease(ctx context.Context, id uuid.UUID) error
	CancelCommand(ctx context.Context, id uuid.UUID) error
	BeginSelfExecutedSend(ctx context.Context, task, mailbox, nonce uuid.UUID) (bool, error)
	CancelSelfExecutedSend(ctx context.Context, task, mailbox, nonce uuid.UUID) error
	AnswerCommand(ctx context.Context, instanceID, id uuid.UUID, decide func(*StoredPoolLinkCommand) (*PoolLinkCommandAnswer, error)) error
	ExpireCommands(ctx context.Context, limit int) ([]StoredPoolLinkCommand, error)
	WarmupTaskSend(ctx context.Context, task, sender uuid.UUID) (WarmupTaskSend, error)

	RecordArrival(ctx context.Context, a PoolLinkArrival) (bool, error)
	DeleteArrival(ctx context.Context, accountID uuid.UUID, handle string) error
	GetArrival(ctx context.Context, accountID uuid.UUID, handle string) (*PoolLinkArrival, error)
	ArrivalForMessage(ctx context.Context, accountID uuid.UUID, messageID string) (*PoolLinkArrival, error)
	ArrivalHandle(ctx context.Context, accountID uuid.UUID, internalID *uuid.UUID, messageID string) (string, error)

	ListInstanceExecutedAccounts(ctx context.Context) ([]uuid.UUID, error)
	ReporterTrustState(ctx context.Context, accountID uuid.UUID) (string, error)
	SampleReporterTrust(ctx context.Context, accountID uuid.UUID, since time.Time) (ReporterTrustSample, error)
	SaveReporterTrust(ctx context.Context, accountID uuid.UUID, s ReporterTrustSample, state string) (string, error)
	ListSelfExecutionMailboxes(ctx context.Context, orgID uuid.UUID) ([]models.PoolLinkSelfExecutionMailbox, error)
}

func (r *poolLinkRepository) InstanceExecuted(ctx context.Context, accountID uuid.UUID) (*PoolLinkExecMailbox, error) {
	var m PoolLinkExecMailbox
	var userID string
	err := r.db.QueryRow(ctx, `SELECT plx.instance_id, plx.remote_id, ea.id, ea.organization_id, ea.user_id::text, ea.email, ea.provider::text
		FROM pool_link_mailboxes plx JOIN pool_link_instances pli ON pli.id = plx.instance_id
		JOIN email_accounts ea ON ea.id = plx.email_account_id AND ea.organization_id = pli.organization_id
		WHERE plx.email_account_id = $1 AND plx.executor = 'instance'`, accountID).
		Scan(&m.InstanceID, &m.RemoteID, &m.EmailAccountID, &m.OrganizationID, &userID, &m.Email, &m.Provider)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	m.UserID, _ = uuid.Parse(userID)
	return &m, nil
}

func (r *poolLinkRepository) SelfExecutionLive(ctx context.Context, accountID uuid.UUID) (bool, error) {
	var live bool
	err := r.db.QueryRow(ctx, `SELECT `+selfExecutionLiveSQL("$1::uuid"), accountID).Scan(&live)
	return live, err
}

// EnrollInstanceExecuted creates the credential-free mailbox and its link row in one transaction, so no reconciler sees one without the other.
func (r *poolLinkRepository) EnrollInstanceExecuted(ctx context.Context, in InstanceExecutedEnrollment) (*models.PoolLinkMailbox, error) {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)
	var live uuid.UUID
	err = tx.QueryRow(ctx, `SELECT id FROM pool_link_instances WHERE id = $1 AND organization_id = $2 AND revoked_at IS NULL FOR SHARE`, in.InstanceID, in.OrganizationID).Scan(&live)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrPoolLinkInstanceRevoked
	}
	if err != nil {
		return nil, err
	}
	org := in.OrganizationID
	if xerr := ensureMailboxAbsentTx(ctx, tx, &org, in.Email); xerr != nil {
		return nil, xerr
	}
	authMethod := models.MailAuthOAuth
	if in.Provider == models.InboxProviderSMTPIMAP {
		authMethod = models.MailAuthPassword
	}
	id := uuid.New()
	now := time.Now()
	query := withNewMailboxBehavior(`
		INSERT INTO email_accounts (id, user_id, organization_id, email, name, provider, signature_plain, signature_html, tracking_domain, last_synced_at, created_at, updated_at, warmup_tag, mail_host, auth_method)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, '', $9, $9, $9, '', $10, $11)
	`, false)
	if _, err := tx.Exec(ctx, query, id, in.UserID, org, in.Email, in.Name, in.Provider,
		utils.GetSignaturePlain(in.Name), utils.GetSignatureHTML(in.Name), now, in.MailHost, authMethod); err != nil {
		db.CaptureError(err, query, nil, "exec")
		return nil, err
	}
	if xerr := setNewMailboxVolumeTx(ctx, tx, id, in.MailHost, string(in.Provider), in.Email); xerr != nil {
		return nil, xerr
	}
	m := &models.PoolLinkMailbox{InstanceID: in.InstanceID, RemoteID: in.RemoteID, EmailAccountID: id, Executor: models.PoolLinkExecutorInstance}
	if err := tx.QueryRow(ctx, `INSERT INTO pool_link_mailboxes (instance_id, remote_id, email_account_id, managed, executor)
		VALUES ($1, $2, $3, false, 'instance') RETURNING enrolled_at`, in.InstanceID, in.RemoteID, id).Scan(&m.EnrolledAt); err != nil {
		return nil, err
	}
	return m, tx.Commit(ctx)
}

// ConvertToInstance hands a Cloud-executed mailbox to its instance: Cloud's
// credential is deleted and the mailbox leaves its worker, which is returned.
func (r *poolLinkRepository) ConvertToInstance(ctx context.Context, instanceID, remoteID uuid.UUID) (*uuid.UUID, error) {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)
	var account uuid.UUID
	err = tx.QueryRow(ctx, `UPDATE pool_link_mailboxes m SET executor = 'instance'
		FROM pool_link_instances i WHERE i.id = m.instance_id AND i.revoked_at IS NULL
		  AND m.instance_id = $1 AND m.remote_id = $2 AND NOT m.managed
		RETURNING m.email_account_id`, instanceID, remoteID).Scan(&account)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrPoolLinkNotSelfExecuted
	}
	if err != nil {
		return nil, err
	}
	var worker *uuid.UUID
	if err := tx.QueryRow(ctx, `SELECT worker_id FROM email_accounts WHERE id = $1 FOR UPDATE`, account).Scan(&worker); err != nil {
		return nil, err
	}
	for _, q := range []string{
		`DELETE FROM email_accounts_oauth WHERE email_account_id = $1`,
		`DELETE FROM email_accounts_smtp_imap WHERE email_account_id = $1`,
		`UPDATE email_accounts SET worker_id = NULL, worker_assigned_at = NULL, updated_at = NOW() WHERE id = $1`,
	} {
		if _, err := tx.Exec(ctx, q, account); err != nil {
			return nil, err
		}
	}
	if worker != nil {
		if _, err := tx.Exec(ctx, `UPDATE workers SET account_count = GREATEST(account_count - 1, 0), updated_at = NOW() WHERE id = $1`, *worker); err != nil {
			return nil, err
		}
	}
	return worker, tx.Commit(ctx)
}

// ConvertToCloud returns a mailbox to Cloud execution; its credential was written first and its pending commands are cancelled with their reservations.
func (r *poolLinkRepository) ConvertToCloud(ctx context.Context, instanceID, remoteID uuid.UUID) error {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	var account uuid.UUID
	err = tx.QueryRow(ctx, `UPDATE pool_link_mailboxes SET executor = 'cloud' WHERE instance_id = $1 AND remote_id = $2 RETURNING email_account_id`, instanceID, remoteID).Scan(&account)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrPoolLinkNotSelfExecuted
	}
	if err != nil {
		return err
	}
	if err := leaveSelfExecutionTx(ctx, tx, account); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// CancelOpenCommands closes what a mailbox leaving self-execution still had pending.
func (r *poolLinkRepository) CancelOpenCommands(ctx context.Context, accountID uuid.UUID) error {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if err := leaveSelfExecutionTx(ctx, tx, accountID); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// leaveSelfExecutionTx cancels pending commands, releasing each pending send's reservation, and drops the reporter standing.
func leaveSelfExecutionTx(ctx context.Context, tx pgx.Tx, account uuid.UUID) error {
	rows, err := tx.Query(ctx, `UPDATE pool_link_commands SET status = 'cancelled'
		WHERE email_account_id = $1 AND status = 'pending' RETURNING task_id, dispatch_nonce`, account)
	if err != nil {
		return err
	}
	type pendingSend struct{ task, nonce uuid.UUID }
	var sends []pendingSend
	for rows.Next() {
		var task, nonce *uuid.UUID
		if err := rows.Scan(&task, &nonce); err != nil {
			rows.Close()
			return err
		}
		if task != nil && nonce != nil {
			sends = append(sends, pendingSend{*task, *nonce})
		}
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	for _, s := range sends {
		if err := lockSend(ctx, tx, s.task, account); err != nil {
			return err
		}
		if err := cancelSelfExecutedSendTx(ctx, tx, s.task, account, s.nonce); err != nil {
			return err
		}
	}
	_, err = tx.Exec(ctx, `DELETE FROM pool_link_reporter_trust WHERE email_account_id = $1`, account)
	return err
}

// EnqueueCommand adds a command to its link's queue; false when an open command already holds its task or dedupe key.
func (r *poolLinkRepository) EnqueueCommand(ctx context.Context, c QueuedPoolLinkCommand) (bool, error) {
	tag, err := r.db.Exec(ctx, `INSERT INTO pool_link_commands (id, instance_id, email_account_id, organization_id, kind, task_id, dispatch_nonce, dedupe_key, payload, not_after)
		SELECT $1, m.instance_id, m.email_account_id, ea.organization_id, $5, $6, $7, $8, $9, $10
		FROM pool_link_mailboxes m JOIN email_accounts ea ON ea.id = m.email_account_id
		WHERE m.instance_id = $2 AND m.email_account_id = $3 AND ea.organization_id = $4 AND m.executor = 'instance'
		ON CONFLICT DO NOTHING`,
		c.ID, c.InstanceID, c.EmailAccountID, c.OrganizationID, c.Kind, c.TaskID, c.DispatchNonce, c.DedupeKey, c.Payload, c.NotAfter)
	if err != nil {
		return false, err
	}
	return tag.RowsAffected() == 1, nil
}

const storedCommandColumns = `c.id, c.instance_id, m.remote_id, c.email_account_id, c.organization_id, c.kind, c.task_id, c.dispatch_nonce,
	c.payload, c.status, c.not_after, c.leased_until, c.lease_count, c.outcome, c.result, c.created_at`

func scanStoredCommand(row pgx.Row) (*StoredPoolLinkCommand, error) {
	var c StoredPoolLinkCommand
	var remote *uuid.UUID
	if err := row.Scan(&c.ID, &c.InstanceID, &remote, &c.EmailAccountID, &c.OrganizationID, &c.Kind, &c.TaskID, &c.DispatchNonce,
		&c.Payload, &c.Status, &c.NotAfter, &c.LeasedUntil, &c.LeaseCount, &c.Outcome, &c.Result, &c.CreatedAt); err != nil {
		return nil, err
	}
	if remote != nil {
		c.RemoteID = *remote
	}
	return &c, nil
}

func collectStoredCommands(rows pgx.Rows) ([]StoredPoolLinkCommand, error) {
	defer rows.Close()
	var out []StoredPoolLinkCommand
	for rows.Next() {
		c, err := scanStoredCommand(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *c)
	}
	return out, rows.Err()
}

// LeaseCommands stamps the link's executor liveness and leases its due commands.
func (r *poolLinkRepository) LeaseCommands(ctx context.Context, instanceID uuid.UUID, limit int, until time.Time) ([]StoredPoolLinkCommand, error) {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, `UPDATE pool_link_instances SET executor_seen_at = NOW() WHERE id = $1 AND revoked_at IS NULL`, instanceID); err != nil {
		return nil, err
	}
	rows, err := tx.Query(ctx, `WITH due AS (
		SELECT id FROM pool_link_commands
		WHERE instance_id = $1 AND not_after > NOW()
		  AND (status = 'pending' OR (status = 'leased' AND leased_until <= NOW()))
		ORDER BY created_at LIMIT $2 FOR UPDATE SKIP LOCKED
	), leased AS (
		UPDATE pool_link_commands c SET status = 'leased', leased_until = $3, lease_count = c.lease_count + 1
		FROM due WHERE c.id = due.id RETURNING c.*
	)
	SELECT `+storedCommandColumns+` FROM leased c LEFT JOIN pool_link_mailboxes m ON m.email_account_id = c.email_account_id AND m.instance_id = c.instance_id
	ORDER BY c.created_at`, instanceID, limit, until)
	if err != nil {
		return nil, err
	}
	out, err := collectStoredCommands(rows)
	if err != nil {
		return nil, err
	}
	return out, tx.Commit(ctx)
}

func (r *poolLinkRepository) ReleaseCommandLease(ctx context.Context, id uuid.UUID) error {
	_, err := r.db.Exec(ctx, `UPDATE pool_link_commands SET status = 'pending', leased_until = NULL WHERE id = $1 AND status = 'leased'`, id)
	return err
}

func (r *poolLinkRepository) CancelCommand(ctx context.Context, id uuid.UUID) error {
	_, err := r.db.Exec(ctx, `UPDATE pool_link_commands SET status = 'cancelled' WHERE id = $1 AND status IN ('pending', 'leased')`, id)
	return err
}

// BeginSelfExecutedSend is BeginOutbound for a send no worker holds: it starts
// the reserved send once, under the send locks, or retires a reservation the
// mailbox may no longer use. True when the send is started.
func (r *poolLinkRepository) BeginSelfExecutedSend(ctx context.Context, task, mailbox, nonce uuid.UUID) (bool, error) {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return false, err
	}
	defer tx.Rollback(ctx)
	if err = lockSend(ctx, tx, task, mailbox); err != nil {
		return false, err
	}
	var org uuid.UUID
	var provider string
	var recipients []string
	var current *uuid.UUID
	var started *time.Time
	err = tx.QueryRow(ctx, `SELECT ea.organization_id, ea.provider::text, t.send_recipients, t.send_executor_nonce, t.send_executor_started_at
		FROM tasks t JOIN email_accounts ea ON ea.id = t.email_account_id
		WHERE t.id = $1 AND ea.id = $2 AND t.send_executor_worker IS NULL AND t.send_released_at IS NULL AND t.task_type = 'warmup'
		FOR UPDATE OF t`, task, mailbox).Scan(&org, &provider, &recipients, &current, &started)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if current == nil || *current != nonce {
		return false, nil
	}
	if started != nil {
		return true, nil
	}
	lane, err := sendAuthority(ctx, tx, task, mailbox, org, uuid.Nil, provider, true)
	if err == nil && lane != "warmup" {
		err = ErrSendAdmissionDenied
	}
	if err == nil {
		err = checkSendRecipients(ctx, tx, task, org, lane, recipients)
	}
	if err == nil {
		var allowed bool
		if err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM warmup_pool_participants wpp JOIN email_accounts ea ON ea.id=wpp.email_account_id WHERE ea.id=$2 AND wpp.participant_role='sender_receiver' AND `+testSenderSQL+` AND `+poolEligibleSQL+`)
		AND EXISTS(SELECT 1 FROM warmup_tokens wt JOIN email_accounts ea ON ea.id=wt.recipient_account_id JOIN warmup_pool_participants wpp ON wpp.email_account_id=ea.id WHERE wt.task_id=$1 AND `+partnerEligibleSQL+` AND NOT `+instanceExecutedSQL("ea.id")+`)`, task, mailbox).Scan(&allowed); err == nil && !allowed {
			err = ErrSendAdmissionDenied
		}
	}
	if err != nil {
		if !errors.Is(err, ErrSendAdmissionDenied) {
			return false, err
		}
		if err := cancelSelfExecutedSendTx(ctx, tx, task, mailbox, nonce); err != nil {
			return false, err
		}
		return false, tx.Commit(ctx)
	}
	tag, err := tx.Exec(ctx, `UPDATE warmup_tasks SET dispatch_started_at=NOW() WHERE task_id=$1 AND dispatch_nonce=$2
		AND dispatch_worker_id IS NULL AND dispatch_started_at IS NULL AND dispatch_result IS NULL`, task, nonce)
	if err != nil {
		return false, err
	}
	if tag.RowsAffected() != 1 {
		return false, nil
	}
	if _, err = tx.Exec(ctx, `UPDATE tasks SET send_executor_started_at=NOW() WHERE id=$1`, task); err != nil {
		return false, err
	}
	if _, err = tx.Exec(ctx, `INSERT INTO outbound_attempts(nonce,task_id,email_account_id,provider,attempted_at,recipient_count) VALUES($1,$2,$3,$4,NOW(),$5)`, nonce, task, mailbox, provider, len(recipients)); err != nil {
		return false, err
	}
	return true, tx.Commit(ctx)
}

// CancelSelfExecutedSend retires an unstarted self-executed reservation: refund, tokens and hold, as CancelOutbound does.
func (r *poolLinkRepository) CancelSelfExecutedSend(ctx context.Context, task, mailbox, nonce uuid.UUID) error {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if err = lockSend(ctx, tx, task, mailbox); err != nil {
		return err
	}
	if err := cancelSelfExecutedSendTx(ctx, tx, task, mailbox, nonce); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func cancelSelfExecutedSendTx(ctx context.Context, tx pgx.Tx, task, mailbox, nonce uuid.UUID) error {
	tag, err := tx.Exec(ctx, `UPDATE tasks SET send_released_at=NOW(),send_result_state='failed',send_result_applied_at=NOW(),status='cancelled'
		WHERE id=$1 AND email_account_id=$2 AND task_type='warmup' AND send_executor_worker IS NULL AND send_executor_nonce=$3
		AND send_executor_started_at IS NULL AND send_released_at IS NULL
		AND EXISTS(SELECT 1 FROM warmup_tasks w WHERE w.task_id=tasks.id AND w.dispatch_nonce=$3
		AND w.dispatch_worker_id IS NULL AND w.dispatch_started_at IS NULL AND w.dispatch_result IS NULL)`, task, mailbox, nonce)
	if err != nil || tag.RowsAffected() == 0 {
		return err
	}
	if err = refundWarmupCharge(ctx, tx, task, mailbox); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `DELETE FROM warmup_tokens WHERE task_id=$1`, task); err != nil {
		return err
	}
	return refreshUnknownSendHold(ctx, tx, mailbox, task)
}

// AnswerCommand locks this link's command, lets decide choose what to store,
// and stores a send's result where the consumer's stored-result replay
// applies it, all in one transaction.
func (r *poolLinkRepository) AnswerCommand(ctx context.Context, instanceID, id uuid.UUID, decide func(*StoredPoolLinkCommand) (*PoolLinkCommandAnswer, error)) error {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	c, err := scanStoredCommand(tx.QueryRow(ctx, `SELECT `+storedCommandColumns+` FROM pool_link_commands c
		LEFT JOIN pool_link_mailboxes m ON m.email_account_id = c.email_account_id AND m.instance_id = c.instance_id
		WHERE c.id = $1 AND c.instance_id = $2 FOR UPDATE OF c`, id, instanceID))
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrPoolLinkCommandNotFound
	}
	if err != nil {
		return err
	}
	answer, err := decide(c)
	if err != nil || answer == nil {
		return err
	}
	if answer.SendResult != nil && c.TaskID != nil && c.DispatchNonce != nil {
		if err := lockSend(ctx, tx, *c.TaskID, c.EmailAccountID); err != nil {
			return err
		}
		raw, err := json.Marshal(answer.SendResult)
		if err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `UPDATE tasks SET send_executor_result=$4 WHERE id=$1 AND email_account_id=$2 AND send_executor_nonce=$3
			AND send_executor_worker IS NULL AND send_executor_started_at IS NOT NULL AND send_executor_result IS NULL`,
			*c.TaskID, c.EmailAccountID, *c.DispatchNonce, raw); err != nil {
			return err
		}
	}
	if _, err := tx.Exec(ctx, `UPDATE pool_link_commands SET status = $2, outcome = $3, result = $4, answered_at = COALESCE(answered_at, NOW()) WHERE id = $1`,
		c.ID, answer.Status, answer.Outcome, []byte(answer.Result)); err != nil {
		return err
	}
	if answer.Publish != nil {
		if err := answer.Publish(ctx); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

// ExpireCommands closes commands past their deadline that no live lease still holds.
func (r *poolLinkRepository) ExpireCommands(ctx context.Context, limit int) ([]StoredPoolLinkCommand, error) {
	rows, err := r.db.Query(ctx, `WITH due AS (
		SELECT id FROM pool_link_commands
		WHERE not_after <= NOW() AND (status = 'pending' OR (status = 'leased' AND leased_until <= NOW()))
		ORDER BY not_after LIMIT $1 FOR UPDATE SKIP LOCKED
	), expired AS (
		UPDATE pool_link_commands c SET status = 'expired', outcome = 'expired', answered_at = NOW()
		FROM due WHERE c.id = due.id RETURNING c.*
	)
	SELECT `+storedCommandColumns+` FROM expired c LEFT JOIN pool_link_mailboxes m ON m.email_account_id = c.email_account_id AND m.instance_id = c.instance_id`, limit)
	if err != nil {
		return nil, err
	}
	return collectStoredCommands(rows)
}

// WarmupTaskSend is what Cloud recorded about the send behind a warmup token.
type WarmupTaskSend struct {
	MintedID       string // the Message-ID the send was built with
	Settled        bool   // no result is still to come
	SentRecorded   bool   // a sent result has been applied
	CloudExecuted  bool   // a Cloud worker took the send
	SenderExecuted bool   // a linked instance executes the sender
}

// WarmupTaskSend reads the send behind task and whether a linked instance executes sender.
func (r *poolLinkRepository) WarmupTaskSend(ctx context.Context, task, sender uuid.UUID) (WarmupTaskSend, error) {
	var s WarmupTaskSend
	err := r.db.QueryRow(ctx, `SELECT COALESCE(t.message_id, ''),
		COALESCE(t.send_result_state = 'sent' AND t.send_result_applied_at IS NOT NULL, false)
		  OR EXISTS (SELECT 1 FROM warmup_tokens wt WHERE wt.task_id = t.id AND wt.sent_message_id <> ''),
		t.id IS NULL OR t.send_result_applied_at IS NOT NULL,
		t.send_executor_worker IS NOT NULL OR EXISTS (SELECT 1 FROM warmup_tasks w WHERE w.task_id = t.id AND w.dispatch_worker_id IS NOT NULL),
		`+instanceExecutedSQL("$2::uuid")+`
		FROM (SELECT 1) one LEFT JOIN tasks t ON t.id = $1 AND t.task_type = 'warmup'`, task, sender).
		Scan(&s.MintedID, &s.SentRecorded, &s.Settled, &s.CloudExecuted, &s.SenderExecuted)
	s.Settled = s.Settled || s.SentRecorded
	return s, err
}

// RecordArrival stores accepted warmup mail by handle; false when the handle or its message was already reported.
func (r *poolLinkRepository) RecordArrival(ctx context.Context, a PoolLinkArrival) (bool, error) {
	tag, err := r.db.Exec(ctx, `INSERT INTO pool_link_arrivals (email_account_id, message_handle, message_id, internal_id, sender_account_id, landed_spam, received_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
		ON CONFLICT DO NOTHING`, a.EmailAccountID, a.MessageHandle, a.MessageID, a.InternalID, a.SenderAccountID, a.LandedSpam, a.ReceivedAt)
	if err != nil {
		return false, err
	}
	return tag.RowsAffected() == 1, nil
}

func (r *poolLinkRepository) DeleteArrival(ctx context.Context, accountID uuid.UUID, handle string) error {
	_, err := r.db.Exec(ctx, `DELETE FROM pool_link_arrivals WHERE email_account_id = $1 AND message_handle = $2`, accountID, handle)
	return err
}

const arrivalColumns = `email_account_id, message_handle, message_id, internal_id, sender_account_id, landed_spam, received_at`

func scanArrival(row pgx.Row) (*PoolLinkArrival, error) {
	var a PoolLinkArrival
	err := row.Scan(&a.EmailAccountID, &a.MessageHandle, &a.MessageID, &a.InternalID, &a.SenderAccountID, &a.LandedSpam, &a.ReceivedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &a, nil
}

func (r *poolLinkRepository) GetArrival(ctx context.Context, accountID uuid.UUID, handle string) (*PoolLinkArrival, error) {
	return scanArrival(r.db.QueryRow(ctx, `SELECT `+arrivalColumns+` FROM pool_link_arrivals WHERE email_account_id = $1 AND message_handle = $2`, accountID, handle))
}

func (r *poolLinkRepository) ArrivalForMessage(ctx context.Context, accountID uuid.UUID, messageID string) (*PoolLinkArrival, error) {
	return scanArrival(r.db.QueryRow(ctx, `SELECT `+arrivalColumns+` FROM pool_link_arrivals
		WHERE email_account_id = $1 AND btrim(message_id, '<>') = btrim($2, '<>') ORDER BY reported_at LIMIT 1`, accountID, messageID))
}

// ArrivalHandle resolves the instance's handle for a received message, by internal id then Message-ID; ” for the mailbox's own sent copy.
func (r *poolLinkRepository) ArrivalHandle(ctx context.Context, accountID uuid.UUID, internalID *uuid.UUID, messageID string) (string, error) {
	var handle string
	err := r.db.QueryRow(ctx, `SELECT message_handle FROM pool_link_arrivals WHERE email_account_id = $1
		AND (($2::uuid IS NOT NULL AND internal_id = $2::uuid) OR ($3::text <> '' AND btrim(message_id, '<>') = btrim($3::text, '<>')))
		ORDER BY ($2::uuid IS NOT NULL AND internal_id = $2::uuid) DESC LIMIT 1`, accountID, internalID, strings.TrimSpace(messageID)).Scan(&handle)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", nil
	}
	return handle, err
}

func (r *poolLinkRepository) ListInstanceExecutedAccounts(ctx context.Context) ([]uuid.UUID, error) {
	rows, err := r.db.Query(ctx, `SELECT email_account_id FROM pool_link_mailboxes WHERE executor = 'instance' ORDER BY email_account_id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []uuid.UUID
	for rows.Next() {
		var id uuid.UUID
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}

// SampleReporterTrust compares a mailbox's first-landing spam reports with
// the rate Cloud-executed recipients of the same provider family saw from the
// same senders, weighted by how much each sender sent to this mailbox.
func (r *poolLinkRepository) SampleReporterTrust(ctx context.Context, accountID uuid.UUID, since time.Time) (ReporterTrustSample, error) {
	var s ReporterTrustSample
	err := r.db.QueryRow(ctx, `WITH target AS (
		SELECT ea.id, `+recipientGroupSQL("ea")+` AS grp FROM email_accounts ea WHERE ea.id = $1
	), mine AS (
		SELECT wr.sender_account_id AS sender, COUNT(*) AS n,
		       COUNT(*) FILTER (WHERE wr.landed_spam OR wr.first_landing = 'spam') AS spam
		FROM warmup_received wr
		WHERE wr.email_account_id = $1 AND wr.created_at >= $2 AND wr.message_id <> ''
		GROUP BY 1
	), theirs AS (
		SELECT wr.sender_account_id AS sender, COUNT(*) AS n,
		       COUNT(*) FILTER (WHERE wr.landed_spam OR wr.first_landing = 'spam') AS spam
		FROM warmup_received wr
		JOIN email_accounts rec ON rec.id = wr.email_account_id
		CROSS JOIN target t
		WHERE wr.sender_account_id IN (SELECT sender FROM mine)
		  AND wr.created_at >= $2 AND wr.message_id <> '' AND rec.id <> $1
		  AND (wr.first_landing IN ('inbox', 'tabs', 'spam') OR wr.landed_spam)
		  AND `+recipientGroupSQL("rec")+` = t.grp
		  AND NOT `+instanceExecutedSQL("rec.id")+`
		GROUP BY 1
	)
	SELECT COALESCE(SUM(m.n), 0)::int, COALESCE(SUM(m.spam), 0)::int, COALESCE(SUM(th.n), 0)::int,
	       COALESCE(SUM(m.n * th.spam::float8 / th.n), 0)::float8
	FROM mine m JOIN theirs th ON th.sender = m.sender`, accountID, since).Scan(&s.Reported, &s.ReportedSpam, &s.Observed, &s.ExpectedSpam)
	return s, err
}

// SaveReporterTrust records a verdict and returns the state it replaced (” when new).
func (r *poolLinkRepository) SaveReporterTrust(ctx context.Context, accountID uuid.UUID, s ReporterTrustSample, state string) (string, error) {
	var reportedRate, expectedRate, divergence *float64
	if s.Reported > 0 {
		rr, er, d := s.Rates()
		reportedRate, expectedRate, divergence = &rr, &er, &d
	}
	var previous string
	err := r.db.QueryRow(ctx, `WITH prev AS (SELECT trust_state FROM pool_link_reporter_trust WHERE email_account_id = $1)
		INSERT INTO pool_link_reporter_trust (email_account_id, organization_id, trust_state, reported_samples, observed_samples, reported_rate, expected_rate, divergence, evaluated_at, changed_at)
		SELECT ea.id, ea.organization_id, $2, $3, $4, $5, $6, $7, NOW(), NOW() FROM email_accounts ea WHERE ea.id = $1 AND ea.organization_id IS NOT NULL
		ON CONFLICT (email_account_id) DO UPDATE SET trust_state = EXCLUDED.trust_state, reported_samples = EXCLUDED.reported_samples,
		    observed_samples = EXCLUDED.observed_samples, reported_rate = EXCLUDED.reported_rate, expected_rate = EXCLUDED.expected_rate,
		    divergence = EXCLUDED.divergence, evaluated_at = NOW(),
		    changed_at = CASE WHEN pool_link_reporter_trust.trust_state = EXCLUDED.trust_state THEN pool_link_reporter_trust.changed_at ELSE NOW() END
		RETURNING COALESCE((SELECT trust_state FROM prev), '')`, accountID, state, s.Reported, s.Observed, reportedRate, expectedRate, divergence).Scan(&previous)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", nil
	}
	return previous, err
}

// Rates is the reported spam rate, the rate Cloud expected, and their absolute difference.
func (s ReporterTrustSample) Rates() (reported, expected, divergence float64) {
	if s.Reported <= 0 {
		return 0, 0, 0
	}
	reported = float64(s.ReportedSpam) / float64(s.Reported)
	expected = s.ExpectedSpam / float64(s.Reported)
	divergence = reported - expected
	if divergence < 0 {
		divergence = -divergence
	}
	return reported, expected, divergence
}

// ListSelfExecutionMailboxes is every instance-executed mailbox of a workspace with its reporter standing.
func (r *poolLinkRepository) ListSelfExecutionMailboxes(ctx context.Context, orgID uuid.UUID) ([]models.PoolLinkSelfExecutionMailbox, error) {
	rows, err := r.db.Query(ctx, `SELECT ea.id, ea.email, COALESCE(t.trust_state, '`+models.PoolLinkReportProbation+`'), COALESCE(t.reported_samples, 0),
		t.divergence, t.evaluated_at, i.executor_seen_at
		FROM pool_link_mailboxes m
		JOIN pool_link_instances i ON i.id = m.instance_id
		JOIN email_accounts ea ON ea.id = m.email_account_id AND ea.organization_id = i.organization_id
		LEFT JOIN pool_link_reporter_trust t ON t.email_account_id = ea.id
		WHERE i.organization_id = $1 AND m.executor = 'instance'
		ORDER BY ea.email`, orgID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []models.PoolLinkSelfExecutionMailbox{}
	for rows.Next() {
		var m models.PoolLinkSelfExecutionMailbox
		if err := rows.Scan(&m.EmailAccountID, &m.Email, &m.TrustState, &m.ReportedSamples, &m.Divergence, &m.EvaluatedAt, &m.LastLeasedAt); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// InstanceExecuted reports whether a linked instance, not a worker, runs a mailbox.
func (r *emailRepository) InstanceExecuted(ctx context.Context, id uuid.UUID) (bool, error) {
	var yes bool
	err := r.DB.QueryRow(ctx, `SELECT `+instanceExecutedSQL("$1::uuid"), id).Scan(&yes)
	return yes, err
}

// RestoreMailboxCredential writes the credential of a mailbox returning to Cloud execution, recreating the row its handover deleted.
func (r *emailRepository) RestoreMailboxCredential(ctx context.Context, id uuid.UUID, oauth *models.PoolLinkOAuthCredential, creds *models.SmtpImap) error {
	orgID, err := r.mailboxOrganization(ctx, id)
	if err != nil {
		return err
	}
	switch {
	case oauth != nil && oauth.RefreshToken != "":
		sealed, err := r.sealCredentials(ctx, orgID, oauth.AccessToken, oauth.RefreshToken)
		if err != nil {
			return err
		}
		_, err = r.DB.Exec(ctx, `INSERT INTO email_accounts_oauth (email_account_id, access_token, refresh_token, expires_at) VALUES ($1, $2, $3, $4)
			ON CONFLICT (email_account_id) DO UPDATE SET access_token = EXCLUDED.access_token, refresh_token = EXCLUDED.refresh_token, expires_at = EXCLUDED.expires_at, granted_scopes = NULL`,
			id, sealed[0], sealed[1], oauth.ExpiresAt)
		return err
	case creds != nil && creds.SMTP != nil && creds.IMAP != nil:
		sealed, err := r.sealCredentials(ctx, orgID, creds.SMTP.Host, creds.SMTP.Username, creds.SMTP.Password, creds.IMAP.Host, creds.IMAP.Username, creds.IMAP.Password)
		if err != nil {
			return err
		}
		_, err = r.DB.Exec(ctx, `INSERT INTO email_accounts_smtp_imap (email_account_id, smtp_host, smtp_port, smtp_user, smtp_password, smtp_security,
			imap_host, imap_port, imap_user, imap_password, imap_security, smtp_tls_unverified, smtp_tls_pin, imap_tls_unverified, imap_tls_pin)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15)
			ON CONFLICT (email_account_id) DO UPDATE SET smtp_host = EXCLUDED.smtp_host, smtp_port = EXCLUDED.smtp_port, smtp_user = EXCLUDED.smtp_user,
			  smtp_password = EXCLUDED.smtp_password, smtp_security = EXCLUDED.smtp_security, imap_host = EXCLUDED.imap_host, imap_port = EXCLUDED.imap_port,
			  imap_user = EXCLUDED.imap_user, imap_password = EXCLUDED.imap_password, imap_security = EXCLUDED.imap_security,
			  smtp_tls_unverified = EXCLUDED.smtp_tls_unverified, smtp_tls_pin = EXCLUDED.smtp_tls_pin,
			  imap_tls_unverified = EXCLUDED.imap_tls_unverified, imap_tls_pin = EXCLUDED.imap_tls_pin`,
			id, sealed[0], creds.SMTP.Port, sealed[1], sealed[2], models.ResolveSMTPSecurity(creds.SMTP.Security, creds.SMTP.Port),
			sealed[3], creds.IMAP.Port, sealed[4], sealed[5], models.ResolveIMAPSecurity(creds.IMAP.Security, creds.IMAP.Port),
			creds.SMTP.TLSUnverified, creds.SMTP.StoredTLSPin(), creds.IMAP.TLSUnverified, creds.IMAP.StoredTLSPin())
		return err
	}
	return errx.ErrInvalid
}

// ReporterTrustState is a mailbox's recorded reporter standing, or "" before its first audit.
func (r *poolLinkRepository) ReporterTrustState(ctx context.Context, accountID uuid.UUID) (string, error) {
	var state string
	err := r.db.QueryRow(ctx, `SELECT trust_state FROM pool_link_reporter_trust WHERE email_account_id = $1`, accountID).Scan(&state)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", nil
	}
	return state, err
}
