package fleetnode

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/repository"
)

type fleetNodes struct {
	repository.FleetNodeRepository
	nodes []models.FleetNode
}

func (f *fleetNodes) Get(_ context.Context, id uuid.UUID) (*models.FleetNode, error) {
	for i := range f.nodes {
		if f.nodes[i].ID == id {
			n := f.nodes[i]
			return &n, nil
		}
	}
	return nil, nil
}

func (f *fleetNodes) List(context.Context, models.NodeRole) ([]models.FleetNode, error) {
	return append([]models.FleetNode(nil), f.nodes...), nil
}

// casSettings stores the state as JSON, the way admin_settings does, and
// refuses a write whose prev no longer matches.
type casSettings struct {
	repository.FleetSettingsRepository
	raw      []byte
	conflict bool
}

func (c *casSettings) GetRelease(context.Context) (*models.FleetReleaseState, error) {
	if c.raw == nil {
		return nil, nil
	}
	var st models.FleetReleaseState
	return &st, json.Unmarshal(c.raw, &st)
}

func (c *casSettings) CompareAndSetRelease(_ context.Context, prev, next *models.FleetReleaseState) (bool, error) {
	if c.conflict {
		return false, nil
	}
	if prev == nil {
		if c.raw != nil {
			return false, nil
		}
	} else {
		old, _ := json.Marshal(prev)
		if string(old) != string(c.raw) {
			return false, nil
		}
	}
	c.raw, _ = json.Marshal(next)
	return true, nil
}

type rolloutFixture struct {
	svc      *Service
	nodes    *fleetNodes
	settings *casSettings
	now      time.Time
}

func newRolloutFixture(t *testing.T, workers, consumers int) *rolloutFixture {
	t.Helper()
	t.Setenv("WARMBLY_VERSION", "v1.5.0")
	f := &rolloutFixture{nodes: &fleetNodes{}, settings: &casSettings{}, now: time.Date(2026, 10, 24, 12, 0, 0, 0, time.UTC)}
	seen := f.now
	add := func(role models.NodeRole) {
		f.nodes.nodes = append(f.nodes.nodes, models.FleetNode{
			ID: uuid.New(), Role: role, Name: string(role), Version: "v1.4.0-kafka", Active: true, LastSeenAt: &seen,
		})
	}
	for i := 0; i < workers; i++ {
		add(models.NodeRoleWorker)
	}
	for i := 0; i < consumers; i++ {
		add(models.NodeRoleConsumer)
	}
	raw, _ := json.Marshal(models.FleetReleaseState{Channel: models.FleetChannelPinned, Tag: "v1.4.0"})
	f.settings.raw = raw
	f.svc = &Service{nodes: f.nodes, settings: f.settings, variant: "-kafka", now: func() time.Time { return f.now }}
	return f
}

// advanceTime moves the fixture clock and keeps every active node beating on it.
func (f *rolloutFixture) advanceTime(d time.Duration) {
	f.now = f.now.Add(d)
	seen := f.now
	for i := range f.nodes.nodes {
		if f.nodes.nodes[i].Active {
			f.nodes.nodes[i].LastSeenAt = &seen
		}
	}
}

// converge makes every admitted node report the target and an uptime.
func (f *rolloutFixture) converge(admitted []uuid.UUID, version string, uptime time.Duration) {
	up := int64(uptime.Seconds())
	for i := range f.nodes.nodes {
		for _, id := range admitted {
			if f.nodes.nodes[i].ID == id {
				f.nodes.nodes[i].Version = version
				f.nodes.nodes[i].Usage.UptimeSeconds = &up
			}
		}
	}
}

const testDigest = "sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"

func TestRolloutCanaryThenWavesThenDone(t *testing.T) {
	f := newRolloutFixture(t, 4, 1)
	ctx := context.Background()
	policy := models.FleetRolloutPolicy{Canary: 1, Waves: []int{50, 100}, BakeSeconds: 600, DeadlineSeconds: 300}
	st, err := f.svc.StartRollout(ctx, "v1.5.0-kafka", map[string]string{"worker-kafka": testDigest, "consumer-kafka": "not-a-digest"}, policy)
	if err != nil {
		t.Fatal(err)
	}
	r := st.Rollout
	if st.Tag != "v1.4.0" || r.Target != "v1.5.0" || r.From != "v1.4.0" || len(r.Admitted) != 1 {
		t.Fatalf("unexpected start state: tag=%s rollout=%+v", st.Tag, r)
	}
	if _, ok := r.Digests["consumer-kafka"]; ok {
		t.Fatal("an invalid digest must be dropped, not handed to a node")
	}
	canary, _ := f.nodes.Get(ctx, r.Admitted[0])
	if canary.Role != models.NodeRoleWorker {
		t.Fatal("the canary must be a worker")
	}

	// The canary is told the target and digest; everyone else stays on From.
	for _, n := range f.nodes.nodes {
		v, d := f.svc.desired(ctx, n.ID)
		if n.ID == canary.ID {
			if v != "v1.5.0-kafka" || d != testDigest {
				t.Fatalf("canary got %q %q", v, d)
			}
		} else if v != "v1.4.0-kafka" || d != "" {
			t.Fatalf("non-admitted node got %q %q", v, d)
		}
	}

	// Not converged: holds.
	f.advanceTime(time.Minute)
	if st, _ = f.svc.AdvanceRollout(ctx, false); len(st.Rollout.Admitted) != 1 {
		t.Fatal("must not advance before the canary reports the target")
	}
	// Converged but not baked: holds.
	f.converge(st.Rollout.Admitted, "v1.5.0-kafka", time.Minute)
	if st, _ = f.svc.AdvanceRollout(ctx, false); st.Rollout.Stage != models.FleetRolloutCanary {
		t.Fatal("must not advance before the canary has stayed up for the bake")
	}
	// Baked: wave one takes the fleet to half.
	f.advanceTime(10 * time.Minute)
	f.converge(st.Rollout.Admitted, "v1.5.0-kafka", 11*time.Minute)
	st, err = f.svc.AdvanceRollout(ctx, false)
	if err != nil {
		t.Fatal(err)
	}
	if st.Rollout.Stage != models.FleetRolloutWave || len(st.Rollout.Admitted) != 3 {
		t.Fatalf("wave one should admit up to 50%% of 5 nodes, got %d in %s", len(st.Rollout.Admitted), st.Rollout.Stage)
	}
	f.advanceTime(11 * time.Minute)
	f.converge(st.Rollout.Admitted, "v1.5.0-kafka", 11*time.Minute)
	st, _ = f.svc.AdvanceRollout(ctx, false)
	if len(st.Rollout.Admitted) != 5 {
		t.Fatalf("last wave should admit everyone, got %d", len(st.Rollout.Admitted))
	}
	f.advanceTime(11 * time.Minute)
	f.converge(st.Rollout.Admitted, "v1.5.0-kafka", 11*time.Minute)
	st, _ = f.svc.AdvanceRollout(ctx, false)
	if st.Rollout != nil || st.Tag != "v1.5.0" {
		t.Fatalf("rollout should finish with Tag at the target, got tag=%s rollout=%+v", st.Tag, st.Rollout)
	}
}

func TestRolloutHaltsAndRevertsOnHostRollback(t *testing.T) {
	f := newRolloutFixture(t, 3, 0)
	ctx := context.Background()
	st, err := f.svc.StartRollout(ctx, "v1.5.0", nil, models.FleetRolloutPolicy{})
	if err != nil {
		t.Fatal(err)
	}
	canary := st.Rollout.Admitted[0]
	for i := range f.nodes.nodes {
		if f.nodes.nodes[i].ID == canary {
			f.nodes.nodes[i].UpdateFailed = "v1.5.0-kafka"
		}
	}
	st, err = f.svc.AdvanceRollout(ctx, false)
	if err != nil {
		t.Fatal(err)
	}
	if st.Rollout.Stage != models.FleetRolloutHalted || st.Rollout.HaltReason == "" {
		t.Fatalf("a host rollback must halt the rollout, got %+v", st.Rollout)
	}
	if v, _ := f.svc.desired(ctx, canary); v != "v1.4.0-kafka" {
		t.Fatalf("a halted rollout must send the canary back to From, got %q", v)
	}
	if st, _ = f.svc.AdvanceRollout(ctx, true); st.Rollout.Stage != models.FleetRolloutHalted {
		t.Fatal("forcing must never resume a halted rollout")
	}
}

func TestRolloutHaltsOnDeadlineAndSilentNode(t *testing.T) {
	f := newRolloutFixture(t, 2, 0)
	ctx := context.Background()
	st, _ := f.svc.StartRollout(ctx, "v1.5.0", nil, models.FleetRolloutPolicy{DeadlineSeconds: 300})
	f.advanceTime(6 * time.Minute)
	if st, _ = f.svc.AdvanceRollout(ctx, false); st.Rollout.Stage != models.FleetRolloutHalted {
		t.Fatal("an admitted node missing the deadline must halt the rollout")
	}

	g := newRolloutFixture(t, 2, 0)
	st, _ = g.svc.StartRollout(ctx, "v1.5.0", nil, models.FleetRolloutPolicy{})
	for i := range g.nodes.nodes {
		if g.nodes.nodes[i].ID == st.Rollout.Admitted[0] {
			old := g.now.Add(-time.Hour)
			g.nodes.nodes[i].LastSeenAt = &old
		}
	}
	if st, _ = g.svc.AdvanceRollout(ctx, false); st.Rollout.Stage != models.FleetRolloutHalted {
		t.Fatal("an admitted node that stops beating must halt the rollout")
	}
}

func TestRolloutStartIsIdempotentAndPinWins(t *testing.T) {
	f := newRolloutFixture(t, 2, 0)
	ctx := context.Background()
	first, _ := f.svc.StartRollout(ctx, "v1.5.0", nil, models.FleetRolloutPolicy{})
	again, err := f.svc.StartRollout(ctx, "v1.5.0-kafka", nil, models.FleetRolloutPolicy{})
	if err != nil || again.Rollout.Admitted[0] != first.Rollout.Admitted[0] {
		t.Fatal("starting the running rollout again must change nothing")
	}
	f.nodes.nodes[0].PinnedVersion = "v1.3.0"
	if v, d := f.svc.desired(ctx, f.nodes.nodes[0].ID); v != "v1.3.0-kafka" || d != "" {
		t.Fatalf("a pin must win over a rollout, got %q %q", v, d)
	}
}

func TestRolloutConflictIsReported(t *testing.T) {
	f := newRolloutFixture(t, 1, 0)
	f.settings.conflict = true
	if _, err := f.svc.StartRollout(context.Background(), "v1.5.0", nil, models.FleetRolloutPolicy{}); !errors.Is(err, ErrRolloutConflict) {
		t.Fatalf("a lost compare-and-set must be reported, got %v", err)
	}
}

// A backend that predates rollouts reads only Tag, which stays at From.
func TestRolloutStateIsSafeForOlderReaders(t *testing.T) {
	f := newRolloutFixture(t, 2, 0)
	if _, err := f.svc.StartRollout(context.Background(), "v1.5.0", nil, models.FleetRolloutPolicy{}); err != nil {
		t.Fatal(err)
	}
	var old struct {
		Tag string `json:"tag"`
	}
	if err := json.Unmarshal(f.settings.raw, &old); err != nil || old.Tag != "v1.4.0" {
		t.Fatalf("older readers must still see From, got %q", old.Tag)
	}
}

func TestRolloutRestartWindowKeepsHealthGates(t *testing.T) {
	for _, tc := range []struct {
		name       string
		version    string
		elapsed    time.Duration
		seenAgo    time.Duration
		failed     bool
		wantHalted bool
	}{
		{name: "inactive old process during restart", version: "v1.4.0-kafka", elapsed: time.Minute},
		{name: "restart at deadline", version: "v1.4.0-kafka", elapsed: 5 * time.Minute},
		{name: "restart beyond deadline", version: "v1.4.0-kafka", elapsed: 5*time.Minute + time.Second, wantHalted: true},
		{name: "target process stops during bake", version: "v1.5.0-kafka", elapsed: time.Minute, wantHalted: true},
		{name: "host rollback during restart", version: "v1.4.0-kafka", elapsed: time.Minute, failed: true, wantHalted: true},
		{name: "stale before admission", version: "v1.4.0-kafka", elapsed: time.Minute, seenAgo: time.Hour, wantHalted: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newRolloutFixture(t, 1, 0)
			ctx := context.Background()
			_, err := f.svc.StartRollout(ctx, "v1.5.0", nil, models.FleetRolloutPolicy{DeadlineSeconds: 300})
			if err != nil {
				t.Fatal(err)
			}
			n := &f.nodes.nodes[0]
			n.Version = tc.version
			n.Active = false
			seen := f.now.Add(-tc.seenAgo)
			n.LastSeenAt = &seen
			if tc.failed {
				n.UpdateFailed = "v1.5.0-kafka"
			}
			f.advanceTime(tc.elapsed)
			st, err := f.svc.AdvanceRollout(ctx, false)
			if err != nil {
				t.Fatal(err)
			}
			if halted := st.Rollout.Stage == models.FleetRolloutHalted; halted != tc.wantHalted {
				t.Fatalf("halted=%t, want %t: %+v", halted, tc.wantHalted, st.Rollout)
			}
			if tc.wantHalted {
				return
			}
			n.Active = true
			f.advanceTime(time.Second)
			f.converge(st.Rollout.Admitted, "v1.5.0-kafka", time.Second)
			st, err = f.svc.AdvanceRollout(ctx, false)
			if err != nil || st.Rollout.Stage != models.FleetRolloutCanary {
				t.Fatalf("a returned node still needs to bake: state=%+v err=%v", st, err)
			}
			f.advanceTime(31 * time.Minute)
			f.converge(st.Rollout.Admitted, "v1.5.0-kafka", 31*time.Minute)
			st, err = f.svc.AdvanceRollout(ctx, false)
			if err != nil || st.Rollout != nil || st.Tag != "v1.5.0" {
				t.Fatalf("a returned and baked node should finish: state=%+v err=%v", st, err)
			}
		})
	}
}
