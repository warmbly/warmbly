package repository

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/models"
)

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
