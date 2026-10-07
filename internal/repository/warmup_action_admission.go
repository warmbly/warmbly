package repository

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"

	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/models"
)

type WarmupActionAdmission interface {
	PermittedWarmupActions(context.Context, uuid.UUID, uuid.UUID, []string) ([]string, error)
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
	if err != nil {
		return nil, err
	}
	if !active {
		return nil, nil
	}
	return a.PermittedWarmupActions(actions), nil
}

func (r *httpSyncContextRepository) PermittedWarmupActions(ctx context.Context, mailbox, worker uuid.UUID, actions []string) ([]string, error) {
	raw, err := json.Marshal(struct {
		MailboxID uuid.UUID `json:"mailbox_id"`
		WorkerID  uuid.UUID `json:"worker_id"`
		Actions   []string  `json:"actions"`
	}{mailbox, worker, actions})
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, r.baseURL+"/api/v1/internal/worker/warmup-actions", bytes.NewReader(raw))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+r.token)
	req.Header.Set("Content-Type", "application/json")
	resp, err := r.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, errors.New("warmup action authority unavailable")
	}
	var out struct {
		Actions []string `json:"actions"`
	}
	err = json.NewDecoder(resp.Body).Decode(&out)
	return out.Actions, err
}
