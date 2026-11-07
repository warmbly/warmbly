package handler

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/app/orgtransfer"
	warmupapp "github.com/warmbly/warmbly/internal/app/warmup"
	"github.com/warmbly/warmbly/internal/infrastructure/db"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/pkg/encrypt"
	"github.com/warmbly/warmbly/internal/pkg/generation"
	"github.com/warmbly/warmbly/internal/repository"
	"github.com/warmbly/warmbly/internal/scheduler"
	"github.com/warmbly/warmbly/internal/tasks"
)

type outboundStartFixture struct {
	db                                 *db.DB
	user, org, sender, recipient, task uuid.UUID
	worker, nonce                      uuid.UUID
	repo                               repository.TaskRepository
	admission                          repository.OutboundAdmissionRepository
	dispatch                           repository.WarmupDispatchRepository
	svc                                tasks.TasksService
	source                             tasks.Conversation
}

func newOutboundStartFixture(t *testing.T) *outboundStartFixture {
	t.Helper()
	dsn := os.Getenv("WARMBLY_TEST_DB")
	if dsn == "" {
		t.Skip("WARMBLY_TEST_DB not set")
	}
	handle, err := db.New(t.Context(), dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(handle.Pool.Close)
	f := &outboundStartFixture{db: handle, user: uuid.New(), org: uuid.New(), sender: uuid.New(), recipient: uuid.New(), task: uuid.New(), worker: uuid.New(), source: tasks.Conversation{ID: uuid.New(), Version: generation.WarmupScenarioVersion, Subject: "A wording choice", Description: "I'm rewording the labels on our intake form and can't pick between two options. Which one reads clearer to you?", Messages: []string{"The shorter one, I think. It scans faster.", "Agreed, I'll go with that. Thanks for the quick read."}}}
	contentRepo := repository.NewWarmupContentRepository(handle.Pool)
	rendering := generation.CanonicalRenderingVersion
	if err := contentRepo.InsertConversation(t.Context(), &models.WarmupConversation{ID: f.source.ID, PoolType: "premium", Source: models.WarmupContentSourceAI, Subject: f.source.Subject, Description: f.source.Description, Messages: f.source.Messages, Status: models.WarmupConversationActive, LintPassed: true, ReplyEligible: true, ScenarioVersion: &f.source.Version, RenderingVersion: &rendering, SemanticReview: "passed"}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = contentRepo.DeleteConversation(context.Background(), f.source.ID) })
	f.exec(t, `INSERT INTO users(id,email,first_name,last_name,password_hash) VALUES($1,$2,'Start','Test','x')`, f.user, f.user.String()+"@example.test")
	f.exec(t, `INSERT INTO organizations(id,name,slug,owner_user_id,risk_state) VALUES($1,'Start test',$2,$3,'trusted')`, f.org, f.org.String(), f.user)
	t.Cleanup(func() {
		for _, q := range []string{
			`DELETE FROM warmup_tokens WHERE sender_account_id IN(SELECT id FROM email_accounts WHERE organization_id=$1)`,
			`DELETE FROM tasks WHERE email_account_id IN(SELECT id FROM email_accounts WHERE organization_id=$1)`,
			`DELETE FROM email_accounts WHERE organization_id=$1`,
			`DELETE FROM organizations WHERE id=$1`,
		} {
			if _, err := handle.Exec(context.Background(), q, f.org); err != nil {
				t.Error(err)
			}
		}
		_, _ = handle.Exec(context.Background(), `DELETE FROM users WHERE id=$1`, f.user)
		_, _ = handle.Exec(context.Background(), `DELETE FROM fleet_nodes WHERE id=$1`, f.worker)
	})
	f.exec(t, `INSERT INTO fleet_nodes(id,role,active,last_seen_at,warmup_send_protocol) VALUES($1,'worker',true,NOW(),2)`, f.worker)
	f.exec(t, `INSERT INTO workers(id) VALUES($1)`, f.worker)
	for _, id := range []uuid.UUID{f.sender, f.recipient} {
		f.exec(t, `INSERT INTO email_accounts(id,user_id,organization_id,email,name,signature_plain,signature_html,provider,status,worker_id,timezone,test_mode,test_send_enabled,test_receive_enabled,warmup,warmup_pool_type,warmup_days,warmup_start_time,warmup_end_time,warmup_base,warmup_max,campaign_limit,shared_daily_limit,rolling_recipient_limit)
			VALUES($1,$2,$3,$4,'Local diagnostic','','','smtp_imap','active',$5,'UTC','diagnostic',true,true,NOW()-INTERVAL '30 days','free',127,'00:00','23:59',50,50,50,100,100)`, id, f.user, f.org, id.String()+"@example.test", f.worker)
		f.exec(t, `INSERT INTO warmup_pool_participants(pool_id,email_account_id,participant_role,health_state) SELECT id,$1,'sender_receiver','healthy' FROM warmup_pools WHERE pool_type='free'`, id)
	}
	f.exec(t, `INSERT INTO tasks(id,task_type,email_account_id,status,message_id,scheduled_at) VALUES($1,'warmup',$2,'active','',NOW())`, f.task, f.sender)
	f.exec(t, `INSERT INTO warmup_tasks(task_id,target_account_id,lineage_version,subject,scenario_version,rendering_version,max_turns) VALUES($1,$2,1,$3,$4,$5,$6)`, f.task, f.recipient, f.source.Subject, f.source.Version, generation.CanonicalRenderingVersion, len(f.source.Messages)+1)
	f.exec(t, `INSERT INTO warmup_tokens(token,task_id,sender_account_id,recipient_account_id,subject,content_source,conversation_id,conversation_turn) VALUES($1,$2,$3,$4,$5,'ai',$6,0)`, uuid.New(), f.task, f.sender, f.recipient, f.source.Subject, f.source.ID)
	f.repo = repository.NewTaskRepository(handle.Pool)
	f.admission = f.repo.(repository.OutboundAdmissionRepository)
	f.dispatch = f.repo.(repository.WarmupDispatchRepository)
	enc, err := encrypt.NewEncrypter([]byte("0123456789abcdef0123456789abcdef"))
	if err != nil {
		t.Fatal(err)
	}
	password, err := enc.Encrypt("unused-local-fixture-password")
	if err != nil {
		t.Fatal(err)
	}
	f.exec(t, `INSERT INTO email_accounts_smtp_imap(email_account_id,smtp_host,smtp_port,smtp_user,smtp_password,imap_host,imap_port,imap_user,imap_password) SELECT id,'smtp.test.invalid',587,email,$2,'imap.test.invalid',993,email,$2 FROM email_accounts WHERE organization_id=$1`, f.org, password)
	emailRepo := repository.NewEmailRepostory(handle, enc)
	warmupRepo := repository.NewWarmupRepository(handle.Pool)
	policy := scheduler.NewSchedulerService(f.repo, warmupRepo, nil, emailRepo, nil, nil, nil)
	f.svc = tasks.NewService(nil, nil, nil, nil, policy, nil, nil, nil, nil, f.repo, warmupRepo, nil, contentRepo, nil, emailRepo, nil, nil, nil, nil, nil, nil, nil)
	f.nonce, err = f.dispatch.AuthorizeWarmupDispatch(t.Context(), f.task, f.sender, f.worker)
	if err != nil {
		t.Fatal(err)
	}
	reserved, err := f.admission.ReserveOutbound(t.Context(), repository.OutboundReservation{TaskID: f.task, MailboxID: f.sender, OrganizationID: f.org, WorkerID: f.worker, Provider: models.InboxProviderSMTPIMAP, Recipients: []string{f.recipient.String() + "@example.test"}})
	if err != nil || reserved != f.nonce {
		t.Fatalf("generic warmup reservation: nonce=%s error=%v", reserved, err)
	}
	return f
}

func (f *outboundStartFixture) exec(t *testing.T, q string, args ...any) {
	t.Helper()
	if _, err := f.db.Exec(t.Context(), q, args...); err != nil {
		t.Fatal(err)
	}
}

func (f *outboundStartFixture) snapshot(t *testing.T) string {
	t.Helper()
	var raw string
	if err := f.db.QueryRow(t.Context(), `SELECT jsonb_build_object('task',to_jsonb(t),'warmup',to_jsonb(w),'mailbox',to_jsonb(ea),'token',(SELECT jsonb_agg(to_jsonb(wt) ORDER BY wt.token) FROM warmup_tokens wt WHERE wt.task_id=t.id),'statistics',(SELECT jsonb_agg(to_jsonb(ws) ORDER BY ws.date) FROM warmup_statistics ws WHERE ws.email_account_id=ea.id),'attempts',(SELECT jsonb_agg(to_jsonb(a) ORDER BY a.nonce) FROM outbound_attempts a WHERE a.task_id=t.id))::text
		FROM tasks t JOIN warmup_tasks w ON w.task_id=t.id JOIN email_accounts ea ON ea.id=t.email_account_id WHERE t.id=$1`, f.task).Scan(&raw); err != nil {
		t.Fatal(err)
	}
	return raw
}

func (f *outboundStartFixture) importPendingSnapshot(t *testing.T, status string) {
	t.Helper()
	before := f.snapshot(t)
	rows := map[string][]map[string]any{
		"tasks":         {{"id": f.task, "task_type": "warmup", "email_account_id": f.sender, "status": status, "message_id": "", "send_result_state": "unknown"}},
		"warmup_tasks":  {{"task_id": f.task, "target_account_id": f.recipient, "subject": f.source.Subject}},
		"warmup_tokens": {{"token": uuid.New(), "task_id": f.task, "sender_account_id": f.sender, "recipient_account_id": f.recipient, "subject": f.source.Subject, "content_source": "ai", "conversation_id": f.source.ID, "conversation_turn": 0}},
	}
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	manifest := orgtransfer.Manifest{Kind: orgtransfer.ArchiveKind, FormatVersion: models.OrgTransferFormatVersion, OrganizationID: f.org, ExportedAt: time.Now()}
	for _, table := range orgtransfer.Tables {
		data, ok := rows[table.Name]
		if !ok {
			continue
		}
		entry, err := zw.Create("data/" + table.Name + ".ndjson")
		if err != nil {
			t.Fatal(err)
		}
		columns := []string{}
		for c := range data[0] {
			columns = append(columns, c)
		}
		for _, row := range data {
			if err := json.NewEncoder(entry).Encode(row); err != nil {
				t.Fatal(err)
			}
		}
		manifest.Tables = append(manifest.Tables, orgtransfer.ManifestTable{Name: table.Name, Group: table.Group, Rows: int64(len(data)), Columns: columns})
	}
	entry, err := zw.Create("manifest.json")
	if err != nil {
		t.Fatal(err)
	}
	if err := json.NewEncoder(entry).Encode(manifest); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	svc := orgtransfer.NewService(repository.NewOrgTransferRepository(f.db), nil, nil, nil, orgtransfer.InstanceInfo{})
	result, err := svc.ImportFrom(t.Context(), f.org, bytes.NewReader(buf.Bytes()), orgtransfer.ImportOptions{Conflict: models.OrgImportConflictOverwrite, ActorUserID: f.user}, nil)
	if err != nil || result == nil || result.RowCounts["tasks"] != 0 || result.RowCounts["warmup_tasks"] != 0 || result.RowCounts["warmup_tokens"] != 0 {
		t.Fatalf("real partial archive import: result=%+v error=%v", result, err)
	}
	if after := f.snapshot(t); after != before {
		t.Fatalf("protected partial import changed local authority/history\nbefore=%s\nafter=%s", before, after)
	}
}

func (f *outboundStartFixture) prepareDeferredWarmup(t *testing.T) {
	t.Helper()
	lineage := f.repo.(repository.WarmupLineageRepository)
	if err := lineage.RescheduleWarmupTask(t.Context(), f.task, time.Now().Add(-time.Minute)); err != nil {
		t.Fatal(err)
	}
	if claimed, err := lineage.ClaimWarmupTask(t.Context(), f.task, time.Now()); err != nil || !claimed {
		t.Fatalf("claim deferred warmup: claimed=%v error=%v", claimed, err)
	}
	if err := lineage.SaveWarmupLineage(t.Context(), f.task, repository.WarmupLineage{
		Subject: f.source.Subject, ScenarioVersion: f.source.Version,
		RenderingVersion: generation.CanonicalRenderingVersion, MaxTurns: len(f.source.Messages) + 1,
	}); err != nil {
		t.Fatal(err)
	}
	if err := repository.NewWarmupRepository(f.db.Pool).CreateWarmupToken(t.Context(), &models.WarmupToken{
		Token: uuid.New(), TaskID: f.task, SenderAccountID: f.sender, RecipientAccountID: f.recipient,
		Subject: f.source.Subject, ContentSource: models.WarmupContentSourceAI, ConversationID: &f.source.ID,
		ExpiresAt: time.Now().Add(24 * time.Hour),
	}); err != nil {
		t.Fatal(err)
	}
}

type pausedOutboundValidation struct {
	tasks.TasksService
	validated chan struct{}
	resume    chan struct{}
	refuse    bool
}

func (p *pausedOutboundValidation) ValidateOutboundExecution(ctx context.Context, task uuid.UUID) error {
	validator := p.TasksService.(interface {
		ValidateOutboundExecution(context.Context, uuid.UUID) error
	})
	if err := validator.ValidateOutboundExecution(ctx, task); err != nil {
		return err
	}
	close(p.validated)
	select {
	case <-p.resume:
		if p.refuse {
			return validator.ValidateOutboundExecution(ctx, task)
		}
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (f *outboundStartFixture) request(t *testing.T, svc tasks.TasksService, start bool, result *models.SendEmailResult) (*httptest.ResponseRecorder, string) {
	t.Helper()
	body, err := json.Marshal(map[string]any{"task_id": f.task, "mailbox_id": f.sender, "worker_id": f.worker, "nonce": f.nonce, "start": start, "result": result})
	if err != nil {
		t.Fatal(err)
	}
	router := gin.New()
	h := &Handler{WarmupDispatch: f.dispatch, TasksService: svc}
	router.POST("/internal/warmup-dispatch", h.InternalWarmupDispatch)
	recorder := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/internal/warmup-dispatch", bytes.NewReader(body)).WithContext(t.Context())
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(recorder, req)
	var out repository.WarmupDispatchState
	if len(recorder.Body.Bytes()) > 0 && json.Unmarshal(recorder.Body.Bytes(), &out) != nil {
		t.Fatalf("bad dispatch reply: %s", recorder.Body.String())
	}
	return recorder, out.State
}

func TestLiveWarmupGenericStartAfterPartialImport(t *testing.T) {
	for _, mode := range []string{"null_nonce", "replacement_nonce", "cancel_null_nonce", "cancel_replacement_nonce"} {
		t.Run(mode, func(t *testing.T) {
			f := newOutboundStartFixture(t)
			pause := &pausedOutboundValidation{TasksService: f.svc, validated: make(chan struct{}), resume: make(chan struct{})}
			pause.refuse = mode == "cancel_null_nonce" || mode == "cancel_replacement_nonce"
			done := make(chan struct{})
			var response *httptest.ResponseRecorder
			var state string
			go func() {
				defer close(done)
				response, state = f.request(t, pause, true, nil)
			}()
			defer func() {
				select {
				case <-pause.resume:
				default:
					close(pause.resume)
				}
				select {
				case <-done:
				case <-time.After(10 * time.Second):
					t.Error("HTTP handler did not complete")
				}
			}()
			select {
			case <-pause.validated:
			case <-done:
				t.Fatalf("real execution validation failed before barrier: %v", response)
			case <-time.After(10 * time.Second):
				t.Fatal("execution validation barrier timed out")
			}
			if err := f.dispatch.DeferWarmupDispatch(t.Context(), f.task, f.sender, f.worker, f.nonce, time.Now()); err != nil {
				t.Fatal(err)
			}
			status := "completed"
			if mode == "replacement_nonce" || mode == "cancel_replacement_nonce" {
				status = "active"
			}
			f.importPendingSnapshot(t, status)
			f.prepareDeferredWarmup(t)
			if mode == "replacement_nonce" || mode == "cancel_replacement_nonce" {
				nonce, err := f.dispatch.AuthorizeWarmupDispatch(t.Context(), f.task, f.sender, f.worker)
				if err != nil || nonce == f.nonce {
					t.Fatalf("replacement authorization: nonce=%s error=%v", nonce, err)
				}
			}
			var current *uuid.UUID
			var generic uuid.UUID
			if err := f.db.QueryRow(t.Context(), `SELECT w.dispatch_nonce,t.send_executor_nonce FROM warmup_tasks w JOIN tasks t ON t.id=w.task_id WHERE t.id=$1`, f.task).Scan(&current, &generic); err != nil || generic != f.nonce || current != nil && *current == generic {
				t.Fatalf("valid operations did not reach mismatch: warmup=%v generic=%s error=%v", current, generic, err)
			}
			validator := f.svc.(interface {
				ValidateOutboundExecution(context.Context, uuid.UUID) error
			})
			if err := validator.ValidateOutboundExecution(t.Context(), f.task); err != nil {
				t.Fatalf("fresh local warmup preparation is not executable: %v", err)
			}
			if pause.refuse {
				if err := warmupapp.NewService(repository.NewWarmupRepository(f.db.Pool)).RemoveFromAllPools(t.Context(), f.recipient); err != nil {
					t.Fatal(err)
				}
				if err := validator.ValidateOutboundExecution(t.Context(), f.task); err == nil {
					t.Fatal("recipient pool removal did not fail the real execution validator")
				}
			}
			before := f.snapshot(t)
			close(pause.resume)
			<-done
			if response.Code != http.StatusOK || state != "denied" {
				t.Errorf("stale HTTP start accepted: code=%d state=%s body=%s", response.Code, state, response.Body.String())
			}
			if after := f.snapshot(t); after != before {
				t.Errorf("stale entrance changed task/warmup/token/accounting/hold/attempt evidence\nbefore=%s\nafter=%s", before, after)
			}
			result := models.SendEmailResult{TaskID: f.task, Success: true, MessageID: "<unstarted@example.test>"}
			response, _ = f.request(t, f.svc, false, &result)
			if response.Code != http.StatusConflict || f.snapshot(t) != before {
				t.Error("unstarted old reservation gained result authority", response.Code)
			}
		})
	}
}

func TestLiveWarmupGenericCancellationPreservesOtherHolds(t *testing.T) {
	for _, hold := range []string{"own_only", "another_unknown", "conflict"} {
		t.Run(hold, func(t *testing.T) {
			f := newOutboundStartFixture(t)
			other := uuid.New()
			if hold == "another_unknown" {
				f.exec(t, `INSERT INTO tasks(id,task_type,email_account_id,status,message_id,send_result_state) VALUES($1,'email',$2,'completed','','unknown')`, other, f.sender)
			}
			if hold == "conflict" {
				f.exec(t, `UPDATE email_accounts SET send_recovery_reason='conflict' WHERE id=$1`, f.sender)
			}
			if err := f.admission.CancelOutbound(t.Context(), f.task, f.sender, f.worker, f.nonce); err != nil {
				t.Fatal(err)
			}
			var cancelled, refunded, tokenAbsent, held bool
			var sent int
			var heldTask *uuid.UUID
			var reason *string
			if err := f.db.QueryRow(t.Context(), `SELECT t.status='cancelled' AND t.send_result_state='failed' AND t.send_result_applied_at IS NOT NULL AND t.send_released_at IS NOT NULL,
				w.warmup_refunded_at IS NOT NULL,ws.emails_sent,NOT EXISTS(SELECT 1 FROM warmup_tokens WHERE task_id=t.id),ea.send_recovery_hold,ea.send_recovery_task_id,ea.send_recovery_reason
				FROM tasks t JOIN warmup_tasks w ON w.task_id=t.id JOIN warmup_statistics ws ON ws.email_account_id=t.email_account_id AND ws.date=w.warmup_charged_date JOIN email_accounts ea ON ea.id=t.email_account_id WHERE t.id=$1`, f.task).Scan(&cancelled, &refunded, &sent, &tokenAbsent, &held, &heldTask, &reason); err != nil {
				t.Fatal(err)
			}
			if !cancelled || !refunded || sent != 0 || !tokenAbsent {
				t.Fatalf("valid unstarted cancellation lost charge/token/release: cancelled=%v refunded=%v sent=%d tokenAbsent=%v", cancelled, refunded, sent, tokenAbsent)
			}
			if hold == "own_only" && (held || heldTask != nil || reason != nil) || hold == "another_unknown" && (!held || heldTask == nil || *heldTask != other || reason == nil || *reason != "unknown") || hold == "conflict" && (!held || heldTask == nil || *heldTask != f.task || reason == nil || *reason != "conflict") {
				t.Fatalf("incorrect hold release: mode=%s held=%v task=%v reason=%v", hold, held, heldTask, reason)
			}
			before := f.snapshot(t)
			if err := f.admission.CancelOutbound(t.Context(), f.task, f.sender, f.worker, f.nonce); err != nil || f.snapshot(t) != before {
				t.Fatal("repeated cancellation changed refund/history", err)
			}
		})
	}
}

func TestLiveWarmupGenericStartAndLateResult(t *testing.T) {
	f := newOutboundStartFixture(t)
	response, state := f.request(t, f.svc, true, nil)
	if response.Code != http.StatusOK || state != "execute" {
		t.Fatalf("valid generic entrance: code=%d state=%s body=%s", response.Code, state, response.Body.String())
	}
	before := f.snapshot(t)
	response, state = f.request(t, f.svc, true, nil)
	if response.Code != http.StatusOK || state != "started" || f.snapshot(t) != before {
		t.Fatal("duplicate entrance changed authority or attempts", response.Code, state)
	}
	if err := f.admission.CancelOutbound(t.Context(), f.task, f.sender, f.worker, f.nonce); err != nil || f.snapshot(t) != before {
		t.Fatal("cancel released started unknown", err)
	}
	f.importPendingSnapshot(t, "completed")
	result := models.SendEmailResult{TaskID: f.task, Success: true, MessageID: "<local-result@example.test>", SentAt: time.Now().UTC()}
	for range 2 {
		response, _ = f.request(t, f.svc, false, &result)
		if response.Code != http.StatusNoContent {
			t.Fatalf("correlated late task result rejected: code=%d body=%s", response.Code, response.Body.String())
		}
	}
	stateResult, err := f.admission.InspectOutbound(t.Context(), f.task, f.sender, f.worker)
	if err != nil || stateResult.State != "finished" || stateResult.Result == nil || stateResult.Result.MessageID != result.MessageID {
		t.Fatalf("correlated result missing: %+v %v", stateResult, err)
	}
}

func TestLiveWarmupGenericStartAfterCompatibleOverwrite(t *testing.T) {
	f := newOutboundStartFixture(t)
	f.importPendingSnapshot(t, "completed")
	var current uuid.UUID
	if err := f.db.QueryRow(t.Context(), `SELECT dispatch_nonce FROM warmup_tasks WHERE task_id=$1`, f.task).Scan(&current); err != nil || current != f.nonce {
		t.Fatalf("compatible overwrite changed active dispatch nonce: %s %v", current, err)
	}
	validator := f.svc.(interface {
		ValidateOutboundExecution(context.Context, uuid.UUID) error
	})
	if err := validator.ValidateOutboundExecution(t.Context(), f.task); err != nil {
		t.Fatal(err)
	}
	response, state := f.request(t, f.svc, true, nil)
	if response.Code != http.StatusOK || state != "execute" {
		t.Fatalf("matching reservation blocked after compatible import: %d %s", response.Code, response.Body.String())
	}
	result := models.SendEmailResult{TaskID: f.task, Success: true, MessageID: "<confirmed-local@example.test>"}
	response, _ = f.request(t, f.svc, false, &result)
	if response.Code != http.StatusNoContent {
		t.Fatalf("matching started reservation lost correlated result: %d %s", response.Code, response.Body.String())
	}
}

func TestLiveWarmupGenericStartAfterMailboxArchiveImport(t *testing.T) {
	for _, sourceOnly := range []bool{false, true} {
		name := "local_mailbox_rotation"
		if sourceOnly {
			name = "source_history"
		}
		t.Run(name, func(t *testing.T) {
			f := newOutboundStartFixture(t)
			enc, err := encrypt.NewEncrypter([]byte("0123456789abcdef0123456789abcdef"))
			if err != nil {
				t.Fatal(err)
			}
			svc := orgtransfer.NewService(repository.NewOrgTransferRepository(f.db), nil, enc, nil, orgtransfer.InstanceInfo{})
			const passphrase = "synthetic-mailbox-import-control"
			var buf bytes.Buffer
			if _, err := svc.ExportTo(t.Context(), f.org, orgtransfer.ExportOptions{Groups: []models.OrgDataGroup{models.OrgDataGroupSending}, Passphrase: passphrase}, &buf, nil); err != nil {
				t.Fatal(err)
			}
			var generationBefore uuid.UUID
			if err := f.db.QueryRow(t.Context(), `SELECT transport_generation FROM email_accounts WHERE id=$1`, f.sender).Scan(&generationBefore); err != nil {
				t.Fatal(err)
			}
			if sourceOnly {
				f.exec(t, `DELETE FROM tasks WHERE id=$1`, f.task)
			}
			result, err := svc.ImportFrom(t.Context(), f.org, bytes.NewReader(buf.Bytes()), orgtransfer.ImportOptions{Conflict: models.OrgImportConflictOverwrite, ActorUserID: f.user, Passphrase: passphrase}, nil)
			if err != nil || result == nil {
				t.Fatalf("mailbox archive import: result=%+v error=%v", result, err)
			}
			var count int64
			if sourceOnly {
				count = 1
			}
			for _, table := range []string{"tasks", "warmup_tasks", "warmup_tokens"} {
				if result.RowCounts[table] != count {
					t.Fatalf("mailbox archive %s count=%d want=%d", table, result.RowCounts[table], count)
				}
			}
			var generationAfter uuid.UUID
			var worker *uuid.UUID
			if err := f.db.QueryRow(t.Context(), `SELECT transport_generation,worker_id FROM email_accounts WHERE id=$1`, f.sender).Scan(&generationAfter, &worker); err != nil || generationAfter == generationBefore || worker == nil || *worker != f.worker {
				t.Fatalf("mailbox import failed to rotate generation or preserve destination placement: before=%s after=%s worker=%v error=%v", generationBefore, generationAfter, worker, err)
			}
			var generic, dispatch *uuid.UUID
			var lineage *int
			if err := f.db.QueryRow(t.Context(), `SELECT t.send_executor_nonce,w.dispatch_nonce,w.lineage_version FROM tasks t JOIN warmup_tasks w ON w.task_id=t.id WHERE t.id=$1`, f.task).Scan(&generic, &dispatch, &lineage); err != nil {
				t.Fatal(err)
			}
			if sourceOnly {
				if generic != nil || dispatch != nil || lineage != nil {
					t.Fatalf("imported history retained source execution authority: generic=%v dispatch=%v lineage=%v", generic, dispatch, lineage)
				}
				var unknown bool
				if err := f.db.QueryRow(t.Context(), `SELECT ea.send_recovery_hold AND t.send_result_state='unknown' AND t.send_result_applied_at IS NULL FROM tasks t JOIN email_accounts ea ON ea.id=t.email_account_id WHERE t.id=$1`, f.task).Scan(&unknown); err != nil || !unknown {
					t.Fatalf("imported uncertainty was cleared: unknown=%v error=%v", unknown, err)
				}
			} else if generic == nil || *generic != f.nonce || dispatch == nil || *dispatch != f.nonce || lineage == nil || *lineage != 1 {
				t.Fatalf("mailbox rotation erased local identity: generic=%v dispatch=%v lineage=%v", generic, dispatch, lineage)
			}
			oldWorker := f.worker
			before := f.snapshot(t)
			f.worker = uuid.New()
			status, state := f.request(t, f.svc, true, nil)
			f.worker = oldWorker
			wantStatus := http.StatusOK
			if sourceOnly {
				wantStatus = http.StatusServiceUnavailable
			}
			if status.Code != wantStatus || state == "execute" || state == "started" || f.snapshot(t) != before {
				t.Fatalf("foreign worker acquired imported/local authority: status=%d state=%s", status.Code, state)
			}
			status, state = f.request(t, f.svc, true, nil)
			t.Logf("sourceOnly=%v old command: status=%d state=%s", sourceOnly, status.Code, state)
			if sourceOnly {
				if status.Code != wantStatus || state == "execute" || state == "started" || f.snapshot(t) != before {
					t.Fatalf("source history acquired execution authority: status=%d state=%s", status.Code, state)
				}
				result := models.SendEmailResult{TaskID: f.task, Success: true, MessageID: "<unstarted-source@example.test>"}
				response, _ := f.request(t, f.svc, false, &result)
				if response.Code != http.StatusConflict || f.snapshot(t) != before {
					t.Fatal("unstarted source history gained result authority", response.Code)
				}
			} else if status.Code != http.StatusOK || state != "execute" {
				t.Fatalf("preserved local command lost execution authority: status=%d state=%s", status.Code, state)
			}
			var started bool
			if err := f.db.QueryRow(t.Context(), `SELECT t.send_executor_started_at IS NOT NULL OR w.dispatch_started_at IS NOT NULL OR EXISTS(SELECT 1 FROM outbound_attempts WHERE task_id=t.id) FROM tasks t JOIN warmup_tasks w ON w.task_id=t.id WHERE t.id=$1`, f.task).Scan(&started); err != nil || started == sourceOnly {
				t.Fatalf("execution markers disagree with local/source authority: started=%v sourceOnly=%v error=%v", started, sourceOnly, err)
			}
		})
	}
}

func TestLiveWarmupGenericCannotRepeatStartedWarmupClaim(t *testing.T) {
	f := newOutboundStartFixture(t)
	started, err := f.dispatch.BeginWarmupDispatch(t.Context(), f.task, f.sender, f.worker, f.nonce)
	if err != nil || started.State != "execute" {
		t.Fatalf("native warmup start did not consume the claim: %+v %v", started, err)
	}
	before := f.snapshot(t)
	response, state := f.request(t, f.svc, true, nil)
	if response.Code != http.StatusOK || state != "denied" || f.snapshot(t) != before {
		t.Fatalf("generic entrance repeated an already started warmup claim: code=%d state=%s", response.Code, state)
	}
	if err := f.admission.CancelOutbound(t.Context(), f.task, f.sender, f.worker, f.nonce); err != nil || f.snapshot(t) != before {
		t.Fatal("cancel erased already started native warmup claim", err)
	}
}

func TestLiveWarmupGenericDeniesHistoricalDiagnosticContent(t *testing.T) {
	f := newOutboundStartFixture(t)
	legacy := tasks.Conversation{ID: uuid.NewSHA1(uuid.NameSpaceOID, []byte("warmbly:diagnostic-clock:v1")), Subject: "Simulated diagnostic: fictional clock"}
	f.exec(t, `UPDATE warmup_tasks SET subject=$2 WHERE task_id=$1`, f.task, legacy.Subject)
	f.exec(t, `UPDATE warmup_tokens SET subject=$2,content_source='static',conversation_id=$3 WHERE task_id=$1`, f.task, legacy.Subject, legacy.ID)
	response, state := f.request(t, f.svc, true, nil)
	if response.Code != http.StatusOK || state != "denied" {
		t.Fatalf("historical wording authorized: status=%d state=%s", response.Code, state)
	}
	var unstarted bool
	if err := f.db.QueryRow(t.Context(), `SELECT send_executor_started_at IS NULL FROM tasks WHERE id=$1`, f.task).Scan(&unstarted); err != nil || !unstarted {
		t.Fatalf("denied content entered native execution: unstarted=%v err=%v", unstarted, err)
	}
}
