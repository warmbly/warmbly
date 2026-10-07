package repository

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/models"
)

type WorkerWarmupDispatch interface {
	WarmupDispatch(context.Context, uuid.UUID, uuid.UUID, uuid.UUID, uuid.UUID, bool, *models.SendEmailResult) (*WarmupDispatchState, error)
}

func (r *httpSyncContextRepository) WarmupDispatch(ctx context.Context, task, mailbox, worker, nonce uuid.UUID, start bool, result *models.SendEmailResult) (*WarmupDispatchState, error) {
	raw, err := json.Marshal(struct {
		TaskID    uuid.UUID               `json:"task_id"`
		MailboxID uuid.UUID               `json:"mailbox_id"`
		WorkerID  uuid.UUID               `json:"worker_id"`
		Nonce     uuid.UUID               `json:"nonce"`
		Start     bool                    `json:"start"`
		Result    *models.SendEmailResult `json:"result,omitempty"`
	}{task, mailbox, worker, nonce, start, result})
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, r.baseURL+"/api/v1/internal/worker/warmup-dispatch", bytes.NewReader(raw))
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
	if resp.StatusCode == http.StatusNotFound && !start && result == nil {
		return &WarmupDispatchState{State: "legacy"}, nil
	}
	if result != nil && resp.StatusCode == http.StatusNoContent {
		return nil, nil
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("warmup dispatch authority unavailable: status %d", resp.StatusCode)
	}
	out := &WarmupDispatchState{}
	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		return nil, err
	}
	return out, nil
}
