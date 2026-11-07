package orgtransfer

import (
	"bytes"
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/repository"
)

func TestLiveArchiveCampaignIsolationSurvivesSelectiveRoundTrip(t *testing.T) {
	for _, sending := range []bool{false, true} {
		name := "campaigns_without_sending"
		if sending {
			name = "campaigns_with_sending"
		}
		t.Run(name, func(t *testing.T) {
			svc, pool := liveImportService(t)
			f := newTenantFixture(t, pool)
			ctx := t.Context()
			mailbox, task, step, nonce := uuid.New(), uuid.New(), uuid.New(), uuid.New()
			exec := func(query string, args ...any) {
				t.Helper()
				if _, err := pool.Exec(ctx, query, args...); err != nil {
					t.Fatal(err)
				}
			}
			exec(`INSERT INTO email_accounts(id,user_id,organization_id,email,name,signature_plain,signature_html,provider) VALUES($1,$2,$3,$4,'Archive','','','smtp_imap')`, mailbox, f.owner, f.org, mailbox.String()+"@example.test")
			t.Cleanup(func() { _, _ = pool.Exec(context.Background(), `DELETE FROM email_accounts WHERE id=$1`, mailbox) })
			exec(`INSERT INTO sequences(id,campaign_id,organization_id,name,subject,body_plain,body_html,wait_after,position) VALUES($1,$2,$3,'Step','Test','Text','',0,1)`, step, f.campaign, f.org)
			exec(`INSERT INTO campaign_leads(campaign_id,contact_id,position) VALUES($1,$2,0)`, f.campaign, f.contact)
			exec(`INSERT INTO tasks(id,email_account_id,task_type,status,message_id,send_result_state,send_reserved_at,send_executor_nonce,send_executor_started_at) VALUES($1,$2,'campaign','completed','','unknown',NOW()-INTERVAL '1 hour',$3,NOW()-INTERVAL '1 hour')`, task, mailbox, nonce)
			exec(`INSERT INTO campaign_tasks(task_id,campaign_id,sequence_id,contact_id) VALUES($1,$2,$3,$4)`, task, f.campaign, step, f.contact)
			exec(`INSERT INTO campaign_contact_progress(campaign_id,contact_id,sequence_id,dispatch_task_id,dispatched_at) VALUES($1,$2,$3,$4,NOW()-INTERVAL '1 hour')`, f.campaign, f.contact, step, task)
			at := time.Now().UTC().Truncate(time.Microsecond)
			exec(`UPDATE tasks SET status='cancelled',campaign_send_isolated_at=$2 WHERE id=$1`, task, at)
			exec(`UPDATE campaign_leads SET send_outcome_pending_at=$3 WHERE campaign_id=$1 AND contact_id=$2`, f.campaign, f.contact, at)
			groups := []models.OrgDataGroup{models.OrgDataGroupCore, models.OrgDataGroupContacts, models.OrgDataGroupCampaigns}
			if sending {
				groups = append(groups, models.OrgDataGroupSending)
			}
			var archive bytes.Buffer
			if _, err := svc.ExportTo(ctx, f.org, ExportOptions{Groups: groups}, &archive, nil); err != nil {
				t.Fatal(err)
			}
			exec(`DELETE FROM campaign_contact_progress WHERE campaign_id=$1`, f.campaign)
			exec(`DELETE FROM campaign_leads WHERE campaign_id=$1`, f.campaign)
			exec(`DELETE FROM tasks WHERE id=$1`, task)
			if _, err := svc.ImportFrom(ctx, f.org, bytes.NewReader(archive.Bytes()), ImportOptions{Groups: groups, Conflict: models.OrgImportConflictOverwrite, ActorUserID: f.owner}, nil); err != nil {
				t.Fatal(err)
			}
			var pending time.Time
			if err := pool.QueryRow(ctx, `SELECT send_outcome_pending_at FROM campaign_leads WHERE campaign_id=$1 AND contact_id=$2`, f.campaign, f.contact).Scan(&pending); err != nil || !pending.Equal(at) {
				t.Fatalf("portable pause lost: at=%s err=%v", pending, err)
			}
			held, err := repository.NewCampaignProgressRepository(pool).CountHeldLeads(ctx, f.campaign)
			if err != nil || held != 1 {
				t.Fatalf("imported pause not effective: held=%d err=%v", held, err)
			}
			var exists bool
			if err := pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM tasks WHERE id=$1)`, task).Scan(&exists); err != nil || exists != sending {
				t.Fatalf("sending group boundary: exists=%t err=%v", exists, err)
			}
			if sending {
				var safe bool
				if err := pool.QueryRow(ctx, `SELECT campaign_send_isolated_at=$2 AND status='cancelled' AND send_result_state='unknown' AND send_result_applied_at IS NULL AND send_released_at IS NULL AND send_executor_nonce IS NULL AND send_executor_started_at IS NULL AND send_executor_worker IS NULL AND send_executor_result IS NULL FROM tasks WHERE id=$1`, task, at).Scan(&safe); err != nil || !safe {
					t.Fatalf("isolation or local authority reset lost: safe=%t err=%v", safe, err)
				}
				admission := repository.NewTaskRepository(pool).(repository.OutboundAdmissionRepository)
				state, err := admission.InspectOutbound(ctx, task, mailbox, uuid.New())
				if err != nil || state.State != "denied" {
					t.Fatalf("imported command acquired execution authority: %+v %v", state, err)
				}
			}
		})
	}
}
