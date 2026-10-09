package repository

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/warmbly/warmbly/internal/models"
)

type campaignAdmissionFixture struct {
	*poolLinkFixture
	r                      *taskRepository
	worker, campaign, step uuid.UUID
	historyContact         uuid.UUID
}

func newCampaignAdmissionFixture(t *testing.T, limit int) *campaignAdmissionFixture {
	t.Helper()
	base, r := lineageFixture(t)
	f := &campaignAdmissionFixture{poolLinkFixture: base, r: r, worker: uuid.New(), campaign: uuid.New(), step: uuid.New()}
	f.exec(t, `INSERT INTO fleet_nodes(id,role,active,last_seen_at,warmup_send_protocol) VALUES($1,'worker',true,NOW(),2)`, f.worker)
	f.exec(t, `INSERT INTO workers(id) VALUES($1)`, f.worker)
	t.Cleanup(func() {
		f.exec(t, `DELETE FROM campaigns WHERE id=$1`, f.campaign)
		f.exec(t, `DELETE FROM contacts WHERE organization_id=$1`, f.org)
		f.exec(t, `DELETE FROM fleet_nodes WHERE id=$1`, f.worker)
	})
	f.exec(t, `UPDATE email_accounts SET worker_id=$1,campaign_limit=5000,shared_daily_limit=10000,rolling_recipient_limit=10000 WHERE organization_id=$2`, f.worker, f.org)
	f.exec(t, `INSERT INTO campaigns(id,user_id,organization_id,name,description,status,daily_limit,timezone,days,created_at,updated_at) VALUES($1,$2,$3,'Admission','','active',$4,'Pacific/Honolulu',127,NOW(),NOW())`, f.campaign, f.user, f.org, limit)
	f.exec(t, `INSERT INTO sequences(id,campaign_id,organization_id,name,subject,body_plain,body_html,wait_after,position) VALUES($1,$2,$3,'Step','Test','Text','',0,1)`, f.step, f.campaign, f.org)
	f.historyContact = f.contact(t)
	return f
}

func (f *campaignAdmissionFixture) exec(t *testing.T, query string, args ...any) {
	t.Helper()
	if _, err := f.pool.Exec(context.Background(), query, args...); err != nil {
		t.Fatal(err)
	}
}

func (f *campaignAdmissionFixture) contact(t *testing.T) uuid.UUID {
	t.Helper()
	id := uuid.New()
	f.exec(t, `INSERT INTO contacts(id,user_id,organization_id,email,first_name,last_name,company,phone,custom_fields) VALUES($1,$2,$3,$4,'Test','','','','{}')`, id, f.user, f.org, id.String()+"@example.test")
	f.exec(t, `INSERT INTO campaign_leads(campaign_id,contact_id,position) VALUES($1,$2,0)`, f.campaign, id)
	return id
}

func (f *campaignAdmissionFixture) request(t *testing.T, mailbox uuid.UUID) (OutboundReservation, uuid.UUID) {
	t.Helper()
	id, contact := uuid.New(), f.contact(t)
	f.exec(t, `INSERT INTO tasks(id,task_type,email_account_id,status,message_id) VALUES($1,'campaign',$2,'active','')`, id, mailbox)
	f.exec(t, `INSERT INTO campaign_tasks(task_id,campaign_id,sequence_id,contact_id) VALUES($1,$2,$3,$4)`, id, f.campaign, f.step, contact)
	claimed, err := NewCampaignProgressRepository(f.pool).ReserveSend(t.Context(), f.campaign, contact, f.step, id, mailbox, true)
	if err != nil || !claimed {
		t.Fatalf("reserve lead: %v %v", claimed, err)
	}
	return OutboundReservation{TaskID: id, MailboxID: mailbox, OrganizationID: f.org, WorkerID: f.worker, Provider: models.InboxProviderSMTPIMAP, Recipients: []string{contact.String() + "@example.test"}}, contact
}

func (f *campaignAdmissionFixture) history(t *testing.T, mailbox uuid.UUID, count, days int, message string, wakeup bool) []uuid.UUID {
	t.Helper()
	var step any = f.step
	if wakeup {
		step = nil
	}
	rows, err := f.pool.Query(t.Context(), `WITH history AS (
	 INSERT INTO tasks(id,task_type,email_account_id,status,message_id,completed_at)
	 SELECT gen_random_uuid(),'campaign',$1,'completed',$4,NOW()+$3::int*INTERVAL '1 day' FROM generate_series(1,$2::int) RETURNING id)
	 INSERT INTO campaign_tasks(task_id,campaign_id,sequence_id,contact_id)
	 SELECT id,$5,$6,$7 FROM history RETURNING task_id`, mailbox, count, days, message, f.campaign, step, f.historyContact)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var ids []uuid.UUID
	for rows.Next() {
		var id uuid.UUID
		if err := rows.Scan(&id); err != nil {
			t.Fatal(err)
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return ids
}

func TestLiveCampaignAdmissionPerMailbox(t *testing.T) {
	f := newCampaignAdmissionFixture(t, 50)
	ids := f.history(t, f.sender, 50, 0, "<sent@example.test>", false)
	f.exec(t, `INSERT INTO contacts(id,user_id,organization_id,email,first_name,last_name,company,phone,custom_fields)
	 SELECT id,$2,$3,id::text||'@example.test','Sent','','','','{}' FROM tasks WHERE id=ANY($1)`, ids, f.user, f.org)
	f.exec(t, `INSERT INTO campaign_leads(campaign_id,contact_id,position) SELECT $2,id,0 FROM tasks WHERE id=ANY($1)`, ids, f.campaign)
	f.exec(t, `UPDATE campaign_tasks SET contact_id=task_id WHERE task_id=ANY($1)`, ids)
	f.exec(t, `INSERT INTO campaign_contact_progress(campaign_id,contact_id,sequence_id,dispatch_task_id,dispatched_at,sent_at)
	 SELECT $2,id,$3,id,completed_at,completed_at FROM tasks WHERE id=ANY($1)`, ids, f.campaign, f.step)
	counts, err := f.r.CountCampaignSendsTodayBySender(t.Context(), f.campaign)
	if err != nil || counts[f.sender] != 50 || counts[f.recipient] != 0 {
		t.Fatalf("scheduler counts: %v %v", counts, err)
	}
	atCap, _ := f.request(t, f.sender)
	if _, err := f.r.ReserveOutbound(t.Context(), atCap); !errors.Is(err, ErrCampaignDailyLimit) || !errors.Is(err, ErrSendAdmissionDenied) {
		t.Fatalf("full mailbox: %v", err)
	}
	fresh, _ := f.request(t, f.recipient)
	nonce, err := f.r.ReserveOutbound(t.Context(), fresh)
	if err != nil {
		t.Fatalf("unused mailbox after 50 sends on another: %v", err)
	}
	state, err := f.r.BeginOutbound(t.Context(), fresh.TaskID, fresh.MailboxID, fresh.WorkerID, nonce)
	if err != nil || state.State != "execute" {
		t.Fatalf("second admission: %+v %v", state, err)
	}
	counts, err = f.r.CountCampaignSendsTodayBySender(t.Context(), f.campaign)
	if err != nil || counts[f.sender] != 50 || counts[f.recipient] != 1 {
		t.Fatalf("reservation counted once: %v %v", counts, err)
	}
}

func TestLiveCampaignAdmissionConfiguredLimitsAndProvenance(t *testing.T) {
	for _, tc := range []struct {
		name           string
		limit, sends   int
		days           int
		wakeup, denied bool
		progressOnly   bool
	}{
		{name: "configured seven", limit: 7, sends: 7, denied: true},
		{name: "zero unlimited", limit: 0, sends: 50},
		{name: "negative unlimited", limit: -1, sends: 50},
		{name: "previous day", limit: 1, sends: 50, days: -1},
		{name: "wake ups", limit: 1, sends: 50, wakeup: true},
		{name: "dispatch provenance without message id", limit: 1, sends: 1, progressOnly: true, denied: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newCampaignAdmissionFixture(t, tc.limit)
			message := "<legacy@example.test>"
			if tc.wakeup || tc.progressOnly {
				message = ""
			}
			ids := f.history(t, f.sender, tc.sends, tc.days, message, tc.wakeup)
			if tc.progressOnly {
				f.exec(t, `INSERT INTO campaign_contact_progress(campaign_id,contact_id,sequence_id,dispatch_task_id,dispatched_at,sent_at) VALUES($1,$2,$3,$4,NOW(),NOW())`, f.campaign, f.historyContact, f.step, ids[0])
			}
			request, _ := f.request(t, f.sender)
			nonce, err := f.r.ReserveOutbound(t.Context(), request)
			if tc.denied {
				if !errors.Is(err, ErrCampaignDailyLimit) {
					t.Fatalf("want campaign daily limit, got %v", err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			state, err := f.r.BeginOutbound(t.Context(), request.TaskID, request.MailboxID, request.WorkerID, nonce)
			if err != nil || state.State != "execute" {
				t.Fatalf("own reservation spent the last slot twice: %+v %v", state, err)
			}
		})
	}
}

func TestLiveCampaignAdmissionUsesUTCBudget(t *testing.T) {
	f := newCampaignAdmissionFixture(t, 1)
	ids := f.history(t, f.sender, 1, 0, "<legacy@example.test>", false)
	f.exec(t, `UPDATE tasks SET completed_at=(date_trunc('day',NOW() AT TIME ZONE 'UTC') AT TIME ZONE 'UTC') WHERE id=$1`, ids[0])
	config := f.pool.Config()
	config.ConnConfig.RuntimeParams["timezone"] = "Pacific/Honolulu"
	pool, err := pgxpool.NewWithConfig(t.Context(), config)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	r := NewTaskRepository(pool).(*taskRepository)
	counts, err := r.CountCampaignSendsTodayBySender(t.Context(), f.campaign)
	if err != nil || counts[f.sender] != 1 {
		t.Fatalf("scheduler UTC count: %v %v", counts, err)
	}
	request, _ := f.request(t, f.sender)
	if _, err := r.ReserveOutbound(t.Context(), request); !errors.Is(err, ErrCampaignDailyLimit) {
		t.Fatalf("admission differs from scheduler in non-UTC session: %v", err)
	}
}

func TestLiveCampaignAdmissionPlacementUTCBudget(t *testing.T) {
	f := newCampaignAdmissionFixture(t, 50)
	config := f.pool.Config()
	config.MaxConns = 1
	zone := "Etc/GMT+12"
	if time.Now().UTC().Hour() >= 12 {
		zone = "Etc/GMT-14"
	}
	config.ConnConfig.RuntimeParams["timezone"] = zone
	pool, err := pgxpool.NewWithConfig(t.Context(), config)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	r := &taskRepository{db: pool}
	f.exec(t, `WITH day AS (SELECT date_trunc('day',NOW() AT TIME ZONE 'UTC') AT TIME ZONE 'UTC' AS start)
	 INSERT INTO tasks(id,task_type,email_account_id,status,message_id,completed_at,scheduled_at)
	 SELECT gen_random_uuid(),'placement'::task_type,$1::uuid,'completed'::task_status,'',start-INTERVAL '1 minute',start-INTERVAL '1 minute' FROM day
	 UNION ALL SELECT gen_random_uuid(),'placement',$1,'completed','',start+INTERVAL '1 minute',start+INTERVAL '1 minute' FROM day
	 UNION ALL SELECT gen_random_uuid(),'placement',$1,'pending','',NULL,start+INTERVAL '1 hour' FROM day
	 UNION ALL SELECT gen_random_uuid(),'placement',$1,'pending','',NULL,start+INTERVAL '25 hours' FROM day`, f.sender)
	count, err := r.CountCampaignEmailsSentToday(t.Context(), f.sender)
	if err != nil || count != 2 {
		t.Fatalf("scalar UTC placement count: %d %v", count, err)
	}
	counts, err := r.CountCampaignEmailsSentTodayByAccounts(t.Context(), []uuid.UUID{f.sender})
	if err != nil || counts[f.sender] != 2 {
		t.Fatalf("batched UTC placement count: %v %v", counts, err)
	}
}

func TestLiveCampaignAdmissionPreservesMailboxControls(t *testing.T) {
	for _, column := range []string{"campaign_limit", "shared_daily_limit", "rolling_recipient_limit"} {
		t.Run(column, func(t *testing.T) {
			f := newCampaignAdmissionFixture(t, 0)
			f.history(t, f.sender, 1, 0, "<legacy@example.test>", false)
			f.exec(t, `UPDATE email_accounts SET `+column+`=1 WHERE id=$1`, f.sender)
			request, _ := f.request(t, f.sender)
			if _, err := f.r.ReserveOutbound(t.Context(), request); !errors.Is(err, ErrSendAdmissionDenied) || errors.Is(err, ErrCampaignDailyLimit) {
				t.Fatalf("mailbox control bypassed or mislabeled: %v", err)
			}
		})
	}
}

func TestLiveCampaignAdmissionConcurrentLastSlotAndCancellation(t *testing.T) {
	f := newCampaignAdmissionFixture(t, 7)
	f.history(t, f.sender, 6, 0, "<legacy@example.test>", false)
	requests := make([]OutboundReservation, 12)
	for i := range requests {
		requests[i], _ = f.request(t, f.sender)
	}
	start := make(chan struct{})
	wins := make(chan OutboundReservation, len(requests))
	errCh := make(chan error, len(requests))
	var wg sync.WaitGroup
	for _, request := range requests {
		wg.Go(func() {
			<-start
			if _, err := f.r.ReserveOutbound(t.Context(), request); err == nil {
				wins <- request
			} else if !errors.Is(err, ErrSendAdmissionDenied) {
				errCh <- err
			}
		})
	}
	close(start)
	wg.Wait()
	close(wins)
	close(errCh)
	for err := range errCh {
		t.Fatal(err)
	}
	if len(wins) != 1 {
		t.Fatalf("admitted %d sends into one slot", len(wins))
	}
	winner := <-wins
	var nonce uuid.UUID
	if err := f.pool.QueryRow(t.Context(), `SELECT send_executor_nonce FROM tasks WHERE id=$1`, winner.TaskID).Scan(&nonce); err != nil {
		t.Fatal(err)
	}
	// A changed cap is rechecked before any command executes.
	f.exec(t, `UPDATE campaigns SET daily_limit=6 WHERE id=$1`, f.campaign)
	state, err := f.r.BeginOutbound(t.Context(), winner.TaskID, winner.MailboxID, winner.WorkerID, nonce)
	if err != nil || state.State != "denied" {
		t.Fatalf("worker did not recheck campaign capacity: %+v %v", state, err)
	}
	if err := f.r.CancelOutbound(t.Context(), winner.TaskID, winner.MailboxID, winner.WorkerID, nonce); err != nil {
		t.Fatal(err)
	}
	var reason, code string
	if err := f.pool.QueryRow(t.Context(), `SELECT message FROM task_failures WHERE task_id=$1`, winner.TaskID).Scan(&reason); err != nil || reason != ErrCampaignDailyLimit.Error() {
		t.Fatalf("worker denial lost its task failure reason: %s %v", reason, err)
	}
	if err := f.pool.QueryRow(t.Context(), `SELECT metadata->>'code' FROM campaign_logs WHERE campaign_id=$1 AND metadata->>'task_id'=$2`, f.campaign, winner.TaskID.String()).Scan(&code); err != nil || code != "CAMPAIGN_DAILY_LIMIT_REACHED" {
		t.Fatalf("worker denial lost its capacity diagnostic: %s %v", code, err)
	}
	counts, err := f.r.CountCampaignSendsTodayBySender(t.Context(), f.campaign)
	if err != nil || counts[f.sender] != 6 {
		t.Fatalf("cancelled reservation still consumes the cap: %v %v", counts, err)
	}
	for _, request := range requests {
		var reserved, held bool
		if err := f.pool.QueryRow(t.Context(), `SELECT send_reserved_at IS NOT NULL AND send_released_at IS NULL,ea.send_recovery_hold FROM tasks t JOIN email_accounts ea ON ea.id=t.email_account_id WHERE t.id=$1`, request.TaskID).Scan(&reserved, &held); err != nil || reserved || held {
			t.Fatalf("reservation leaked after cancellation: %v %v %v", reserved, held, err)
		}
	}
}
