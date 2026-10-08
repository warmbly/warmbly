package repository

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/warmbly/warmbly/internal/models"
)

type DiagnosticAuthAuthority interface {
	DiagnosticAuth(context.Context, models.DiagnosticAuthRequest) (*models.DiagnosticAuthGrant, error)
}

func (r *taskRepository) DiagnosticAuth(ctx context.Context, req models.DiagnosticAuthRequest) (*models.DiagnosticAuthGrant, error) {
	if req.Token == uuid.Nil || req.MailboxID == uuid.Nil || req.WorkerID == uuid.Nil || len(req.MessageID) > 998 || strings.ContainsAny(req.MessageID, "\r\n") {
		return nil, ErrSendAdmissionDenied
	}
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)
	grant := &models.DiagnosticAuthGrant{}
	var task uuid.UUID
	err = tx.QueryRow(ctx, `SELECT wt.task_id,sender.email,ea.email,$4::text
 FROM warmup_tokens wt JOIN warmup_tasks w ON w.task_id=wt.task_id
 JOIN email_accounts sender ON sender.id=wt.sender_account_id
 JOIN email_accounts ea ON ea.id=wt.recipient_account_id
 JOIN warmup_pool_participants wpp ON wpp.email_account_id=ea.id
 JOIN warmup_pools wp ON wp.id=wpp.pool_id JOIN fleet_nodes n ON n.id=ea.worker_id
 WHERE wt.token=$1 AND ea.id=$2 AND ea.worker_id=$3 AND n.role='worker' AND n.active AND n.warmup_send_protocol>=2 AND n.last_seen_at>NOW()-INTERVAL '10 minutes'
 AND w.lineage_version=1 AND w.scenario_version='diagnostic-v1' AND w.rendering_version='canonical-v1'
 AND wt.expires_at>NOW() AND wt.sent_retired_at IS NULL AND wt.sent_message_id<>''
 AND EXISTS(SELECT 1 FROM tasks sent WHERE sent.id=wt.task_id AND sent.status='completed' AND sent.email_account_id=wt.sender_account_id)
 AND (btrim(wt.sent_message_id,'<>')=$4 OR EXISTS(SELECT 1 FROM warmup_received wr
      WHERE wr.task_id=wt.task_id AND wr.email_account_id=ea.id AND wr.sender_account_id=wt.sender_account_id
      AND wr.retired_at IS NULL AND btrim(wr.message_id,'<>')=$4))
 AND sender.status='active' AND (sender.test_mode IS NULL OR sender.test_mode='legacy' OR sender.test_mode='diagnostic' AND sender.test_send_enabled)
 AND NOT EXISTS(SELECT 1 FROM cloud_link_mailboxes clm WHERE clm.email_account_id=ea.id)
 AND `+partnerEligibleSQL, req.Token, req.MailboxID, req.WorkerID, strings.Trim(req.MessageID, "<> \t")).Scan(&task, &grant.From, &grant.To, &grant.MessageID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrSendAdmissionDenied
	}
	if err != nil {
		return nil, err
	}
	if req.Result == nil {
		grant.Nonce = uuid.New()
		err = tx.QueryRow(ctx, `INSERT INTO diagnostic_auth_verifications(task_id,email_account_id,worker_id,nonce,message_id)VALUES($1,$2,$3,$4,$5)
 ON CONFLICT(task_id,email_account_id) DO UPDATE SET worker_id=EXCLUDED.worker_id,nonce=EXCLUDED.nonce,message_id=EXCLUDED.message_id,authorized_at=NOW(),result=NULL,verified_at=NULL,attempts=diagnostic_auth_verifications.attempts+1
 WHERE diagnostic_auth_verifications.attempts<3 AND diagnostic_auth_verifications.authorized_at<NOW()-INTERVAL '30 seconds'
 AND (diagnostic_auth_verifications.result IS NULL OR diagnostic_auth_verifications.result->>'dkim'='unknown') RETURNING nonce`, task, req.MailboxID, req.WorkerID, grant.Nonce, grant.MessageID).Scan(&grant.Nonce)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		if err != nil {
			return nil, err
		}
		return grant, tx.Commit(ctx)
	}
	if req.Nonce == uuid.Nil || !req.Result.Valid(time.Now()) {
		return nil, ErrSendAdmissionDenied
	}
	result := *req.Result
	result.Alignment = "unknown"
	if result.DKIM == "pass" {
		if at := strings.LastIndex(grant.From, "@"); at >= 0 && strings.EqualFold(grant.From[at+1:], result.SigningDomain) {
			result.Alignment = "pass"
		}
	}
	raw, err := json.Marshal(result)
	if err != nil {
		return nil, err
	}
	var prior []byte
	err = tx.QueryRow(ctx, `SELECT result FROM diagnostic_auth_verifications WHERE task_id=$1 AND email_account_id=$2 AND worker_id=$3 AND nonce=$4 AND message_id=$5 AND authorized_at>NOW()-INTERVAL '5 minutes' FOR UPDATE`, task, req.MailboxID, req.WorkerID, req.Nonce, grant.MessageID).Scan(&prior)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrSendAdmissionDenied
	}
	if err != nil {
		return nil, err
	}
	if len(prior) > 0 {
		var existing models.DiagnosticDKIMResult
		if json.Unmarshal(prior, &existing) != nil || existing.DKIM != result.DKIM || existing.SigningDomain != result.SigningDomain || existing.Alignment != result.Alignment {
			return nil, ErrSendAdmissionDenied
		}
		return nil, nil
	}
	if _, err = tx.Exec(ctx, `UPDATE diagnostic_auth_verifications SET result=$3,verified_at=NOW() WHERE task_id=$1 AND email_account_id=$2`, task, req.MailboxID, raw); err != nil {
		return nil, err
	}
	if err = attachDiagnosticAuth(ctx, tx, task, req.MailboxID); err != nil {
		return nil, err
	}
	return nil, tx.Commit(ctx)
}

func attachDiagnosticAuth(ctx context.Context, q sendResultDB, task, recipient uuid.UUID) error {
	_, err := q.Exec(ctx, `UPDATE warmup_received wr SET evidence=jsonb_set(jsonb_set(jsonb_set(jsonb_set(COALESCE(wr.evidence,'{}'::jsonb),'{dkim}',d.result->'dkim'),'{alignment}',d.result->'alignment'),'{trust}','"worker_cryptographic"'::jsonb),'{dkim_verification}',d.result)
 FROM diagnostic_auth_verifications d WHERE wr.task_id=$1 AND wr.email_account_id=$2
 AND d.task_id=wr.task_id AND d.email_account_id=wr.email_account_id AND d.result IS NOT NULL AND btrim(d.message_id,'<>')=btrim(wr.message_id,'<>')`, task, recipient)
	return err
}

func (r *httpSyncContextRepository) DiagnosticAuth(ctx context.Context, in models.DiagnosticAuthRequest) (*models.DiagnosticAuthGrant, error) {
	raw, err := json.Marshal(in)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, r.baseURL+"/api/v1/internal/worker/diagnostic-auth", bytes.NewReader(raw))
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
		return nil, errors.New("diagnostic verification authority unavailable")
	}
	var out *models.DiagnosticAuthGrant
	if err = json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, err
	}
	return out, nil
}
