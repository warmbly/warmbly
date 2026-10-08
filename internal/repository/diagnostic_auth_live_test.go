package repository

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/models"
)

func TestLiveDiagnosticAuthBindsWorkerContextAndRetainsOnlyMinimalProof(t *testing.T) {
	f, r := lineageFixture(t)
	ctx := t.Context()
	worker := uuid.New()
	token := uuid.New()
	exec := func(query string, args ...any) {
		t.Helper()
		if _, err := f.pool.Exec(ctx, query, args...); err != nil {
			t.Fatal(err)
		}
	}
	exec(`INSERT INTO fleet_nodes(id,role,active,last_seen_at,warmup_send_protocol)VALUES($1,'worker',true,NOW(),2)`, worker)
	exec(`INSERT INTO workers(id)VALUES($1)`, worker)
	t.Cleanup(func() { _, _ = f.pool.Exec(context.Background(), `DELETE FROM fleet_nodes WHERE id=$1`, worker) })
	exec(`UPDATE email_accounts SET worker_id=$1,provider=$3 WHERE id=$2`, worker, f.recipient, models.InboxProviderGoogle)
	exec(`UPDATE email_accounts SET send_as_email='unused-alias@example.test' WHERE id=$1`, f.sender)
	exec(`INSERT INTO warmup_pool_participants(pool_id,email_account_id)SELECT id,$1 FROM warmup_pools WHERE pool_type='free'`, f.recipient)
	exec(`INSERT INTO warmup_tasks(task_id,lineage_version,subject,scenario_version,rendering_version,max_turns)VALUES($1,1,'Diagnostic','diagnostic-v1','canonical-v1',1)`, f.task)
	exec(`INSERT INTO warmup_tokens(token,task_id,sender_account_id,recipient_account_id,sent_message_id)VALUES($1,$2,$3,$4,'')`, token, f.task, f.sender, f.recipient)
	request := models.DiagnosticAuthRequest{Token: token, MailboxID: f.recipient, WorkerID: worker, MessageID: "probe@example.test"}
	if grant, err := r.DiagnosticAuth(ctx, request); err == nil || grant != nil {
		t.Fatal("receipt-before-result authorized raw retrieval", grant, err)
	}
	exec(`UPDATE warmup_tokens SET sent_message_id='<probe@example.test>' WHERE token=$1`, token)
	for _, bad := range []models.DiagnosticAuthRequest{
		{Token: token, MailboxID: f.recipient, WorkerID: uuid.New(), MessageID: request.MessageID},
		{Token: token, MailboxID: f.sender, WorkerID: worker, MessageID: request.MessageID},
		{Token: uuid.New(), MailboxID: f.recipient, WorkerID: worker, MessageID: request.MessageID},
		{Token: token, MailboxID: f.recipient, WorkerID: worker, MessageID: "unrelated@example.test"},
	} {
		if grant, err := r.DiagnosticAuth(ctx, bad); err == nil || grant != nil {
			t.Fatal("unrelated context authorized", bad, grant)
		}
	}
	grant, err := r.DiagnosticAuth(ctx, request)
	if err != nil || grant == nil || grant.Nonce == uuid.Nil {
		t.Fatalf("grant=%+v %v", grant, err)
	}
	if grant.From != "pl-"+f.sender.String()[:8]+"@test.local" {
		t.Fatal("diagnostic authorized unused cold-mail alias", grant.From)
	}
	request.Nonce = grant.Nonce
	request.Result = &models.DiagnosticDKIMResult{DKIM: "pass", Alignment: "pass", SigningDomain: "test.local", Verifier: models.DiagnosticDKIMVerifier, ObservedAt: time.Now()}
	exec(`UPDATE email_accounts SET test_mode='off' WHERE id=$1`, f.recipient)
	if _, err = r.DiagnosticAuth(ctx, request); err == nil {
		t.Fatal("revocation did not fence proof completion")
	}
	exec(`UPDATE email_accounts SET test_mode='diagnostic',test_receive_enabled=true WHERE id=$1`, f.recipient)
	wrong := request
	wrong.Nonce = uuid.New()
	if _, err = r.DiagnosticAuth(ctx, wrong); err == nil {
		t.Fatal("wrong grant nonce accepted")
	}
	if _, err = r.DiagnosticAuth(ctx, request); err != nil {
		t.Fatal(err)
	}
	if _, err = r.DiagnosticAuth(ctx, request); err != nil {
		t.Fatal("duplicate proof rejected", err)
	}
	var stored string
	var rows int
	if err = f.pool.QueryRow(ctx, `SELECT result::text FROM diagnostic_auth_verifications WHERE task_id=$1`, f.task).Scan(&stored); err != nil {
		t.Fatal(err)
	}
	if err = f.pool.QueryRow(ctx, `SELECT COUNT(*) FROM diagnostic_auth_verifications WHERE task_id=$1`, f.task).Scan(&rows); err != nil {
		t.Fatal(err)
	}
	if rows != 1 || strings.Contains(stored, "From:") || strings.Contains(stored, "Body") || !strings.Contains(stored, models.DiagnosticDKIMVerifier) {
		t.Fatal("nonminimal or duplicate proof", stored)
	}
	request.Result.DKIM = "fail"
	request.Result.Alignment = "unknown"
	request.Result.SigningDomain = ""
	if _, err = r.DiagnosticAuth(ctx, request); err == nil {
		t.Fatal("late conflicting proof overwrote immutable first result")
	}
	request.Result = nil
	if next, err := r.DiagnosticAuth(ctx, request); err != nil || next != nil {
		t.Fatal("completed diagnostic fetched again", next, err)
	}
	receipt := uuid.New()
	exec(`INSERT INTO warmup_received(email_account_id,internal_id,sender_account_id,message_id,evidence)VALUES($1,$2,$3,'probe@example.test','{"spf":"unknown","dkim":"unknown","dmarc":"unknown","tls":"unknown","alignment":"unknown","trust":"unverified_headers"}')`, f.recipient, receipt, f.sender)
	if err = r.RecordVerifiedWarmupParent(ctx, f.task, f.recipient, receipt, "", WarmupReceiptProof{SenderAddress: f.senderTo}); err != nil {
		t.Fatal(err)
	}
	var evidence models.ReceivedEvidence
	var data []byte
	if err = f.pool.QueryRow(ctx, `SELECT evidence FROM warmup_received WHERE email_account_id=$1 AND internal_id=$2`, f.recipient, receipt).Scan(&data); err != nil {
		t.Fatal(err)
	}
	if err = json.Unmarshal(data, &evidence); err != nil {
		t.Fatal(err)
	}
	if evidence.DKIM != "pass" || evidence.Alignment != "pass" || evidence.DKIMVerification == nil || evidence.SPF != "unknown" || evidence.DMARC != "unknown" || evidence.TLS != "unknown" {
		t.Fatal("late exact receipt lost crypto proof or fabricated other auth", evidence)
	}
}

func TestLiveDiagnosticAuthRelayRewriteUsesOnlyBoundReceivedID(t *testing.T) {
	f, r := lineageFixture(t)
	ctx := t.Context()
	worker, token, receipt := uuid.New(), uuid.New(), uuid.New()
	exec := func(query string, args ...any) {
		t.Helper()
		if _, err := f.pool.Exec(ctx, query, args...); err != nil {
			t.Fatal(err)
		}
	}
	exec(`INSERT INTO fleet_nodes(id,role,active,last_seen_at,warmup_send_protocol)VALUES($1,'worker',true,NOW(),2)`, worker)
	exec(`INSERT INTO workers(id)VALUES($1)`, worker)
	t.Cleanup(func() { _, _ = f.pool.Exec(context.Background(), `DELETE FROM fleet_nodes WHERE id=$1`, worker) })
	exec(`UPDATE email_accounts SET worker_id=$1,provider=$3 WHERE id=$2`, worker, f.recipient, models.InboxProviderGoogle)
	exec(`INSERT INTO warmup_pool_participants(pool_id,email_account_id)SELECT id,$1 FROM warmup_pools WHERE pool_type='free'`, f.recipient)
	exec(`INSERT INTO warmup_tasks(task_id,lineage_version,subject,scenario_version,rendering_version,max_turns)VALUES($1,1,'Diagnostic','diagnostic-v1','canonical-v1',1)`, f.task)
	exec(`INSERT INTO warmup_tokens(token,task_id,sender_account_id,recipient_account_id,sent_message_id)VALUES($1,$2,$3,$4,'<sent@example.test>')`, token, f.task, f.sender, f.recipient)
	exec(`INSERT INTO warmup_received(email_account_id,internal_id,sender_account_id,message_id)VALUES($1,$2,$3,'received@mailjet.com')`, f.recipient, receipt, f.sender)
	request := models.DiagnosticAuthRequest{Token: token, MailboxID: f.recipient, WorkerID: worker, MessageID: "<received@mailjet.com>"}
	if grant, err := r.DiagnosticAuth(ctx, request); err == nil || grant != nil {
		t.Fatal("unbound rewritten ID authorized raw retrieval", grant, err)
	}
	if err := r.RecordVerifiedWarmupParent(ctx, f.task, f.recipient, receipt, "", WarmupReceiptProof{Token: token, SenderAddress: f.senderTo}); err != nil {
		t.Fatal(err)
	}
	grant, err := r.DiagnosticAuth(ctx, request)
	if err != nil || grant == nil || grant.MessageID != "received@mailjet.com" {
		t.Fatalf("rewritten grant=%+v err=%v", grant, err)
	}
	request.Nonce = grant.Nonce
	request.Result = &models.DiagnosticDKIMResult{DKIM: "pass", SigningDomain: "test.local", Alignment: "pass", Verifier: models.DiagnosticDKIMVerifier, ObservedAt: time.Now()}
	if _, err := r.DiagnosticAuth(ctx, request); err != nil {
		t.Fatal(err)
	}
	var dkim string
	if err := f.pool.QueryRow(ctx, `SELECT evidence->>'dkim' FROM warmup_received WHERE email_account_id=$1 AND internal_id=$2`, f.recipient, receipt).Scan(&dkim); err != nil || dkim != "pass" {
		t.Fatalf("received-ID proof not attached: %q %v", dkim, err)
	}
	request.Result, request.MessageID = nil, "unrelated@smtp-pulse.com"
	if grant, err := r.DiagnosticAuth(ctx, request); err == nil || grant != nil {
		t.Fatal("bound rewrite authorized a different ID", grant, err)
	}
}
