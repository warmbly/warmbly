package scheduler

import (
	"context"
	"testing"

	"github.com/google/uuid"

	"github.com/warmbly/warmbly/internal/models"
)

// noWorkers says no worker is heartbeating.
type noWorkers struct{}

func (noWorkers) IsWorkerLive(context.Context, uuid.UUID) (bool, error) { return false, nil }

// A mailbox no live worker holds is shown as reconnecting, and its day is
// still expected: the gap is minutes long, and the capacity estimate counts it
// the same way. A mailbox that is also held by its warmup health says that
// instead, because that hold is what moves its leads.
func TestLivePlanShowsAMailboxWithoutAWorkerAsReconnecting(t *testing.T) {
	_, pool := liveDB(t)
	f := newLiveFixture(t, pool, "UTC")
	s := loggedScheduler(t, f)
	planner := s.(CampaignSendPlanner)

	// -1: no workspace allowance, which would otherwise clamp both to zero.
	before, err := planner.PlanCampaignDay(context.Background(), f.campaign, -1)
	if err != nil {
		t.Fatal(err)
	}
	if before.ExpectedRemaining <= 0 {
		t.Fatalf("baseline remaining %d, want positive capacity", before.ExpectedRemaining)
	}
	s.(WorkerLivenessAware).WireWorkerLiveness(noWorkers{})
	plan, err := planner.PlanCampaignDay(context.Background(), f.campaign, -1)
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Mailboxes) != 1 || plan.Mailboxes[0].State != models.MailboxPlanNoWorker {
		t.Fatalf("mailboxes %+v, want the one mailbox reconnecting", plan.Mailboxes)
	}
	if plan.ExpectedRemaining != before.ExpectedRemaining {
		t.Fatalf("remaining %d with no worker, %d with one; a worker gap must not cost the day", plan.ExpectedRemaining, before.ExpectedRemaining)
	}

	if _, err := pool.Exec(context.Background(), `INSERT INTO warmup_pool_participants (pool_id, email_account_id, health_state)
	    VALUES ($1, $2, 'quarantined')`, models.WarmupPoolFreeID, f.mailbox); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM warmup_pool_participants WHERE email_account_id = $1`, f.mailbox)
	})
	held, err := planner.PlanCampaignDay(context.Background(), f.campaign, -1)
	if err != nil {
		t.Fatal(err)
	}
	if held.Mailboxes[0].State != models.MailboxPlanHealthHold {
		t.Fatalf("state %q for a quarantined mailbox without a worker, want the health hold", held.Mailboxes[0].State)
	}
}
