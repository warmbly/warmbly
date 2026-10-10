package repository

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/models"
)

func TestLiveWarmupActionsDenyUnavailableMailbox(t *testing.T) {
	for _, state := range []string{"missing", "deleted", "unassigned"} {
		t.Run(state, func(t *testing.T) {
			f, r := lineageFixture(t)
			worker := uuid.New()
			mailbox := f.recipient
			switch state {
			case "missing":
				mailbox = uuid.New()
			case "deleted":
				if _, err := f.pool.Exec(t.Context(), `DELETE FROM email_accounts WHERE id=$1`, mailbox); err != nil {
					t.Fatal(err)
				}
			case "unassigned":
				if _, err := f.pool.Exec(t.Context(), `UPDATE email_accounts SET worker_id=NULL WHERE id=$1`, mailbox); err != nil {
					t.Fatal(err)
				}
			}
			actions, err := r.PermittedWarmupActions(t.Context(), mailbox, worker, []string{models.WarmupActionFile, models.WarmupActionDelete})
			if err != nil || len(actions) != 0 {
				t.Fatalf("unavailable mailbox must deny provider actions without retry: actions=%v err=%v", actions, err)
			}
		})
	}
}

func TestLiveWarmupActionsPreserveAuthorityFailure(t *testing.T) {
	f, r := lineageFixture(t)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	actions, err := r.PermittedWarmupActions(ctx, f.recipient, uuid.New(), []string{models.WarmupActionDelete})
	if !errors.Is(err, context.Canceled) || len(actions) != 0 {
		t.Fatalf("authority failure must remain retryable: actions=%v err=%v", actions, err)
	}
}

func TestLiveWarmupFilingAuthorityRequiresOwnedPendingWork(t *testing.T) {
	f, r := lineageFixture(t)
	worker := uuid.New()
	ctx := t.Context()
	exec := func(query string, args ...any) {
		t.Helper()
		if _, err := f.pool.Exec(ctx, query, args...); err != nil {
			t.Fatal(err)
		}
	}
	exec(`INSERT INTO fleet_nodes(id,role,active,last_seen_at,warmup_send_protocol)VALUES($1,'worker',true,NOW(),2)`, worker)
	exec(`INSERT INTO workers(id)VALUES($1)`, worker)
	t.Cleanup(func() { _, _ = f.pool.Exec(context.Background(), `DELETE FROM fleet_nodes WHERE id=$1`, worker) })
	exec(`UPDATE email_accounts SET worker_id=$1,test_mode='diagnostic',test_receive_enabled=true,send_recovery_hold=true,send_recovery_reason='unknown' WHERE id=$2`, worker, f.recipient)
	recovery := NewWarmupRecoveryRepository(f.pool)
	id, err := recovery.EnqueueFiling(ctx, models.WarmupEmailAction{EmailID: f.recipient, RFCMessageID: "<durable@example.test>", Actions: []string{models.WarmupActionFile}})
	if err != nil {
		t.Fatal(err)
	}
	req := models.WarmupActionRequest{MailboxID: f.recipient, WorkerID: worker, FilingID: id.String(), Actions: []string{models.WarmupActionFile}}
	for _, tc := range []struct {
		name    string
		change  func(*models.WarmupActionRequest)
		pending bool
	}{
		{"pending", func(*models.WarmupActionRequest) {}, true},
		{"missing filing", func(r *models.WarmupActionRequest) { r.FilingID = uuid.NewString() }, false},
		{"other mailbox", func(r *models.WarmupActionRequest) { r.MailboxID = f.sender }, false},
		{"other worker", func(r *models.WarmupActionRequest) { r.WorkerID = uuid.New() }, false},
		{"mixed action", func(r *models.WarmupActionRequest) {
			r.Actions = []string{models.WarmupActionFile, models.WarmupActionDelete}
		}, false},
		{"legacy event", func(r *models.WarmupActionRequest) { r.FilingID = "" }, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			patch := req
			tc.change(&patch)
			out, err := r.AdmitWarmupAction(ctx, patch)
			if err != nil || out.FilingPending != tc.pending {
				t.Fatalf("decision=%+v err=%v", out, err)
			}
		})
	}
	exec(`UPDATE warmup_pending_filings SET payload=jsonb_set(payload,'{actions}',to_jsonb($2::text[])) WHERE id=$1`, id, []string{models.WarmupActionFile, models.WarmupActionDelete})
	if out, err := r.AdmitWarmupAction(ctx, req); err != nil || out.FilingPending {
		t.Fatal("mixed durable work granted filing-only proof", out, err)
	}
	exec(`UPDATE warmup_pending_filings SET payload=jsonb_set(payload,'{actions}',to_jsonb($2::text[])) WHERE id=$1`, id, []string{models.WarmupActionFile})
	// Inspecting authority does not complete work or bypass recovery backoff.
	claimed, err := recovery.ClaimFilings(ctx, 1)
	if err != nil || len(claimed) != 1 || claimed[0].FilingID != id.String() {
		t.Fatal("filing was lost before acknowledgement", claimed, err)
	}
	if claimed, err := recovery.ClaimFilings(ctx, 1); err != nil || len(claimed) != 0 {
		t.Fatal("retry backoff was lost", claimed, err)
	}
	if out, err := r.AdmitWarmupAction(ctx, req); err != nil || !out.FilingPending {
		t.Fatal("backoff erased durable authority", out, err)
	}
	exec(`UPDATE email_accounts SET test_mode='off' WHERE id=$1`, f.recipient)
	if out, err := r.AdmitWarmupAction(ctx, req); err != nil || out.FilingPending || len(out.Actions) != 0 {
		t.Fatal("revoked filing remained permitted", out, err)
	}
	exec(`UPDATE email_accounts SET test_mode='diagnostic' WHERE id=$1`, f.recipient)
	if err := recovery.CompleteFiling(ctx, f.recipient, id); err != nil {
		t.Fatal(err)
	}
	if out, err := r.AdmitWarmupAction(ctx, req); err != nil || out.FilingPending {
		t.Fatal("completed filing retained pending proof", out, err)
	}
	var held bool
	var reason string
	if err := f.pool.QueryRow(ctx, `SELECT send_recovery_hold,send_recovery_reason FROM email_accounts WHERE id=$1`, f.recipient).Scan(&held, &reason); err != nil || !held || reason != "unknown" {
		t.Fatal("filing authority changed uncertain-send protection", err)
	}
	ctx, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := r.AdmitWarmupAction(ctx, req); !errors.Is(err, context.Canceled) {
		t.Fatal("authority failure did not remain retryable", err)
	}
}

func TestLiveDiagnosticOffStopsQueuedSendAndActions(t *testing.T) {
	f, r := lineageFixture(t)
	ctx := t.Context()
	worker := uuid.New()
	exec := func(sql string, args ...any) {
		t.Helper()
		if _, err := f.pool.Exec(ctx, sql, args...); err != nil {
			t.Fatal(err)
		}
	}
	exec(`INSERT INTO fleet_nodes(id,role,active,last_seen_at,warmup_send_protocol)VALUES($1,'worker',true,NOW(),2)`, worker)
	exec(`INSERT INTO workers(id)VALUES($1)`, worker)
	t.Cleanup(func() { _, _ = f.pool.Exec(context.Background(), `DELETE FROM fleet_nodes WHERE id=$1`, worker) })
	exec(`UPDATE email_accounts SET worker_id=$1,test_mode='diagnostic',test_send_enabled=true,test_receive_enabled=true WHERE id IN($2,$3)`, worker, f.sender, f.recipient)
	exec(`INSERT INTO warmup_pool_participants(pool_id,email_account_id)SELECT wp.id,ea.id FROM warmup_pools wp CROSS JOIN email_accounts ea WHERE wp.pool_type='free' AND ea.id IN($1,$2) ON CONFLICT DO NOTHING`, f.sender, f.recipient)
	reserve := func() (uuid.UUID, uuid.UUID) {
		t.Helper()
		id := uuid.New()
		exec(`INSERT INTO tasks(id,task_type,email_account_id,status,message_id)VALUES($1,'warmup',$2,'active','')`, id, f.sender)
		exec(`INSERT INTO warmup_tasks(task_id,target_account_id,lineage_version,subject,scenario_version,rendering_version,max_turns)VALUES($1,$2,1,'Pinned diagnostic','diagnostic-v1','canonical-v1',3)`, id, f.recipient)
		exec(`INSERT INTO warmup_tokens(token,task_id,sender_account_id,recipient_account_id,subject)VALUES($1,$2,$3,$4,'Pinned diagnostic')`, uuid.New(), id, f.sender, f.recipient)
		if _, err := r.AuthorizeWarmupDispatch(ctx, id, f.sender, worker); err != nil {
			t.Fatal(err)
		}
		nonce, err := r.ReserveOutbound(ctx, OutboundReservation{TaskID: id, MailboxID: f.sender, OrganizationID: f.org, WorkerID: worker, Provider: models.InboxProviderSMTPIMAP, Recipients: []string{"pl-" + f.recipient.String()[:8] + "@test.local"}})
		if err != nil {
			t.Fatal(err)
		}
		return id, nonce
	}
	id, nonce := reserve()
	exec(`UPDATE email_accounts SET test_mode='off' WHERE id=$1`, f.recipient)
	if state, err := r.BeginOutbound(ctx, id, f.sender, worker, nonce); err != nil || state.State != "denied" {
		t.Fatal("queued send ignored recipient revocation")
	}
	exec(`UPDATE email_accounts SET test_mode='diagnostic',test_receive_enabled=true,test_send_enabled=false WHERE id=$1`, f.recipient)
	id, nonce = reserve()
	exec(`UPDATE email_accounts SET test_send_enabled=false WHERE id=$1`, f.sender)
	if state, err := r.BeginOutbound(ctx, id, f.sender, worker, nonce); err != nil || state.State != "denied" {
		t.Fatal("recipient-only mailbox was allowed to send")
	}
	exec(`UPDATE email_accounts SET test_send_enabled=true WHERE id=$1`, f.sender)
	id, nonce = reserve()
	if state, err := r.BeginOutbound(ctx, id, f.sender, worker, nonce); err != nil || state.State != "execute" {
		t.Fatal("valid sender/recipient consent unavailable", err)
	}
	actions := []string{models.WarmupActionMarkRead, models.WarmupActionRescueFromSpam, models.WarmupActionStar, models.WarmupActionFile, models.WarmupActionDelete}
	got, err := r.PermittedWarmupActions(ctx, f.recipient, worker, actions)
	if err != nil || len(got) != 2 || got[0] != models.WarmupActionFile || got[1] != models.WarmupActionDelete {
		t.Fatal("diagnostic mode permitted synthetic engagement", got, err)
	}
	exec(`UPDATE email_accounts SET test_mode='legacy' WHERE id=$1`, f.recipient)
	got, err = r.PermittedWarmupActions(ctx, f.recipient, worker, actions)
	if err != nil || len(got) != len(actions) {
		t.Fatal("explicit legacy behavior changed", got, err)
	}
	got, err = r.PermittedWarmupActions(ctx, f.recipient, uuid.New(), actions)
	if err != nil || len(got) != 0 {
		t.Fatal("a different worker gained provider action authority", got, err)
	}
	exec(`UPDATE email_accounts SET test_mode='off' WHERE id=$1`, f.recipient)
	got, err = r.PermittedWarmupActions(ctx, f.recipient, worker, actions)
	if err != nil || len(got) != 1 || got[0] != models.WarmupActionDelete {
		t.Fatal("queued actions ignored stop", got, err)
	}
	exec(`UPDATE fleet_nodes SET warmup_send_protocol=1 WHERE id=$1`, worker)
	got, err = r.PermittedWarmupActions(ctx, f.recipient, worker, actions)
	if err != nil || len(got) != 0 {
		t.Fatal("old executor gained capable action authority", got, err)
	}
}
