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
)

type WarmupFilingDeferralAuthority interface {
	DeferWarmupFiling(context.Context, models.WarmupFilingDeferralRequest) (models.WarmupFilingDeferralDecision, error)
}

func filingRetryAt(at time.Time) time.Time {
	utc := at.UTC()
	rounded := utc.Truncate(time.Microsecond)
	if rounded.Before(utc) {
		rounded = rounded.Add(time.Microsecond)
	}
	return rounded
}

func (r *taskRepository) DeferWarmupFiling(ctx context.Context, req models.WarmupFilingDeferralRequest) (models.WarmupFilingDeferralDecision, error) {
	var out models.WarmupFilingDeferralDecision
	if req.MailboxID == uuid.Nil || req.WorkerID == uuid.Nil || req.FilingID == uuid.Nil || req.ProviderRetryAt.IsZero() {
		return out, ErrSendAdmissionDenied
	}
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return out, err
	}
	defer tx.Rollback(ctx)
	var filing uuid.UUID
	err = tx.QueryRow(ctx, `SELECT f.id FROM warmup_pending_filings f JOIN email_accounts ea ON ea.id=f.email_account_id
		WHERE f.id=$3 AND f.email_account_id=$1 AND ea.worker_id=$2 AND f.payload->'actions'=to_jsonb($4::text[])
		FOR UPDATE OF f, ea`, req.MailboxID, req.WorkerID, req.FilingID, []string{models.WarmupActionFile}).Scan(&filing)
	if errors.Is(err, pgx.ErrNoRows) {
		return out, ErrSendAdmissionDenied
	}
	if err != nil {
		return out, err
	}
	actions, err := permittedWarmupActions(ctx, tx, req.MailboxID, req.WorkerID, []string{models.WarmupActionFile})
	if err != nil {
		return out, err
	}
	if len(actions) != 1 || actions[0] != models.WarmupActionFile {
		return out, ErrSendAdmissionDenied
	}
	var at time.Time
	err = tx.QueryRow(ctx, `UPDATE warmup_pending_filings SET provider_retry_at=GREATEST(provider_retry_at,$2),next_attempt_at=GREATEST(next_attempt_at,$2)
		WHERE id=$1 RETURNING provider_retry_at`, filing, filingRetryAt(req.ProviderRetryAt)).Scan(&at)
	if err != nil {
		return out, err
	}
	if err := tx.Commit(ctx); err != nil {
		return out, err
	}
	out.FilingRecovery = &models.WarmupFilingRecoveryProof{Protocol: models.WarmupFilingRecoveryProtocol, MailboxID: req.MailboxID, WorkerID: req.WorkerID, FilingID: req.FilingID, ProviderRetryAt: &at}
	out.Persisted = true
	return out, nil
}

func (r *httpSyncContextRepository) DeferWarmupFiling(ctx context.Context, request models.WarmupFilingDeferralRequest) (models.WarmupFilingDeferralDecision, error) {
	var out models.WarmupFilingDeferralDecision
	request.ProviderRetryAt = filingRetryAt(request.ProviderRetryAt)
	raw, err := json.Marshal(request)
	if err != nil {
		return out, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, r.baseURL+"/api/v1/internal/worker/warmup-actions/defer", bytes.NewReader(raw))
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
		return out, errors.New("warmup filing deferral unavailable")
	}
	decoder := json.NewDecoder(resp.Body)
	if err := decoder.Decode(&out); err != nil {
		return models.WarmupFilingDeferralDecision{}, err
	}
	var trailing any
	if decoder.Decode(&trailing) != io.EOF || !out.Persisted || !out.FilingRecovery.ValidFor(request.MailboxID, request.WorkerID, request.FilingID) ||
		out.FilingRecovery.ProviderRetryAt == nil || out.FilingRecovery.ProviderRetryAt.Before(request.ProviderRetryAt) {
		return models.WarmupFilingDeferralDecision{}, errors.New("unconfirmed warmup filing deferral")
	}
	return out, nil
}
