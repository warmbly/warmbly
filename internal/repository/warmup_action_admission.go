package repository

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/pkg/nodeauth"
)

type WarmupActionAdmission interface {
	PermittedWarmupActions(context.Context, uuid.UUID, uuid.UUID, []string) ([]string, error)
}

type WarmupActionRecoveryAdmission interface {
	AdmitWarmupAction(context.Context, models.WarmupActionRequest) (models.WarmupActionDecision, error)
}

func (r *taskRepository) AdmitWarmupAction(ctx context.Context, req models.WarmupActionRequest) (models.WarmupActionDecision, error) {
	actions, err := permittedWarmupActionsFor(ctx, r.db, req.MailboxID, req.WorkerID, req.Actions, req.CommandID)
	out := models.WarmupActionDecision{Actions: actions}
	if err != nil || req.FilingID == "" || len(req.Actions) != 1 || req.Actions[0] != models.WarmupActionFile || len(actions) != 1 || actions[0] != models.WarmupActionFile {
		return out, err
	}
	filing, err := uuid.Parse(req.FilingID)
	if err != nil || filing == uuid.Nil {
		return out, errors.New("invalid warmup filing identifier")
	}
	proof := &models.WarmupFilingRecoveryProof{Protocol: models.WarmupFilingRecoveryProtocol, MailboxID: req.MailboxID, WorkerID: req.WorkerID, FilingID: filing}
	err = r.db.QueryRow(ctx, `SELECT f.provider_retry_at FROM warmup_pending_filings f JOIN email_accounts ea ON ea.id=f.email_account_id
		WHERE f.id=$3 AND f.email_account_id=$1 AND ea.worker_id=$2 AND f.payload->'actions'=to_jsonb($4::text[])`, req.MailboxID, req.WorkerID, filing, []string{models.WarmupActionFile}).Scan(&proof.ProviderRetryAt)
	if errors.Is(err, pgx.ErrNoRows) {
		out.Actions = nil
		return out, nil
	}
	if err == nil {
		out.FilingPending, out.FilingRecovery = true, proof
	}
	return out, err
}

func (r *taskRepository) PermittedWarmupActions(ctx context.Context, mailbox, worker uuid.UUID, actions []string) ([]string, error) {
	return permittedWarmupActions(ctx, r.db, mailbox, worker, actions)
}

type warmupActionQuery interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}

func permittedWarmupActions(ctx context.Context, q warmupActionQuery, mailbox, worker uuid.UUID, actions []string) ([]string, error) {
	return permittedWarmupActionsFor(ctx, q, mailbox, worker, actions, "")
}

// permittedWarmupActionsFor admits an instance-executed mailbox's actions only under the one Cloud command they carry.
func permittedWarmupActionsFor(ctx context.Context, q warmupActionQuery, mailbox, worker uuid.UUID, actions []string, command string) ([]string, error) {
	if len(actions) > 16 {
		return nil, ErrSendAdmissionDenied
	}
	var commandID *uuid.UUID
	if command != "" {
		id, err := uuid.Parse(command)
		if err != nil || id == uuid.Nil {
			return nil, nil
		}
		commandID = &id
	}
	var a models.Email
	var active bool
	var cloud cloudCommandAdmission
	var executor *string
	err := q.QueryRow(ctx, `SELECT ea.status='active' AND ea.worker_id=$2 AND n.role='worker' AND n.active AND n.last_seen_at>NOW()-INTERVAL '10 minutes' AND n.warmup_send_protocol>=2
 AND o.risk_state IN ('trusted','watch'),ea.test_mode,ea.test_send_enabled,ea.test_receive_enabled,c.executor,
 k.admit_until,COALESCE(k.actions,'{}')
 FROM email_accounts ea JOIN organizations o ON o.id=ea.organization_id JOIN fleet_nodes n ON n.id=ea.worker_id
 LEFT JOIN cloud_link_mailboxes c ON c.email_account_id=ea.id
 LEFT JOIN cloud_link_commands k ON k.id=$3 AND k.email_account_id=ea.id AND k.instance_id=c.instance_id AND k.kind='action' AND k.state='dispatched'
 WHERE ea.id=$1`, mailbox, worker, commandID).Scan(&active, &a.TestMode, &a.TestSendEnabled, &a.TestReceiveEnabled, &executor, &cloud.AdmitUntil, &cloud.Actions)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if !active {
		return nil, nil
	}
	if executor != nil {
		cloud.Linked, cloud.Executor = true, models.PoolLinkExecutor(*executor)
	}
	out := a.PermittedWarmupActions(cloudActionsAdmitted(cloud, actions, time.Now()))
	if cloud.Linked && commandID != nil && len(out) > 0 {
		var stamped uuid.UUID
		if err := q.QueryRow(ctx, `UPDATE cloud_link_commands SET admitted_at=COALESCE(admitted_at,NOW()) WHERE id=$1 RETURNING id`, *commandID).Scan(&stamped); err != nil {
			return nil, err
		}
	}
	return out, nil
}

func (r *httpSyncContextRepository) PermittedWarmupActions(ctx context.Context, mailbox, worker uuid.UUID, actions []string) ([]string, error) {
	out, err := r.AdmitWarmupAction(ctx, models.WarmupActionRequest{MailboxID: mailbox, WorkerID: worker, Actions: actions})
	return out.Actions, err
}

func (r *httpSyncContextRepository) AdmitWarmupAction(ctx context.Context, request models.WarmupActionRequest) (models.WarmupActionDecision, error) {
	var out models.WarmupActionDecision
	raw, err := json.Marshal(request)
	if err != nil {
		return out, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, r.baseURL+"/api/v1/internal/worker/warmup-actions", bytes.NewReader(raw))
	if err != nil {
		return out, err
	}
	nodeauth.Authorize(req, r.token)
	req.Header.Set("Content-Type", "application/json")
	resp, err := r.client.Do(req)
	if err != nil {
		return out, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return out, errors.New("warmup action authority unavailable")
	}
	var response struct {
		Actions        json.RawMessage                   `json:"actions"`
		FilingPending  bool                              `json:"filing_pending"`
		FilingRecovery *models.WarmupFilingRecoveryProof `json:"filing_recovery"`
	}
	decoder := json.NewDecoder(resp.Body)
	if err := decoder.Decode(&response); err != nil {
		return out, err
	}
	var trailing any
	if len(response.Actions) == 0 || decoder.Decode(&trailing) != io.EOF {
		return out, errors.New("invalid warmup action authority response")
	}
	if err := json.Unmarshal(response.Actions, &out.Actions); err != nil {
		return out, err
	}
	out.FilingPending = response.FilingPending
	if response.FilingRecovery != nil {
		filing, err := uuid.Parse(request.FilingID)
		if err != nil || !response.FilingPending || len(out.Actions) != 1 || out.Actions[0] != models.WarmupActionFile ||
			!response.FilingRecovery.ValidFor(request.MailboxID, request.WorkerID, filing) {
			return models.WarmupActionDecision{}, errors.New("invalid warmup filing recovery proof")
		}
		out.FilingRecovery = response.FilingRecovery
	}
	return out, nil
}
