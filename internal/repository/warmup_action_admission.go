package repository

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/warmbly/warmbly/internal/models"
)

type WarmupActionAdmission interface {
	PermittedWarmupActions(context.Context, uuid.UUID, uuid.UUID, []string) ([]string, error)
}

type WarmupActionRecoveryAdmission interface {
	AdmitWarmupAction(context.Context, models.WarmupActionRequest) (models.WarmupActionDecision, error)
}

func (r *taskRepository) AdmitWarmupAction(ctx context.Context, req models.WarmupActionRequest) (models.WarmupActionDecision, error) {
	actions, err := r.PermittedWarmupActions(ctx, req.MailboxID, req.WorkerID, req.Actions)
	out := models.WarmupActionDecision{Actions: actions}
	if err != nil || req.FilingID == "" || len(req.Actions) != 1 || req.Actions[0] != models.WarmupActionFile || len(actions) != 1 || actions[0] != models.WarmupActionFile {
		return out, err
	}
	filing, err := uuid.Parse(req.FilingID)
	if err != nil || filing == uuid.Nil {
		return out, errors.New("invalid warmup filing identifier")
	}
	err = r.db.QueryRow(ctx, `SELECT EXISTS (
		SELECT 1 FROM warmup_pending_filings f JOIN email_accounts ea ON ea.id=f.email_account_id
		WHERE f.id=$3 AND f.email_account_id=$1 AND ea.worker_id=$2 AND f.payload->'actions'=to_jsonb($4::text[])
	)`, req.MailboxID, req.WorkerID, filing, []string{models.WarmupActionFile}).Scan(&out.FilingPending)
	return out, err
}

func (r *taskRepository) PermittedWarmupActions(ctx context.Context, mailbox, worker uuid.UUID, actions []string) ([]string, error) {
	if len(actions) > 16 {
		return nil, ErrSendAdmissionDenied
	}
	var a models.Email
	var active bool
	err := r.db.QueryRow(ctx, `SELECT ea.status='active' AND ea.worker_id=$2 AND n.role='worker' AND n.active AND n.last_seen_at>NOW()-INTERVAL '10 minutes' AND n.warmup_send_protocol>=2
 AND o.risk_state IN ('trusted','watch') AND NOT EXISTS(SELECT 1 FROM cloud_link_mailboxes c WHERE c.email_account_id=ea.id),ea.test_mode,ea.test_send_enabled,ea.test_receive_enabled
 FROM email_accounts ea JOIN organizations o ON o.id=ea.organization_id JOIN fleet_nodes n ON n.id=ea.worker_id WHERE ea.id=$1`, mailbox, worker).Scan(&active, &a.TestMode, &a.TestSendEnabled, &a.TestReceiveEnabled)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if !active {
		return nil, nil
	}
	return a.PermittedWarmupActions(actions), nil
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
	req.Header.Set("Authorization", "Bearer "+r.token)
	req.Header.Set("Content-Type", "application/json")
	resp, err := r.client.Do(req)
	if err != nil {
		return out, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return out, errors.New("warmup action authority unavailable")
	}
	err = json.NewDecoder(resp.Body).Decode(&out)
	return out, err
}
