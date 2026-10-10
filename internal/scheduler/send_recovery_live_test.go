package scheduler

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/models"
)

func TestLiveRecoveryMailboxDoesNotParkHealthyCampaignPool(t *testing.T) {
	for _, reason := range []string{"unknown", "permanent", "authentication", "conflict", "cooldown"} {
		t.Run(reason, func(t *testing.T) {
			_, pool := liveDB(t)
			f := newLiveFixture(t, pool, "UTC")
			ctx := t.Context()
			healthy := uuid.New()
			if _, err := pool.Exec(ctx, `INSERT INTO email_accounts(id,user_id,organization_id,email,name,signature_plain,signature_html,provider,status,campaign_limit,min_wait_time,timezone)
			 VALUES($1,$2,$3,$4,'Healthy','','','smtp_imap','active',50,0,'UTC')`, healthy, f.user, f.org, healthy.String()+"@test.local"); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _, _ = pool.Exec(t.Context(), `DELETE FROM email_accounts WHERE id=$1`, healthy) })
			if _, err := pool.Exec(ctx, `UPDATE email_accounts SET send_recovery_hold=$2,send_recovery_reason=$3,
			 send_cooldown_until=CASE WHEN $2 THEN NULL ELSE NOW()+INTERVAL '1 hour' END WHERE id=$1`, f.mailbox, reason != "cooldown", func() any {
				if reason == "cooldown" {
					return nil
				}
				return reason
			}()); err != nil {
				t.Fatal(err)
			}
			s := loggedScheduler(t, f)
			_, _, sender, err := s.CalculateNextCampaignTime(ctx, f.campaign)
			if err != nil || sender != healthy {
				t.Fatalf("healthy pool parked on unavailable sender: %s %v", sender, err)
			}
			plan, err := s.(CampaignSendPlanner).PlanCampaignDay(ctx, f.campaign, -1)
			if err != nil {
				t.Fatal(err)
			}
			want := models.MailboxPlanRecovery
			if reason == "cooldown" {
				want = models.MailboxPlanCooldown
			}
			found := false
			for _, mb := range plan.Mailboxes {
				if mb.ID == f.mailbox {
					found = true
					if mb.State != want || mb.ExpectedRemaining != 0 {
						t.Fatalf("held mailbox capacity: %+v", mb)
					}
				}
			}
			if !found || plan.ExpectedRemaining == 0 {
				t.Fatalf("healthy capacity lost: %+v", plan)
			}
			if _, err := pool.Exec(ctx, `UPDATE email_accounts SET send_recovery_hold=false,send_recovery_reason=NULL,send_cooldown_until=NOW()-INTERVAL '1 second' WHERE id=$1`, f.mailbox); err != nil {
				t.Fatal(err)
			}
			acct := models.Email{ID: f.mailbox, OrganizationID: &f.org, Provider: "smtp_imap"}
			if gate := s.(*schedulerService).sendGate(ctx, &campaignPass{}, acct); !gate.open() {
				t.Fatalf("expired/repaired gate still closed: %+v", gate)
			}
		})
	}
}

func TestLiveAllSendRecoveryMailboxesDeferWithWake(t *testing.T) {
	_, pool := liveDB(t)
	f := newLiveFixture(t, pool, "UTC")
	if _, err := pool.Exec(t.Context(), `UPDATE email_accounts SET send_recovery_hold=true,send_recovery_reason='unknown' WHERE id=$1`, f.mailbox); err != nil {
		t.Fatal(err)
	}
	s := loggedScheduler(t, f)
	at, _, _, err := s.CalculateNextCampaignTime(t.Context(), f.campaign)
	if err != ErrCampaignDeferred || at.Before(time.Now()) || at.After(time.Now().Add(workerRecheck+time.Second)) {
		t.Fatalf("recovery defer has no bounded wake: %s %v", at, err)
	}
}
