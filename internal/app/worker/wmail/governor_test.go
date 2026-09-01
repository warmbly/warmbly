package wmail

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/config"
	"github.com/warmbly/warmbly/internal/models"
)

// Every lane must span exactly the budgets the design promises: priority has
// its own daily window and never touches the organization budget; live spans
// burst, hourly, daily and org; backfill is paced per minute and charged to
// the org budget only.
func TestGovernorWindowsPerLane(t *testing.T) {
	org := uuid.New()
	g := newGovernor(uuid.New(), &org, nil, models.SyncPolicy{DailyMessages: 10, OrgDailyMessages: 100})
	now := time.Date(2026, 8, 18, 13, 7, 0, 0, time.UTC)

	reasons := func(lane SyncLane) []string {
		var out []string
		for _, w := range g.windows(lane, now) {
			out = append(out, w.reason)
		}
		return out
	}

	if got := reasons(LanePriority); len(got) != 1 || got[0] != models.SyncThrottlePriorityFull {
		t.Fatalf("priority windows = %v", got)
	}
	if got := reasons(LaneLive); len(got) != 4 || got[3] != models.SyncThrottleOrgDaily {
		t.Fatalf("live windows = %v", got)
	}
	if got := reasons(LaneBackfill); len(got) != 2 || got[0] != models.SyncThrottleBurst || got[1] != models.SyncThrottleOrgDaily {
		t.Fatalf("backfill windows = %v", got)
	}

	// Without an organization there is no org window at all.
	g2 := newGovernor(uuid.New(), nil, nil, models.SyncPolicy{})
	if got := reasons(LaneLive); len(got) != 4 {
		t.Fatalf("live windows = %v", got)
	}
	if got := g2.windows(LaneLive, now); len(got) != 3 {
		t.Fatalf("live windows without org = %d, want 3", len(got))
	}

	// A daily window rolls at the next UTC midnight, an hourly one at the hour.
	live := g.windows(LaneLive, now)
	if live[1].until != now.Truncate(time.Hour).Add(time.Hour) {
		t.Errorf("hourly until = %v", live[1].until)
	}
	if live[2].until != time.Date(2026, 8, 19, 0, 0, 0, 0, time.UTC) {
		t.Errorf("daily until = %v", live[2].until)
	}
	if live[2].limit != 10 || live[3].limit != 100 {
		t.Errorf("limits = %d/%d, want policy 10/100", live[2].limit, live[3].limit)
	}
}

// Zero policy fields resolve to compiled defaults, never to "no budget".
func TestGovernorDefaultsPolicy(t *testing.T) {
	g := newGovernor(uuid.New(), nil, nil, models.SyncPolicy{})
	if g.policy.DailyMessages != config.SyncDailyMessagesMailboxDefault ||
		g.policy.OrgDailyMessages != config.SyncDailyMessagesOrgDefault ||
		g.policy.BackfillDays != config.SyncBackfillDaysDefault ||
		g.policy.BackfillMessages != config.SyncBackfillMessagesDefault {
		t.Fatalf("policy = %+v", g.policy)
	}
}

// Without a cache the governor admits everything (fail open), and never
// reports a flood or chronic overage.
func TestGovernorFailsOpenWithoutCache(t *testing.T) {
	g := newGovernor(uuid.New(), nil, nil, models.SyncPolicy{})
	for _, lane := range []SyncLane{LanePriority, LaneLive, LaneBackfill} {
		if a := g.Admit(t.Context(), lane); !a.OK {
			t.Errorf("%s denied without a cache: %+v", lane, a)
		}
	}
	if g.ObserveLive(t.Context(), 1_000_000) {
		t.Error("flood reported without a cache")
	}
	if g.RecordThrottledDay(t.Context()) {
		t.Error("chronic overage reported without a cache")
	}
}

// A hold only ever extends: a burst denial after a daily one must not shorten
// the reported ThrottledUntil, and release clears it once it has passed.
func TestSyncTrackerThrottleAndRelease(t *testing.T) {
	var sent []models.SyncState
	tr := newSyncTracker(nil, func(s models.SyncState) error { sent = append(sent, s); return nil })
	if tr.state.BackfillStatus != models.SyncBackfillPending {
		t.Fatalf("fresh status = %q", tr.state.BackfillStatus)
	}

	now := time.Now()
	tr.throttle(Admission{OK: false, Reason: models.SyncThrottleDaily, Until: now.Add(6 * time.Hour)})
	tr.throttle(Admission{OK: false, Reason: models.SyncThrottleBurst, Until: now.Add(3 * time.Minute)})
	if tr.state.ThrottleReason != models.SyncThrottleDaily {
		t.Fatalf("reason = %q, burst must not override daily", tr.state.ThrottleReason)
	}
	tr.release(now.Add(time.Hour))
	if tr.state.ThrottledUntil == nil {
		t.Fatal("released before the hold expired")
	}
	tr.release(now.Add(7 * time.Hour))
	if tr.state.ThrottledUntil != nil || tr.state.ThrottleReason != "" {
		t.Fatal("hold not cleared after expiry")
	}

	// touch relays a dirty state once, then stays quiet until the heartbeat.
	tr.touch(now)
	tr.touch(now.Add(time.Minute))
	if len(sent) != 1 {
		t.Fatalf("relayed %d times, want 1", len(sent))
	}
	tr.touch(now.Add(syncStateHeartbeat + time.Second))
	if len(sent) != 2 {
		t.Fatalf("heartbeat did not relay: %d", len(sent))
	}
}

// startBackfill fixes the window once; a second call (a later tick with a
// different setting) must not move it.
func TestSyncTrackerBackfillWindowIsFixed(t *testing.T) {
	tr := newSyncTracker(nil, func(models.SyncState) error { return nil })
	now := time.Now()
	tr.startBackfill(now, 30)
	first := *tr.state.BackfillSince
	tr.startBackfill(now.Add(time.Hour), 90)
	if !tr.state.BackfillSince.Equal(first) {
		t.Fatal("backfill window moved after start")
	}
	tr.completeBackfill(now)
	if tr.state.BackfillStatus != models.SyncBackfillComplete || tr.state.BackfillCompletedAt == nil {
		t.Fatal("not completed")
	}
}

func TestImapBackfillEligible(t *testing.T) {
	cases := []struct {
		name  string
		attrs []string
		want  bool
	}{
		{"INBOX", nil, true},
		{"Sent", []string{"\\Sent"}, true},
		{"Archive", []string{"\\Archive"}, true},
		{"Clients/2026", nil, true},
		{"[Gmail]/All Mail", []string{"\\All"}, false},
		{"Trash", []string{"\\Trash"}, false},
		{"Junk", nil, false},
		{"INBOX.Spam", nil, false},
		// Drafts is imported since the folder sidebar gave it a destination;
		// trash and spam stay out so their history cannot eat the budget.
		{"Drafts", []string{"\\Drafts"}, true},
		{"[Gmail]", []string{"\\Noselect"}, false},
	}
	for _, tc := range cases {
		if got := imapBackfillEligible(&models.Mailbox{Name: tc.name, Attrs: tc.attrs}); got != tc.want {
			t.Errorf("%q %v: eligible = %v, want %v", tc.name, tc.attrs, got, tc.want)
		}
	}
}

// The next pass waits longer while fair use holds the mailbox, but never
// beyond the backoff ceiling and never below half the base interval.
func TestNextSyncDelay(t *testing.T) {
	w := &WMail{tracker: newSyncTracker(nil, func(models.SyncState) error { return nil })}
	base := time.Minute
	for i := 0; i < 50; i++ {
		d := w.nextSyncDelay(base, nil)
		if d < base/2 || d > base+base/10 {
			t.Fatalf("plain delay %v out of band", d)
		}
	}
	until := time.Now().Add(3 * time.Hour)
	w.tracker.state.ThrottledUntil = &until
	for i := 0; i < 50; i++ {
		d := w.nextSyncDelay(base, nil)
		if d < syncBackoffMax-syncBackoffMax/10 || d > syncBackoffMax+syncBackoffMax/10 {
			t.Fatalf("throttled delay %v not near the ceiling", d)
		}
	}
}
