package repository

import (
	"context"
	"errors"
	"os"
	"testing"

	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/infrastructure/db"
	"github.com/warmbly/warmbly/internal/models"
)

func TestLiveMailboxPlanNewAccountDefaults(t *testing.T) {
	f, _ := lineageFixture(t)
	repo := NewEmailRepostory(&db.DB{Pool: f.pool}, nil)
	account, xerr := repo.NewManagedAccount(t.Context(), f.user.String(), models.NewOauthAccount{
		Email: uuid.NewString() + "@example.test", Name: "New mailbox", Provider: models.InboxProviderGoogle, OrganizationID: &f.org,
	})
	if xerr != nil {
		t.Fatal(xerr)
	}
	t.Cleanup(func() {
		if _, err := f.pool.Exec(context.Background(), `DELETE FROM email_accounts WHERE id=$1`, account.ID); err != nil {
			t.Error(err)
		}
	})
	b, err := NewBehaviorRepository(f.pool).GetBehavior(t.Context(), account.ID)
	if err != nil || b == nil || !b.Enabled || b.DailyLimitMin != 28 || b.DailyLimitMax != 32 || account.CampaignLimit != 50 {
		t.Fatalf("new mailbox plan/default ceiling: %+v %+v %v", account, b, err)
	}
	legacy, err := NewBehaviorRepository(f.pool).GetBehavior(t.Context(), f.sender)
	if err != nil || legacy != nil {
		t.Fatalf("existing fixed schedule was opted in: %+v %v", legacy, err)
	}
}

func TestLiveMailboxPlanDefaultsMigrationPreservesRows(t *testing.T) {
	f, _ := lineageFixture(t)
	tx, err := f.pool.Begin(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(t.Context())
	if _, err := tx.Exec(t.Context(), `CREATE TEMP TABLE email_account_behavior(daily_limit_min int DEFAULT 30,daily_limit_max int DEFAULT 45); INSERT INTO email_account_behavior VALUES(30,45),(7,11)`); err != nil {
		t.Fatal(err)
	}
	for _, direction := range []string{"up", "down"} {
		migration, err := os.ReadFile("../infrastructure/db/migrations/000276_mailbox_behavior_defaults." + direction + ".sql")
		if err != nil {
			t.Fatal(err)
		}
		if _, err := tx.Exec(t.Context(), string(migration)); err != nil {
			t.Fatal(err)
		}
		var preserved int
		if err := tx.QueryRow(t.Context(), `SELECT COUNT(*) FROM email_account_behavior WHERE (daily_limit_min,daily_limit_max) IN ((30,45),(7,11))`).Scan(&preserved); err != nil || preserved != 2 {
			t.Fatalf("%s migration changed existing values: %d %v", direction, preserved, err)
		}
		var low, high int
		if err := tx.QueryRow(t.Context(), `INSERT INTO email_account_behavior DEFAULT VALUES RETURNING daily_limit_min,daily_limit_max`).Scan(&low, &high); err != nil {
			t.Fatal(err)
		}
		if direction == "up" && (low != 28 || high != 32) || direction == "down" && (low != 30 || high != 45) {
			t.Fatalf("%s defaults: %d-%d", direction, low, high)
		}
		if direction == "up" {
			if _, err := tx.Exec(t.Context(), `DELETE FROM email_account_behavior WHERE daily_limit_min=28`); err != nil {
				t.Fatal(err)
			}
		}
	}
}

func TestLiveMailboxPlanAdmissionAndWorkerRecheck(t *testing.T) {
	f := newCampaignAdmissionFixture(t, 50)
	f.exec(t, `UPDATE email_accounts SET timezone='Pacific/Honolulu' WHERE id=$1`, f.sender)
	f.exec(t, `INSERT INTO email_account_behavior(email_account_id,enabled) VALUES($1,true)`, f.sender)
	in, _ := f.request(t, f.sender)
	if _, err := f.r.ReserveOutbound(t.Context(), in); !errors.Is(err, ErrMailboxSendingPlan) {
		t.Fatalf("enabled profile without persisted plan must fail closed: %v", err)
	}
	f.exec(t, `INSERT INTO email_account_daily_plan(email_account_id,plan_date,timezone,is_working_day,daily_limit,hourly_limit,work_start_minute,work_end_minute,gap_min_seconds,gap_max_seconds)
	 VALUES($1,(NOW() AT TIME ZONE 'Pacific/Honolulu')::date,'Pacific/Honolulu',true,2,100,0,1440,90,420)`, f.sender)
	f.history(t, f.sender, 1, 0, "<sent@example.test>", false)
	f.history(t, f.sender, 10, 0, "", true)
	nonce, err := f.r.ReserveOutbound(t.Context(), in)
	if err != nil {
		t.Fatal(err)
	}
	tx, err := f.pool.Begin(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if err := checkMailboxSendingPlan(t.Context(), tx, in.TaskID, in.MailboxID); err != nil {
		t.Fatalf("current reservation counted twice: %v", err)
	}
	if err := tx.Rollback(t.Context()); err != nil {
		t.Fatal(err)
	}
	f.history(t, f.sender, 1, 0, "<another-send@example.test>", false)
	state, err := f.r.BeginOutbound(t.Context(), in.TaskID, f.sender, f.worker, nonce)
	if err != nil || state == nil || state.State != "denied" {
		t.Fatalf("worker must recheck plan: %+v %v", state, err)
	}
	var released bool
	if err := f.pool.QueryRow(t.Context(), `SELECT send_released_at IS NOT NULL FROM tasks WHERE id=$1`, in.TaskID).Scan(&released); err != nil || !released {
		t.Fatalf("plan denial kept reservation: %v %v", released, err)
	}
	var attempts int
	if err := f.pool.QueryRow(t.Context(), `SELECT COUNT(*) FROM outbound_attempts WHERE task_id=$1`, in.TaskID).Scan(&attempts); err != nil || attempts != 0 {
		t.Fatalf("plan denial spent an attempt: %d %v", attempts, err)
	}
}

func TestLiveMailboxPlanGatesAndIsolation(t *testing.T) {
	f := newCampaignAdmissionFixture(t, 5000)
	f.exec(t, `INSERT INTO email_account_behavior(email_account_id,enabled) VALUES($1,true)`, f.sender)
	f.exec(t, `INSERT INTO email_account_daily_plan(email_account_id,plan_date,timezone,is_working_day,daily_limit,hourly_limit,work_start_minute,work_end_minute,gap_min_seconds,gap_max_seconds)
	 VALUES($1,NOW()::date,'UTC',true,2,2,0,1440,90,420)`, f.sender)
	f.history(t, f.sender, 1, 0, "<today@example.test>", false)
	f.history(t, f.sender, 20, -1, "<yesterday@example.test>", false)
	f.history(t, f.recipient, 20, 0, "<other-mailbox@example.test>", false)
	for _, test := range []struct {
		name, update string
		allowed      bool
	}{
		{"separate sender and previous day", "daily_limit=2,hourly_limit=2,is_working_day=true,work_end_minute=1440", true},
		{"daily exhausted", "daily_limit=1,hourly_limit=2", false},
		{"hourly exhausted", "daily_limit=2,hourly_limit=1", false},
		{"non-working day", "hourly_limit=2,is_working_day=false", false},
		{"closed window", "is_working_day=true,work_end_minute=0", false},
		{"lunch", "work_end_minute=1440,lunch_start_minute=0,lunch_end_minute=1440", false},
		{"working window", "lunch_start_minute=NULL,lunch_end_minute=NULL", true},
	} {
		t.Run(test.name, func(t *testing.T) {
			f.exec(t, `UPDATE email_account_daily_plan SET `+test.update+` WHERE email_account_id=$1`, f.sender)
			tx, err := f.pool.Begin(t.Context())
			if err != nil {
				t.Fatal(err)
			}
			defer tx.Rollback(t.Context())
			err = checkMailboxSendingPlan(t.Context(), tx, uuid.Nil, f.sender)
			if test.allowed && err != nil || !test.allowed && !errors.Is(err, ErrMailboxSendingPlan) {
				t.Fatalf("allowed=%v: %v", test.allowed, err)
			}
		})
	}
	f.exec(t, `UPDATE email_account_behavior SET enabled=false WHERE email_account_id=$1`, f.sender)
	f.exec(t, `DELETE FROM email_account_daily_plan WHERE email_account_id=$1`, f.sender)
	tx, err := f.pool.Begin(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(t.Context())
	if err := checkMailboxSendingPlan(t.Context(), tx, uuid.Nil, f.sender); err != nil {
		t.Fatalf("disabled profile changed fixed-schedule authority: %v", err)
	}
}
