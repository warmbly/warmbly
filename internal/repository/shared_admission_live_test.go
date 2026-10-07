package repository

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/warmbly/warmbly/internal/models"
)

func TestLiveSharedAdmissionMixedLanesRetainsUnknownAndDoesNotDoubleCountCampaign(t *testing.T) {
	for _, first := range []string{"campaign", "warmup", "placement", "email"} {
		t.Run(first, func(t *testing.T) {
			f, r := lineageFixture(t)
			ctx := t.Context()
			worker, campaign, step, contact := uuid.New(), uuid.New(), uuid.New(), uuid.New()
			address := "pl-" + f.recipient.String()[:8] + "@test.local"
			exec := func(sql string, args ...any) {
				t.Helper()
				if _, err := f.pool.Exec(ctx, sql, args...); err != nil {
					t.Fatal(err)
				}
			}
			exec(`INSERT INTO fleet_nodes(id,role,active,last_seen_at,warmup_send_protocol)VALUES($1,'worker',true,NOW(),2)`, worker)
			exec(`INSERT INTO workers(id)VALUES($1)`, worker)
			t.Cleanup(func() { _, _ = f.pool.Exec(context.Background(), `DELETE FROM fleet_nodes WHERE id=$1`, worker) })
			exec(`UPDATE email_accounts SET worker_id=$1,shared_daily_limit=2,rolling_recipient_limit=3,warmup_max=50 WHERE id=$2`, worker, f.sender)
			exec(`INSERT INTO warmup_pool_participants(pool_id,email_account_id)SELECT wp.id,ea.id FROM warmup_pools wp CROSS JOIN email_accounts ea WHERE wp.pool_type='free' AND ea.id IN($1,$2) ON CONFLICT DO NOTHING`, f.sender, f.recipient)
			exec(`INSERT INTO campaigns(id,user_id,organization_id,name,description,status,daily_limit,timezone,days,created_at,updated_at)VALUES($1,$2,$3,'Shared','','active',50,'UTC',127,NOW(),NOW())`, campaign, f.user, f.org)
			exec(`INSERT INTO sequences(id,campaign_id,organization_id,name,subject,body_plain,body_html,wait_after,position)VALUES($1,$2,$3,'Step','Diagnostic','Text','',0,1)`, step, campaign, f.org)
			exec(`INSERT INTO contacts(id,user_id,organization_id,email,first_name,last_name,company,phone,custom_fields)VALUES($1,$2,$3,$4,'Test','','','','{}')`, contact, f.user, f.org, address)
			t.Cleanup(func() {
				c := context.Background()
				_, _ = f.pool.Exec(c, `DELETE FROM campaigns WHERE id=$1`, campaign)
				_, _ = f.pool.Exec(c, `DELETE FROM contacts WHERE id=$1`, contact)
			})
			newTask := func(lane string) OutboundReservation {
				t.Helper()
				id := uuid.New()
				exec(`INSERT INTO tasks(id,task_type,email_account_id,status,message_id)VALUES($1,$2,$3,'active','')`, id, lane, f.sender)
				switch lane {
				case "campaign":
					exec(`INSERT INTO campaign_tasks(task_id,campaign_id,sequence_id,contact_id)VALUES($1,$2,$3,$4)`, id, campaign, step, contact)
					exec(`INSERT INTO campaign_contact_progress(campaign_id,contact_id,sequence_id,dispatch_task_id,dispatched_at)VALUES($1,$2,$3,$4,NOW()) ON CONFLICT(campaign_id,contact_id,sequence_id)DO UPDATE SET dispatch_task_id=$4,dispatched_at=NOW()`, campaign, contact, step, id)
				case "warmup":
					exec(`INSERT INTO warmup_tasks(task_id,target_account_id,lineage_version,subject,scenario_version,rendering_version,max_turns)VALUES($1,$2,1,'Pinned diagnostic','diagnostic-v1','canonical-v1',3)`, id, f.recipient)
					exec(`INSERT INTO warmup_tokens(token,task_id,sender_account_id,recipient_account_id,subject)VALUES($1,$2,$3,$4,'Pinned diagnostic')`, uuid.New(), id, f.sender, f.recipient)
				}
				return OutboundReservation{TaskID: id, MailboxID: f.sender, OrganizationID: f.org, WorkerID: worker, Provider: models.InboxProviderSMTPIMAP, Recipients: []string{address}}
			}
			reserve := func(in OutboundReservation, lane string) (uuid.UUID, error) {
				if lane == "warmup" {
					if _, err := r.AuthorizeWarmupDispatch(ctx, in.TaskID, in.MailboxID, in.WorkerID); err != nil {
						return uuid.Nil, err
					}
				}
				return r.ReserveOutbound(ctx, in)
			}
			in := newTask(first)
			nonce, err := reserve(in, first)
			if err != nil {
				t.Fatal(err)
			}
			state, err := r.BeginOutbound(ctx, in.TaskID, in.MailboxID, in.WorkerID, nonce)
			if err != nil || state.State != "execute" {
				t.Fatalf("execution: %+v %v", state, err)
			}
			result := models.SendEmailResult{TaskID: in.TaskID, Success: true, MessageID: "<confirmed@example.test>"}
			if err = r.FinishOutbound(ctx, in.TaskID, in.MailboxID, in.WorkerID, result); err != nil {
				t.Fatal(err)
			}
			if err = r.ApplySendResult(ctx, result, func(context.Context) error { return nil }); err != nil {
				t.Fatal(err)
			}
			var count int
			if err = f.pool.QueryRow(ctx, `SELECT COUNT(*) FROM tasks WHERE email_account_id=$1 AND send_reserved_at IS NOT NULL`, f.sender).Scan(&count); err != nil || count != 1 {
				t.Fatal("campaign reservation was counted twice", count, err)
			}
			var requests []OutboundReservation
			var lanes []string
			for i := 0; i < 12; i++ {
				lane := []string{"campaign", "warmup", "placement", "email"}[i%4]
				requests = append(requests, newTask(lane))
				lanes = append(lanes, lane)
			}
			start := make(chan struct{})
			wins := make(chan OutboundReservation, 12)
			errs := make(chan error, 12)
			var wg sync.WaitGroup
			for i, request := range requests {
				wg.Add(1)
				go func(in OutboundReservation, lane string) {
					defer wg.Done()
					<-start
					if _, err := reserve(in, lane); err == nil {
						wins <- in
					} else if !errors.Is(err, ErrSendAdmissionDenied) && !errors.Is(err, pgx.ErrNoRows) {
						errs <- err
					}
				}(request, lanes[i])
			}
			close(start)
			wg.Wait()
			close(wins)
			close(errs)
			for err := range errs {
				t.Fatalf("concurrent SQL: %v", err)
			}
			if len(wins) != 1 {
				t.Fatalf("mixed lanes admitted %d rather than one remaining slot", len(wins))
			}
			winner := <-wins
			if err = r.ApplySendResult(ctx, models.SendEmailResult{TaskID: winner.TaskID}, func(context.Context) error { return errors.New("unknown must not reconcile") }); err != nil {
				t.Fatal(err)
			}
			admission, err := r.GetSendAdmission(ctx, f.org, f.sender, models.InboxProviderSMTPIMAP, time.Now())
			if err != nil || admission.Allowed || !admission.RecoveryHold {
				t.Fatal("unknown reservation lost its hold", admission, err)
			}
			if _, err = r.ReserveOutbound(ctx, newTask("email")); !errors.Is(err, ErrSendAdmissionDenied) {
				t.Fatal("unknown admitted another lane", err)
			}
			if err = f.pool.QueryRow(ctx, `SELECT COUNT(*) FROM tasks WHERE email_account_id=$1 AND send_reserved_at IS NOT NULL AND send_released_at IS NULL`, f.sender).Scan(&count); err != nil || count != 2 {
				t.Fatal("unknown capacity was refunded", count, err)
			}
			state, err = r.InspectOutbound(ctx, in.TaskID, in.MailboxID, in.WorkerID)
			if err != nil || state.State != "finished" {
				t.Fatal("durable replay unavailable", state, err)
			}
		})
	}
}
