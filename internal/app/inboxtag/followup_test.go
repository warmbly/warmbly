package inboxtag

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/repository"
)

func TestFollowUp(t *testing.T) {
	now := time.Date(2026, 9, 17, 12, 0, 0, 0, time.UTC)
	ago := func(d int) time.Time { return now.AddDate(0, 0, -d) }

	cases := []struct {
		name  string
		state ThreadState
		want  string
	}{
		{
			"they replied and we have not answered for days",
			ThreadState{LastOutboundAt: ago(6), LastInboundAt: ago(3), BestIntent: IntentWantsInfo, LastKind: KindHumanReply},
			LabelNeedsReply,
		},
		{
			"they replied yesterday, which is not yet a delay worth flagging",
			ThreadState{LastOutboundAt: ago(2), LastInboundAt: ago(1), BestIntent: IntentWantsInfo, LastKind: KindHumanReply},
			"",
		},
		{
			"an unclassified inbound message is not treated as a human reply",
			ThreadState{LastOutboundAt: ago(6), LastInboundAt: ago(3)},
			"",
		},
		{
			"we sent recently and are simply waiting, which wears nothing",
			ThreadState{LastOutboundAt: ago(1)},
			"",
		},
		{
			"we sent a week ago and never heard back",
			ThreadState{LastOutboundAt: ago(7)},
			LabelFollowUp,
		},
		{
			// The one this whole feature is for.
			"they were interested, then went quiet",
			ThreadState{LastOutboundAt: ago(12), LastInboundAt: ago(14), BestIntent: IntentAgreed},
			LabelGoneQuiet,
		},
		{
			"an interested thread still inside its longer fuse",
			ThreadState{LastOutboundAt: ago(6), LastInboundAt: ago(8), BestIntent: IntentAgreed},
			"",
		},
		{
			// Chasing somebody who declined is rude; chasing somebody who asked
			// to be removed is a compliance problem.
			"they said no",
			ThreadState{LastOutboundAt: ago(30), LastInboundAt: ago(31), BestIntent: IntentNotInterested},
			"",
		},
		{
			"they asked to be removed",
			ThreadState{LastOutboundAt: ago(30), LastInboundAt: ago(31), BestIntent: IntentOptOut},
			"",
		},
		{
			"they said not now",
			ThreadState{LastOutboundAt: ago(40), LastInboundAt: ago(41), BestIntent: IntentNotNow},
			"",
		},
		{
			"we were told we have the wrong person",
			ThreadState{LastOutboundAt: ago(20), LastInboundAt: ago(21), BestIntent: IntentWrongPerson},
			"",
		},
		{
			"the only reply was a bounce, so there is nobody to chase",
			ThreadState{LastOutboundAt: ago(20), LastInboundAt: ago(20), LastKind: KindBounceHard},
			"",
		},
		{
			"the only reply was an autoresponder",
			ThreadState{LastOutboundAt: ago(20), LastInboundAt: ago(20), LastKind: KindAutoReplyOOO},
			"",
		},
		{
			"a platform notice is not a conversation",
			ThreadState{LastOutboundAt: ago(20), LastInboundAt: ago(19), LastKind: KindNotification},
			"",
		},
		{
			"nothing has been sent, so nobody is waiting on us",
			ThreadState{LastInboundAt: ago(5)},
			"",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := FollowUp(tc.state, now); got != tc.want {
				t.Fatalf("FollowUp = %q, want %q", got, tc.want)
			}
		})
	}
}

// A clock that has gone backwards, or a message stamped in the future, must not
// produce a negative age and a label a day early.
func TestFollowUpToleratesFutureTimestamps(t *testing.T) {
	now := time.Date(2026, 9, 17, 12, 0, 0, 0, time.UTC)
	got := FollowUp(ThreadState{LastOutboundAt: now.AddDate(0, 0, 3)}, now)
	if got != "" {
		t.Fatalf("FollowUp with a future send = %q, want none", got)
	}
}

// Every state FollowUp can return has to be a label the workspace can filter
// on, or the feature produces something nobody can find.
func TestFollowUpLabelsAreAllCreated(t *testing.T) {
	all := map[string]bool{}
	for _, l := range AllLabels() {
		all[l] = true
	}
	for _, l := range FollowUpLabels {
		if !all[l] {
			t.Errorf("follow-up label %q is not in AllLabels, so it is never created and cannot be filtered on", l)
		}
	}
}

// ── Sweep ──────────────────────────────────────────────────────────────────

type fakeCategories struct {
	// labels[threadID] is what the thread currently wears.
	labels  map[string]map[string]bool
	seeded  []string
	removed map[string][]string
}

func (f *fakeCategories) EnsureCategory(_ context.Context, _ uuid.UUID, slug string) (uuid.UUID, error) {
	return uuid.NewSHA1(uuid.Nil, []byte(slug)), nil
}
func (f *fakeCategories) EnsureAll(_ context.Context, _ uuid.UUID, slugs []string) error {
	f.seeded = slugs
	return nil
}
func (f *fakeCategories) AddThreadLabels(_ context.Context, _ uuid.UUID, threadID string, ids []uuid.UUID) error {
	if f.labels == nil {
		f.labels = map[string]map[string]bool{}
	}
	if f.labels[threadID] == nil {
		f.labels[threadID] = map[string]bool{}
	}
	for _, id := range ids {
		f.labels[threadID][id.String()] = true
	}
	return nil
}
func (f *fakeCategories) SyncExclusiveLabels(ctx context.Context, orgID uuid.UUID, threadID string, family []string, want string) error {
	if f.labels == nil {
		f.labels = map[string]map[string]bool{}
	}
	if f.labels[threadID] == nil {
		f.labels[threadID] = map[string]bool{}
	}
	for _, slug := range family {
		if slug != want {
			delete(f.labels[threadID], slug)
		}
	}
	if want != "" {
		f.labels[threadID][want] = true
	}
	return nil
}
func (f *fakeCategories) RemoveAutoLabels(_ context.Context, _ uuid.UUID, threadID string, slugs []string) error {
	if f.removed == nil {
		f.removed = map[string][]string{}
	}
	f.removed[threadID] = append(f.removed[threadID], slugs...)
	return nil
}
func (f *fakeCategories) has(threadID, label string) bool { return f.labels[threadID][label] }

// The sweep reuses stored classifications and makes no model calls of its own.
func TestSweepNeedsNoModel(t *testing.T) {
	now := time.Now()
	repo := &fakeRepo{states: []repository.ThreadFollowUpState{
		{ThreadID: "t-ours", LastOutboundAt: now.AddDate(0, 0, -6), LastInboundAt: now.AddDate(0, 0, -3), BestIntent: IntentWantsInfo, LastKind: KindHumanReply},
		{ThreadID: "t-chase", LastOutboundAt: now.AddDate(0, 0, -8)},
		{ThreadID: "t-cold", LastOutboundAt: now.AddDate(0, 0, -12), LastInboundAt: now.AddDate(0, 0, -14), BestIntent: IntentAgreed},
		{ThreadID: "t-closed", LastOutboundAt: now.AddDate(0, 0, -30), LastInboundAt: now.AddDate(0, 0, -31), BestIntent: IntentOptOut},
	}}
	cats := &fakeCategories{}
	asker := &countingAsker{}
	svc := NewService(asker, repo, cats, nil, true)

	p, err := svc.SweepFollowUps(context.Background(), uuid.New(), now.AddDate(0, 0, -90), 0)
	if err != nil {
		t.Fatalf("sweep: %v", err)
	}
	if asker.calls != 0 {
		t.Fatalf("the sweep made %d model calls; it is arithmetic over stored facts", asker.calls)
	}
	if p.Threads != 4 {
		t.Errorf("swept %d threads, want 4", p.Threads)
	}

	for _, tc := range []struct{ thread, label string }{
		{"t-ours", LabelNeedsReply},
		{"t-chase", LabelFollowUp},
		{"t-cold", LabelGoneQuiet},
	} {
		if !cats.has(tc.thread, tc.label) {
			t.Errorf("%s did not get %q", tc.thread, tc.label)
		}
	}
	if len(cats.labels["t-closed"]) != 0 {
		t.Errorf("an opted-out thread was labelled %v; it must never be chased", cats.labels["t-closed"])
	}
}

// The states move with the calendar, so a re-sweep has to REPLACE the label.
// A thread wearing both Follow up and Needs reply is wearing its history
// rather than its state.
func TestSweepReplacesRatherThanAccumulates(t *testing.T) {
	now := time.Now()
	cats := &fakeCategories{}
	repo := &fakeRepo{states: []repository.ThreadFollowUpState{
		{ThreadID: "t-1", LastOutboundAt: now.AddDate(0, 0, -9)},
	}}
	svc := NewService(&countingAsker{}, repo, cats, nil, true)
	orgID := uuid.New()

	if _, err := svc.SweepFollowUps(context.Background(), orgID, now.AddDate(0, 0, -90), 0); err != nil {
		t.Fatalf("first sweep: %v", err)
	}
	if !cats.has("t-1", LabelFollowUp) {
		t.Fatal("expected Follow up on a send nobody answered")
	}

	// They answer, and we sit on it.
	repo.states[0].LastInboundAt = now.AddDate(0, 0, -3)
	repo.states[0].LastKind = KindHumanReply
	repo.states[0].BestIntent = IntentWantsInfo
	if _, err := svc.SweepFollowUps(context.Background(), orgID, now.AddDate(0, 0, -90), 0); err != nil {
		t.Fatalf("second sweep: %v", err)
	}
	if !cats.has("t-1", LabelNeedsReply) {
		t.Error("did not move to Needs reply")
	}
	if cats.has("t-1", LabelFollowUp) {
		t.Error("kept Follow up alongside Needs reply; the thread now wears its history")
	}

	// We answer, so nothing is owed either way yet.
	repo.states[0].LastOutboundAt = now
	if _, err := svc.SweepFollowUps(context.Background(), orgID, now.AddDate(0, 0, -90), 0); err != nil {
		t.Fatalf("third sweep: %v", err)
	}
	if cats.has("t-1", LabelNeedsReply) {
		t.Error("still asking us to reply after we did")
	}
}
