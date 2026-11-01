package repository

import (
	"context"
	"errors"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/infrastructure/db"
	"github.com/warmbly/warmbly/internal/models"
)

type campaignIsolationFixture struct {
	*campaignAdmissionFixture
	request          OutboundReservation
	contactID, nonce uuid.UUID
}

func newCampaignIsolationFixture(t *testing.T) *campaignIsolationFixture {
	t.Helper()
	base := newCampaignAdmissionFixture(t, 100)
	request, contact := base.request(t, base.sender)
	nonce, err := base.r.ReserveOutbound(t.Context(), request)
	if err != nil {
		t.Fatal(err)
	}
	state, err := base.r.BeginOutbound(t.Context(), request.TaskID, base.sender, base.worker, nonce)
	if err != nil || state.State != "execute" {
		t.Fatalf("begin: %+v %v", state, err)
	}
	f := &campaignIsolationFixture{base, request, contact, nonce}
	f.exec(t, `UPDATE tasks SET send_reserved_at=NOW()-INTERVAL '1 hour',send_executor_started_at=NOW()-INTERVAL '1 hour',send_executor_result=jsonb_build_object('task_id',id,'success',false) WHERE id=$1`, request.TaskID)
	f.exec(t, `UPDATE outbound_attempts SET attempted_at=NOW()-INTERVAL '1 hour' WHERE task_id=$1`, request.TaskID)
	return f
}

func (f *campaignIsolationFixture) isolate(t *testing.T) {
	t.Helper()
	n, err := f.r.IsolateUnknownCampaignSends(t.Context(), time.Now().Add(-10*time.Minute), 100)
	if err != nil || n != 1 {
		t.Fatalf("isolate: count=%d err=%v", n, err)
	}
}

func (f *campaignIsolationFixture) assertSQL(t *testing.T, query string, args ...any) {
	t.Helper()
	var ok bool
	if err := f.pool.QueryRow(t.Context(), query, args...).Scan(&ok); err != nil || !ok {
		t.Fatalf("state assertion failed: %v (%s)", err, query)
	}
}

func TestLiveCampaignUnknownIsolationPreservesChargeAndFencesCommand(t *testing.T) {
	f := newCampaignIsolationFixture(t)
	var before string
	snapshot := `SELECT jsonb_build_object('nonce',send_executor_nonce,'started',send_executor_started_at,'reserved',send_reserved_at,'result',send_executor_result,'attempts',(SELECT jsonb_agg(to_jsonb(a)) FROM outbound_attempts a WHERE a.task_id=t.id),'daily',(SELECT jsonb_agg(to_jsonb(d)) FROM campaign_daily_sends d WHERE d.campaign_id=$2))::text FROM tasks t WHERE id=$1`
	if err := f.pool.QueryRow(t.Context(), snapshot, f.request.TaskID, f.campaign).Scan(&before); err != nil {
		t.Fatal(err)
	}
	f.isolate(t)
	var after string
	if err := f.pool.QueryRow(t.Context(), snapshot, f.request.TaskID, f.campaign).Scan(&after); err != nil {
		t.Fatal(err)
	}
	if before != after {
		t.Fatalf("isolation changed evidence or charge: before=%s after=%s", before, after)
	}
	if charged, err := f.r.CountCampaignEmailsSentToday(t.Context(), f.sender); err != nil || charged != 1 {
		t.Fatalf("isolated send lost its daily capacity charge: count=%d err=%v", charged, err)
	}
	f.assertSQL(t, `SELECT t.status='cancelled' AND t.campaign_send_isolated_at IS NOT NULL AND t.send_result_state='unknown' AND t.send_result_applied_at IS NULL AND t.send_released_at IS NULL AND NOT ea.send_recovery_hold FROM tasks t JOIN email_accounts ea ON ea.id=t.email_account_id WHERE t.id=$1`, f.request.TaskID)
	f.assertSQL(t, `SELECT cl.send_outcome_pending_at IS NOT NULL AND p.dispatched_at IS NOT NULL AND p.sent_at IS NULL FROM campaign_contact_progress p JOIN campaign_leads cl ON cl.campaign_id=p.campaign_id AND cl.contact_id=p.contact_id WHERE dispatch_task_id=$1`, f.request.TaskID)
	state, err := f.r.BeginOutbound(t.Context(), f.request.TaskID, f.sender, f.worker, f.nonce)
	if err != nil || state.State != "denied" {
		t.Fatalf("isolated command executable: %+v %v", state, err)
	}
	if _, err := f.r.ReserveOutbound(t.Context(), f.request); !errors.Is(err, ErrSendAdmissionDenied) {
		t.Fatalf("old reservation reopened: %v", err)
	}
	next, _ := f.campaignAdmissionFixture.request(t, f.sender)
	nonce, err := f.r.ReserveOutbound(t.Context(), next)
	if err != nil {
		t.Fatalf("unrelated contact blocked: %v", err)
	}
	state, err = f.r.BeginOutbound(t.Context(), next.TaskID, f.sender, f.worker, nonce)
	if err != nil || state.State != "execute" {
		t.Fatalf("unrelated contact execution: %+v %v", state, err)
	}
	if n, err := f.r.IsolateUnknownCampaignSends(t.Context(), time.Now().Add(-10*time.Minute), 100); err != nil || n != 0 {
		t.Fatalf("isolation repeated: %d %v", n, err)
	}
}

func TestLiveCampaignUnknownIsolationProtectsUnverifiedBoundaries(t *testing.T) {
	for _, tc := range []struct{ name, sql string }{
		{"no nonce", `UPDATE tasks SET send_executor_nonce=NULL WHERE id=$1`},
		{"recent execution", `UPDATE tasks SET send_executor_started_at=NOW() WHERE id=$1`},
		{"malformed evidence", `UPDATE tasks SET send_executor_result='[]' WHERE id=$1`},
		{"other task evidence", `UPDATE tasks SET send_executor_result=jsonb_build_object('task_id',gen_random_uuid()) WHERE id=$1`},
		{"definitive result", `UPDATE tasks SET send_executor_result=jsonb_build_object('task_id',id,'success',true) WHERE id=$1`},
		{"authentication hold", `UPDATE email_accounts SET send_recovery_reason='authentication' WHERE id=(SELECT email_account_id FROM tasks WHERE id=$1)`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newCampaignIsolationFixture(t)
			f.exec(t, tc.sql, f.request.TaskID)
			n, err := f.r.IsolateUnknownCampaignSends(t.Context(), time.Now().Add(-10*time.Minute), 100)
			if err != nil || n != 0 {
				t.Fatalf("protected send isolated: %d %v", n, err)
			}
			f.assertSQL(t, `SELECT t.campaign_send_isolated_at IS NULL AND ea.send_recovery_hold FROM tasks t JOIN email_accounts ea ON ea.id=t.email_account_id WHERE t.id=$1`, f.request.TaskID)
		})
	}
}

func TestLiveCampaignUnknownIsolationLateEvidenceReplaysOnce(t *testing.T) {
	f := newCampaignIsolationFixture(t)
	f.isolate(t)
	next, _ := f.campaignAdmissionFixture.request(t, f.sender)
	if _, err := f.r.ReserveOutbound(t.Context(), next); err != nil {
		t.Fatal(err)
	}
	result := models.SendEmailResult{TaskID: f.request.TaskID, Success: true, MessageID: "<confirmed@example.test>", SentAt: time.Now().UTC()}
	if err := f.r.FinishOutbound(t.Context(), result.TaskID, f.sender, f.worker, result); err != nil {
		t.Fatalf("late definitive evidence not persisted: %v", err)
	}
	pending, err := f.r.ListUnappliedSendResults(t.Context(), 100)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, row := range pending {
		if row.TaskID == result.TaskID && row.Success {
			found = true
		}
	}
	if !found {
		t.Fatal("isolated definitive result missing from recovery")
	}
	calls := 0
	for range 2 {
		err := f.r.ApplySendResult(t.Context(), result, func(ctx context.Context) error {
			calls++
			_, err := resultDB(ctx, f.pool).Exec(ctx, `UPDATE campaign_contact_progress SET sent_at=NOW() WHERE dispatch_task_id=$1`, result.TaskID)
			return err
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	if calls != 1 {
		t.Fatalf("result effects ran %d times", calls)
	}
	f.assertSQL(t, `SELECT t.status='cancelled' AND t.campaign_send_isolated_at IS NOT NULL AND t.send_result_state='sent' AND t.send_result_applied_at IS NOT NULL AND p.sent_at IS NOT NULL AND cl.send_outcome_pending_at IS NULL FROM tasks t JOIN campaign_contact_progress p ON p.dispatch_task_id=t.id JOIN campaign_leads cl ON cl.campaign_id=p.campaign_id AND cl.contact_id=p.contact_id WHERE t.id=$1`, result.TaskID)
	f.assertSQL(t, `SELECT send_recovery_hold AND send_recovery_task_id=$2 FROM email_accounts WHERE id=$1`, f.sender, next.TaskID)
	state, err := f.r.InspectOutbound(t.Context(), result.TaskID, f.sender, f.worker)
	if err != nil || state.State != "denied" {
		t.Fatalf("confirmed original regained authority: %+v %v", state, err)
	}
}

func TestLiveCampaignUnknownIsolationPortablePauseAndDatabaseFence(t *testing.T) {
	f := newCampaignIsolationFixture(t)
	f.isolate(t)
	f.exec(t, `UPDATE tasks SET send_executor_nonce=NULL,send_executor_started_at=NULL WHERE id=$1`, f.request.TaskID)
	if _, err := f.pool.Exec(t.Context(), `UPDATE tasks SET send_executor_started_at=NOW() WHERE id=$1`, f.request.TaskID); err == nil {
		t.Fatal("old writer restarted isolated execution")
	}
	f.exec(t, `DELETE FROM campaign_tasks WHERE task_id=$1`, f.request.TaskID)
	f.exec(t, `DELETE FROM tasks WHERE id=$1`, f.request.TaskID)
	f.assertSQL(t, `SELECT `+recoveryPausedLeadSQL("cl")+` FROM campaign_leads cl WHERE campaign_id=$1 AND contact_id=$2`, f.campaign, f.contactID)
	replacement := uuid.New()
	nextStep := uuid.New()
	f.exec(t, `INSERT INTO sequences(id,campaign_id,organization_id,name,subject,body_plain,body_html,wait_after,position) VALUES($1,$2,$3,'Next','Test','Text','',0,2)`, nextStep, f.campaign, f.org)
	f.exec(t, `INSERT INTO tasks(id,task_type,email_account_id,status,message_id) VALUES($1,'campaign',$2,'active','')`, replacement, f.sender)
	claimed, err := NewCampaignProgressRepository(f.pool).ReserveSend(t.Context(), f.campaign, f.contactID, nextStep, replacement, f.sender, false)
	if err != nil || claimed {
		t.Fatalf("portable recovery pause lost: claimed=%t err=%v", claimed, err)
	}
}

func TestLiveCampaignUnknownIsolationPreservesManualPauseAndFinalStepCounts(t *testing.T) {
	f := newCampaignIsolationFixture(t)
	progress := NewCampaignProgressRepository(f.pool)
	if _, err := progress.HoldLead(t.Context(), f.campaign, f.contactID, nil, "User pause", "manual"); err != nil {
		t.Fatal(err)
	}
	f.exec(t, `UPDATE campaign_contact_progress SET sent_at=NOW() WHERE dispatch_task_id=$1`, f.request.TaskID)
	f.isolate(t)
	f.assertSQL(t, `SELECT pause_source='manual' AND pause_reason='User pause' AND paused_at IS NOT NULL FROM campaign_leads WHERE campaign_id=$1 AND contact_id=$2`, f.campaign, f.contactID)
	assertCounts := func() {
		t.Helper()
		held, err := progress.CountHeldLeads(t.Context(), f.campaign)
		if err != nil || held != 1 {
			t.Fatalf("campaign completion hold count=%d err=%v", held, err)
		}
		supply, err := progress.LeadSupply(t.Context(), f.campaign, time.Now().Add(time.Hour), nil)
		if err != nil || supply.Held != 1 {
			t.Fatalf("final-step supply=%+v err=%v", supply, err)
		}
		counts, xerr := NewContactRepostory(&db.DB{Pool: f.pool}).CampaignLeadCounts(t.Context(), f.org.String(), f.campaign.String())
		if xerr != nil || counts.Paused != 1 {
			t.Fatalf("final-step lead counts=%+v err=%v", counts, xerr)
		}
	}
	assertCounts()
	if lifted, err := progress.ResumeLead(t.Context(), f.campaign, f.contactID); err != nil || !lifted {
		t.Fatalf("lift ordinary pause: %t %v", lifted, err)
	}
	f.assertSQL(t, `SELECT paused_at IS NULL AND `+recoveryPausedLeadSQL("cl")+` FROM campaign_leads cl WHERE campaign_id=$1 AND contact_id=$2`, f.campaign, f.contactID)
	assertCounts()
}

func TestLiveCampaignUnknownIsolationFailedEvidenceRollsBackAtomically(t *testing.T) {
	f := newCampaignIsolationFixture(t)
	f.isolate(t)
	progress := NewCampaignProgressRepository(f.pool)
	result := models.SendEmailResult{TaskID: f.request.TaskID, Error: &models.EmailSendError{Code: "RECIPIENT_REJECTED"}, SentAt: time.Now().UTC()}
	if err := f.r.FinishOutbound(t.Context(), result.TaskID, f.sender, f.worker, result); err != nil {
		t.Fatal(err)
	}
	sentinel := errors.New("result effect unavailable")
	err := f.r.ApplySendResult(t.Context(), result, func(ctx context.Context) error {
		if _, _, _, err := progress.RecordSendFailure(ctx, f.campaign, f.contactID, f.step, "Confirmed rejection"); err != nil {
			return err
		}
		return sentinel
	})
	if !errors.Is(err, sentinel) {
		t.Fatalf("failed effect: %v", err)
	}
	f.assertSQL(t, `SELECT t.status='cancelled' AND t.send_result_state='unknown' AND t.send_result_applied_at IS NULL AND cl.send_outcome_pending_at IS NOT NULL AND p.dispatched_at IS NOT NULL AND p.send_attempts=0 FROM tasks t JOIN campaign_contact_progress p ON p.dispatch_task_id=t.id JOIN campaign_leads cl ON cl.campaign_id=p.campaign_id AND cl.contact_id=p.contact_id WHERE t.id=$1`, result.TaskID)
	calls := 0
	for range 2 {
		err := f.r.ApplySendResult(t.Context(), result, func(ctx context.Context) error {
			calls++
			_, _, rolled, err := progress.RecordSendFailure(ctx, f.campaign, f.contactID, f.step, "Confirmed rejection")
			if err == nil && !rolled {
				return errors.New("failure reservation did not roll back")
			}
			return err
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	if calls != 1 {
		t.Fatalf("failure effects repeated: %d", calls)
	}
	f.assertSQL(t, `SELECT status='cancelled' AND send_result_state='failed' AND send_result_applied_at IS NOT NULL AND send_released_at IS NOT NULL FROM tasks WHERE id=$1`, result.TaskID)
	f.assertSQL(t, `SELECT cl.send_outcome_pending_at IS NULL AND p.dispatched_at IS NULL AND p.dispatch_task_id IS NULL AND p.send_attempts=1 FROM campaign_contact_progress p JOIN campaign_leads cl ON cl.campaign_id=p.campaign_id AND cl.contact_id=p.contact_id WHERE p.campaign_id=$1 AND p.contact_id=$2 AND p.sequence_id=$3`, f.campaign, f.contactID, f.step)
}

func TestLiveCampaignUnknownIsolationSerializesWithDefinitiveResult(t *testing.T) {
	for i := range 10 {
		t.Run(fmt.Sprint(i), func(t *testing.T) {
			f := newCampaignIsolationFixture(t)
			progress := NewCampaignProgressRepository(f.pool)
			result := models.SendEmailResult{TaskID: f.request.TaskID, Success: true, MessageID: "<race@example.test>", SentAt: time.Now().UTC()}
			start := make(chan struct{})
			done := make(chan error, 2)
			go func() {
				<-start
				_, err := f.r.IsolateUnknownCampaignSends(t.Context(), time.Now().Add(-10*time.Minute), 100)
				done <- err
			}()
			go func() {
				<-start
				if err := f.r.FinishOutbound(t.Context(), result.TaskID, f.sender, f.worker, result); err != nil {
					done <- err
					return
				}
				done <- f.r.ApplySendResult(t.Context(), result, func(ctx context.Context) error {
					return progress.RecordEmailSent(ctx, f.campaign, f.contactID, f.step)
				})
			}()
			close(start)
			for range 2 {
				if err := <-done; err != nil {
					t.Error(err)
				}
			}
			if t.Failed() {
				return
			}
			f.assertSQL(t, `SELECT t.send_result_state='sent' AND t.send_result_applied_at IS NOT NULL AND p.sent_at IS NOT NULL AND cl.send_outcome_pending_at IS NULL AND NOT ea.send_recovery_hold AND ((t.campaign_send_isolated_at IS NOT NULL AND t.status='cancelled') OR (t.campaign_send_isolated_at IS NULL AND t.status='completed')) FROM tasks t JOIN campaign_contact_progress p ON p.dispatch_task_id=t.id JOIN campaign_leads cl ON cl.campaign_id=p.campaign_id AND cl.contact_id=p.contact_id JOIN email_accounts ea ON ea.id=t.email_account_id WHERE t.id=$1`, result.TaskID)
			state, err := f.r.BeginOutbound(t.Context(), result.TaskID, f.sender, f.worker, f.nonce)
			if err != nil || (state.State != "denied" && state.State != "finished") {
				t.Fatalf("original command regained execution: %+v %v", state, err)
			}
			if state.State == "finished" && (state.Result == nil || !state.Result.Success) {
				t.Fatalf("finished command lost definitive result: %+v", state)
			}
		})
	}
}

func TestLiveCampaignUnknownIsolationDownMigrationPreservesEvidence(t *testing.T) {
	f := newCampaignIsolationFixture(t)
	f.isolate(t)
	migration, err := os.ReadFile("../infrastructure/db/migrations/000331_campaign_unknown_send_isolation.down.sql")
	if err != nil {
		t.Fatal(err)
	}
	tx, err := f.pool.Begin(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(t.Context())
	if _, err := tx.Exec(t.Context(), string(migration)); err == nil {
		t.Fatal("down migration discarded permanent isolation evidence")
	}
	if err := tx.Rollback(t.Context()); err != nil {
		t.Fatal(err)
	}
	f.assertSQL(t, `SELECT t.campaign_send_isolated_at IS NOT NULL AND t.status='cancelled' AND cl.send_outcome_pending_at IS NOT NULL FROM tasks t JOIN campaign_contact_progress p ON p.dispatch_task_id=t.id JOIN campaign_leads cl ON cl.campaign_id=p.campaign_id AND cl.contact_id=p.contact_id WHERE t.id=$1`, f.request.TaskID)
}

func TestLiveCampaignUnknownIsolationAfterCampaignDeletion(t *testing.T) {
	f := newCampaignIsolationFixture(t)
	f.exec(t, `DELETE FROM campaigns WHERE id=$1`, f.campaign)
	f.assertSQL(t, `SELECT campaign_id IS NULL FROM campaign_tasks WHERE task_id=$1`, f.request.TaskID)
	f.isolate(t)
	f.assertSQL(t, `SELECT t.campaign_send_isolated_at IS NOT NULL AND t.status='cancelled' AND t.send_result_state='unknown' AND t.send_result_applied_at IS NULL AND t.send_released_at IS NULL AND t.send_executor_nonce=$2 AND t.send_executor_started_at IS NOT NULL AND NOT ea.send_recovery_hold AND EXISTS(SELECT 1 FROM outbound_attempts a WHERE a.task_id=t.id AND a.nonce=$2) FROM tasks t JOIN email_accounts ea ON ea.id=t.email_account_id WHERE t.id=$1`, f.request.TaskID, f.nonce)
	state, err := f.r.BeginOutbound(t.Context(), f.request.TaskID, f.sender, f.worker, f.nonce)
	if err != nil || state.State != "denied" {
		t.Fatalf("deleted campaign command regained execution: %+v %v", state, err)
	}
}

func TestLiveCampaignUnknownIsolationAfterSequenceOrContactDeletion(t *testing.T) {
	for _, deleted := range []string{"sequence", "contact"} {
		t.Run(deleted, func(t *testing.T) {
			f := newCampaignIsolationFixture(t)
			if deleted == "sequence" {
				f.exec(t, `DELETE FROM sequences WHERE id=$1`, f.step)
			} else {
				f.exec(t, `DELETE FROM contacts WHERE id=$1`, f.contactID)
			}
			f.isolate(t)
			f.assertSQL(t, `SELECT t.campaign_send_isolated_at IS NOT NULL AND t.status='cancelled' AND t.send_result_state='unknown' AND t.send_result_applied_at IS NULL AND t.send_released_at IS NULL AND t.send_executor_nonce=$2 AND t.send_executor_started_at IS NOT NULL AND NOT ea.send_recovery_hold FROM tasks t JOIN email_accounts ea ON ea.id=t.email_account_id WHERE t.id=$1`, f.request.TaskID, f.nonce)
			state, err := f.r.BeginOutbound(t.Context(), f.request.TaskID, f.sender, f.worker, f.nonce)
			if err != nil || state.State != "denied" {
				t.Fatalf("deleted lineage command regained execution: %+v %v", state, err)
			}
			if deleted == "sequence" {
				f.assertSQL(t, `SELECT send_outcome_pending_at IS NOT NULL FROM campaign_leads WHERE campaign_id=$1 AND contact_id=$2`, f.campaign, f.contactID)
				held, err := NewCampaignProgressRepository(f.pool).CountHeldLeads(t.Context(), f.campaign)
				if err != nil || held != 1 {
					t.Fatalf("surviving lead lost pause: %d %v", held, err)
				}
			}
		})
	}
}

func TestLiveCampaignUnknownIsolationAfterLeadRemoval(t *testing.T) {
	f := newCampaignIsolationFixture(t)
	f.exec(t, `DELETE FROM campaign_leads WHERE campaign_id=$1 AND contact_id=$2`, f.campaign, f.contactID)
	f.isolate(t)
	f.assertSQL(t, `SELECT t.status='cancelled' AND t.send_result_state='unknown' AND t.campaign_send_isolated_at IS NOT NULL AND NOT ea.send_recovery_hold FROM tasks t JOIN email_accounts ea ON ea.id=t.email_account_id WHERE t.id=$1`, f.request.TaskID)
}

func TestLiveCampaignUnknownIsolationMultipleResultsClearLeadTogether(t *testing.T) {
	f := newCampaignIsolationFixture(t)
	f.isolate(t)
	other := uuid.New()
	f.exec(t, `INSERT INTO tasks(id,email_account_id,task_type,status,message_id,send_result_state,campaign_send_isolated_at) VALUES($1,$2,'campaign','cancelled','','unknown',NOW())`, other, f.recipient)
	f.exec(t, `INSERT INTO campaign_tasks(task_id,campaign_id,contact_id,sequence_id) VALUES($1,$2,$3,$4)`, other, f.campaign, f.contactID, f.step)
	f.exec(t, `UPDATE email_accounts SET send_recovery_hold=false,send_recovery_reason=NULL,send_recovery_task_id=NULL WHERE id=$1`, f.recipient)
	first := models.SendEmailResult{TaskID: f.request.TaskID, Success: true}
	if err := f.r.ApplySendResult(t.Context(), first, func(context.Context) error { return nil }); err != nil {
		t.Fatal(err)
	}
	f.assertSQL(t, `SELECT send_outcome_pending_at IS NOT NULL FROM campaign_leads WHERE campaign_id=$1 AND contact_id=$2`, f.campaign, f.contactID)
	// Restore only the test result markers to exercise simultaneous definitive outcomes.
	f.exec(t, `UPDATE tasks SET send_result_state='unknown',send_result_applied_at=NULL WHERE id=$1`, f.request.TaskID)
	start, done := make(chan struct{}), make(chan error, 2)
	for _, id := range []uuid.UUID{f.request.TaskID, other} {
		go func(id uuid.UUID) {
			<-start
			done <- f.r.ApplySendResult(t.Context(), models.SendEmailResult{TaskID: id, Success: true}, func(context.Context) error { return nil })
		}(id)
	}
	close(start)
	for range 2 {
		if err := <-done; err != nil {
			t.Error(err)
		}
	}
	if t.Failed() {
		return
	}
	f.assertSQL(t, `SELECT send_outcome_pending_at IS NULL FROM campaign_leads WHERE campaign_id=$1 AND contact_id=$2`, f.campaign, f.contactID)
	f.assertSQL(t, `SELECT COUNT(*)=2 AND bool_and(status='cancelled' AND send_result_state='sent' AND send_result_applied_at IS NOT NULL) FROM tasks WHERE id=ANY($1)`, []uuid.UUID{f.request.TaskID, other})
}
