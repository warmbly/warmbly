package repository

import (
	"context"
	"errors"
	"net/url"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/warmbly/warmbly/internal/infrastructure/db"
)

func liveCampaignReplayDB(t *testing.T) *pgxpool.Pool {
	t.Helper()
	dsn := os.Getenv("WARMBLY_TEST_DB")
	if dsn == "" {
		t.Skip("WARMBLY_TEST_DB not set")
	}
	u, err := url.Parse(dsn)
	if err != nil || (u.Hostname() != "localhost" && u.Hostname() != "127.0.0.1") {
		t.Fatal("replay fixture requires local PostgreSQL")
	}
	admin, err := pgx.Connect(t.Context(), dsn)
	if err != nil {
		t.Fatal(err)
	}
	name := "campaign_replay_" + uuid.New().String()[:8]
	quoted := pgx.Identifier{name}.Sanitize()
	if _, err := admin.Exec(t.Context(), "CREATE DATABASE "+quoted); err != nil {
		_ = admin.Close(context.Background())
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if _, err := admin.Exec(context.Background(), "DROP DATABASE "+quoted); err != nil {
			t.Error(err)
		}
		_ = admin.Close(context.Background())
	})
	u.Path = "/" + name
	if err := db.RunMigrations(u.String()); err != nil {
		t.Fatal(err)
	}
	pool, err := pgxpool.New(t.Context(), u.String())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	return pool
}

func TestLiveCampaignReplayIntentTransitionsFilterBeforeLimitAndLock(t *testing.T) {
	pool := liveCampaignReplayDB(t)
	f := newWarmupUsageFixture(t, pool)
	r := NewTaskRepository(pool)
	campaign, contact, sequence := uuid.New(), uuid.New(), uuid.New()
	f.exec(`INSERT INTO campaigns(id,user_id,organization_id,name,description,status,days,timezone,created_at,updated_at) VALUES($1,$2,$3,'Replay','','active',127,'UTC',NOW(),NOW())`, campaign, f.user, f.org)
	f.exec(`INSERT INTO contacts(id,user_id,organization_id,email,first_name,last_name,company,phone,custom_fields) VALUES($1,$2,$3,'lead@example.test','','','','','{}')`, contact, f.user, f.org)
	f.exec(`INSERT INTO sequences(id,campaign_id,organization_id,name,subject,body_plain,body_html,wait_after,position) VALUES($1,$2,$3,'Step','Subject','Text','',0,1)`, sequence, campaign, f.org)
	if err := r.UpdateCampaignTaskTracking(t.Context(), uuid.New(), contact, sequence); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatal("missing tracking row accepted as durable intent", err)
	}
	for range 60 {
		id := uuid.New()
		f.exec(`INSERT INTO tasks(id,email_account_id,task_type,status,message_id) VALUES($1,$2,'campaign','dead_lettered','')`, id, f.account)
		f.exec(`INSERT INTO campaign_tasks(task_id,campaign_id) VALUES($1,$2)`, id, campaign)
		f.exec(`INSERT INTO task_dead_letters(task_id,task_type,next_retry_at,payload) VALUES($1,'campaign',NOW()-INTERVAL '2 hours','{"dispatch_intent":"wakeup","safe":true}')`, id)
	}
	newWakeup := func() uuid.UUID {
		t.Helper()
		id := uuid.New()
		created, err := r.CreateTaskWithLock(t.Context(), &Task{ID: id, TaskType: "campaign", Status: "dead_lettered", EmailAccountID: f.account}, &CampaignTask{TaskID: id, CampaignID: &campaign})
		if err != nil || !created {
			t.Fatalf("create wakeup: %t %v", created, err)
		}
		f.exec(`INSERT INTO task_dead_letters(task_id,task_type,next_retry_at,payload,last_error,attempts) VALUES($1,'campaign',NOW()-INTERVAL '1 hour','{"original":true}','retained',1)`, id)
		return id
	}
	send := newWakeup()
	if err := r.UpdateCampaignTaskTracking(t.Context(), send, contact, sequence); err != nil {
		t.Fatal(err)
	}
	f.exec(`DELETE FROM contacts WHERE id=$1`, contact)
	f.exec(`DELETE FROM sequences WHERE id=$1`, sequence)
	f.exec(`UPDATE campaign_tasks SET dispatch_intent='wakeup' WHERE task_id=$1`, send)
	ct, err := r.GetCampaignTask(t.Context(), send)
	if err != nil || ct.DispatchIntent != CampaignDispatchSend || ct.ContactID != nil || ct.SequenceID != nil {
		t.Fatalf("send capability lost after FK cleanup: %+v %v", ct, err)
	}
	wakeup := newWakeup()
	listed, err := NewAdvancedOutreachRepository(pool).ListRetryableDeadLetters(t.Context(), 50)
	if err != nil || len(listed) != 1 || listed[0].TaskID != wakeup {
		t.Fatalf("unsafe legacy rows filled LIMIT before wakeup: %+v %v", listed, err)
	}
	for _, row := range listed {
		ct, err := r.GetCampaignTask(t.Context(), row.TaskID)
		if err != nil || ct.DispatchIntent != CampaignDispatchWakeup || ct.ContactID != nil || ct.SequenceID != nil {
			t.Fatalf("unverified replay candidate: %+v %v", ct, err)
		}
	}
	for _, source := range []uuid.UUID{send, uuid.New()} {
		id := uuid.New()
		if _, err := r.CreateCampaignReplayTask(t.Context(), &Task{ID: id, TaskType: "campaign", Status: "pending", EmailAccountID: f.account}, &CampaignTask{TaskID: id, CampaignID: &campaign}, source); !errors.Is(err, ErrCampaignReplayUnverified) {
			t.Fatalf("unverified source replay: %v", err)
		}
	}
	var created atomic.Int32
	var wg sync.WaitGroup
	errs := make(chan error, 8)
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			id := uuid.New()
			ok, err := r.CreateCampaignReplayTask(t.Context(), &Task{ID: id, TaskType: "campaign", Status: "pending", EmailAccountID: f.account}, &CampaignTask{TaskID: id, CampaignID: &campaign}, wakeup)
			if err != nil {
				errs <- err
			}
			if ok {
				created.Add(1)
			}
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Fatal(err)
	}
	if created.Load() != 1 {
		t.Fatal("per-campaign replay created duplicate chains", created.Load())
	}
	var intact bool
	if err := pool.QueryRow(t.Context(), `SELECT status='pending' AND attempts=1 AND last_error='retained' AND payload='{"original":true}'::jsonb FROM task_dead_letters WHERE task_id=$1`, wakeup).Scan(&intact); err != nil || !intact {
		t.Fatal("constructor changed original evidence", intact, err)
	}
}

func TestLiveCampaignReplayRechecksIntentAfterRowLockWait(t *testing.T) {
	pool := liveCampaignReplayDB(t)
	f := newWarmupUsageFixture(t, pool)
	r := NewTaskRepository(pool)
	campaign, source, replay := uuid.New(), uuid.New(), uuid.New()
	f.exec(`INSERT INTO campaigns(id,user_id,organization_id,name,description,status,days,timezone,created_at,updated_at) VALUES($1,$2,$3,'Replay','','active',127,'UTC',NOW(),NOW())`, campaign, f.user, f.org)
	if ok, err := r.CreateTaskWithLock(t.Context(), &Task{ID: source, TaskType: "campaign", Status: "dead_lettered", EmailAccountID: f.account}, &CampaignTask{TaskID: source, CampaignID: &campaign}); err != nil || !ok {
		t.Fatal(ok, err)
	}
	f.exec(`INSERT INTO task_dead_letters(task_id,task_type,next_retry_at) VALUES($1,'campaign',NOW())`, source)
	tx, err := pool.Begin(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(context.Background())
	if _, err := tx.Exec(t.Context(), `UPDATE campaign_tasks SET dispatch_intent='send' WHERE task_id=$1`, source); err != nil {
		t.Fatal(err)
	}
	result := make(chan error, 1)
	go func() {
		_, err := r.CreateCampaignReplayTask(t.Context(), &Task{ID: replay, TaskType: "campaign", Status: "pending", EmailAccountID: f.account}, &CampaignTask{TaskID: replay, CampaignID: &campaign}, source)
		result <- err
	}()
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	for {
		var waiting bool
		if err := pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE datname=current_database() AND wait_event_type='Lock' AND query LIKE 'SELECT true FROM task_dead_letters%')`).Scan(&waiting); err != nil {
			t.Fatal(err)
		}
		if waiting {
			break
		}
		if ctx.Err() != nil {
			t.Fatal(ctx.Err())
		}
	}
	if err := tx.Commit(t.Context()); err != nil {
		t.Fatal(err)
	}
	if err := <-result; !errors.Is(err, ErrCampaignReplayUnverified) {
		t.Fatalf("stale wakeup snapshot allowed replay: %v", err)
	}
}
