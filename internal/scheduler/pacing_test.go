package scheduler

import (
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/warmbly/warmbly/internal/app/behavior"
	"github.com/warmbly/warmbly/internal/config"
	"github.com/warmbly/warmbly/internal/models"
)

// TestDeferSlotCapsAFarFutureWakeup is issue #189's stall in miniature. A
// campaign is one self-perpetuating task, so a deferral parked at the literal
// next-due moment ("wait 3 days") is also the next time anything re-reads the
// campaign — leads imported meanwhile sit at "Queued / Not started" until then.
func TestDeferSlotCapsAFarFutureWakeup(t *testing.T) {
	horizon := config.CampaignMaxDeferMinutes * time.Minute

	for _, tc := range []struct {
		name   string
		in     time.Time
		capped bool
	}{
		{"three days out", time.Now().Add(72 * time.Hour), true},
		{"just past the horizon", time.Now().Add(horizon + 5*time.Minute), true},
		{"zero (nothing computed)", time.Time{}, true},
		{"inside the horizon", time.Now().Add(2 * time.Minute), false},
	} {
		got := DeferSlot(tc.in)
		switch {
		case tc.capped:
			if got.After(time.Now().Add(horizon + time.Second)) {
				t.Errorf("%s: parked at %s, past the %s horizon", tc.name, got, horizon)
			}
		default:
			if !got.Equal(tc.in) {
				t.Errorf("%s: a near-term defer must be kept exactly, got %s want %s", tc.name, got, tc.in)
			}
		}
	}
}

// TestWakeSlotStartsASendableChainNow: a chain seeded outside a tick (start,
// resume, leads arriving) wakes when a send is allowed, not at the paced slot of
// the send after it, which held a just-started campaign for ten minutes.
func TestWakeSlotStartsASendableChainNow(t *testing.T) {
	paced := time.Now().Add(12 * time.Minute)
	if got := WakeSlot(paced, nil); time.Until(got) > time.Second {
		t.Fatalf("sendable chain wakes at %s, want now", got)
	}
	deferred := time.Now().Add(72 * time.Hour)
	if got := WakeSlot(deferred, ErrCampaignDeferred); got.After(time.Now().Add(config.CampaignMaxDeferMinutes*time.Minute + time.Second)) {
		t.Fatalf("deferred chain wakes at %s, past the defer horizon", got)
	}
	near := time.Now().Add(3 * time.Minute)
	if got := WakeSlot(near, ErrLeadDeferred); !got.Equal(near) {
		t.Fatalf("a near deferral must be kept exactly, got %s want %s", got, near)
	}
}

// TestPoolRemainingCountsEveryMailboxSendingToday: the campaign chain runs one
// task at a time, so the even-distribution interval is the campaign's whole send
// rate. Pacing it by the selected mailbox alone made a three-mailbox campaign
// send at one mailbox's rate and leave the other two idle.
func TestPoolRemainingCountsEveryMailboxSendingToday(t *testing.T) {
	now := time.Now()
	pool := []AccountCandidate{
		{RemainingToday: 30},
		{RemainingToday: 30},
		{RemainingToday: 25},
	}
	if got := poolRemainingOn(pool, now); got != 85 {
		t.Fatalf("pool remaining = %d, want 85", got)
	}
	if single := poolRemainingOn(pool[:1], now); single != 30 {
		t.Fatalf("single-mailbox pool = %d, want 30 (unchanged behaviour)", single)
	}
}

// TestPoolRemainingSkipsMailboxesWaitingForTomorrow: a mailbox whose today is
// already spent has been walked to a later day, and counting tomorrow's
// allowance as if it were available now would pace the campaign faster than the
// mailboxes that can actually send today can keep up with.
func TestPoolRemainingSkipsMailboxesWaitingForTomorrow(t *testing.T) {
	now := time.Now()
	tomorrow := now.Add(24 * time.Hour)

	b := behavior.NewStandalone(models.DefaultSendingBehavior(uuid.New()), time.UTC)
	b.Enabled = true

	pool := []AccountCandidate{
		{RemainingToday: 30, Behavior: b, OpenAt: &now, OpenLoc: b.Loc},
		{RemainingToday: 40, Behavior: b, OpenAt: &tomorrow, OpenLoc: b.Loc},
	}
	if got := poolRemainingOn(pool, now); got != 30 {
		t.Fatalf("pool remaining = %d, want 30 (only the mailbox open today)", got)
	}

	// The whole pass pushed to tomorrow because every mailbox was at capacity
	// today: both mailboxes are open by then and both must be counted, or the
	// pacing step is skipped entirely and tomorrow's sends bunch at the window
	// start.
	if got := poolRemainingOn(pool, tomorrow.Add(time.Hour)); got != 70 {
		t.Fatalf("pool remaining tomorrow = %d, want 70 (both mailboxes open by then)", got)
	}
}
