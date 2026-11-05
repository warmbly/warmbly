package repository

import (
	"math/rand"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/warmbly/warmbly/internal/models"
)

// FindRoutedPairs skips hydrating a lead when its facts say route() cannot
// offer it (mayBeDue) or cannot offer or wake for it (inert). Those skips are
// only safe if they never disagree with route() itself, so this drives both
// over random flows and leads and fails on any lead the skip would lose.
func TestRoutedPairsPruneNeverDropsWhatRouteWouldOffer(t *testing.T) {
	rng := rand.New(rand.NewSource(993))
	now := time.Now()
	conditions := [][]models.BranchCondition{
		nil,
		{{Field: "opened", Operator: "within_days", Value: intPtr(1)}},
		{{Field: "not_opened", Operator: "within_days", Value: intPtr(3)}},
		{{Field: "clicked", Operator: "ever"}},
		{{Field: "replied", Operator: "within_days", Value: intPtr(2)}},
		{{Field: "not_replied", Operator: "within_days", Value: intPtr(1)}},
		{{Field: "reply_positive", Operator: "ever"}},
	}
	ago := func(maxHours int) *time.Time {
		at := now.Add(-time.Duration(rng.Intn(maxHours*60+1)) * time.Minute)
		return &at
	}
	maybe := func(at *time.Time) *time.Time {
		if rng.Intn(2) == 0 {
			return nil
		}
		return at
	}

	for i := 0; i < 20000; i++ {
		n := 1 + rng.Intn(5)
		ids := make([]uuid.UUID, n)
		for j := range ids {
			ids[j] = uuid.New()
		}
		pick := func() uuid.UUID {
			if rng.Intn(8) == 0 {
				return uuid.New() // a deleted step
			}
			return ids[rng.Intn(n)]
		}
		cr := &campaignRouter{
			idxByID:           map[uuid.UUID]int{},
			replyFlowSteps:    map[uuid.UUID]bool{},
			now:               now,
			dueBy:             now.Add(time.Minute),
			stopOnReply:       rng.Intn(2) == 0,
			stopOnMeeting:     rng.Intn(2) == 0,
			entryDelay:        []time.Duration{0, 0, time.Hour, 72 * time.Hour}[rng.Intn(4)],
			campaignCreatedAt: now.Add(-48 * time.Hour),
		}
		for j, id := range ids {
			s := routeStep{id: id, kind: []string{"email", "email", "action"}[rng.Intn(3)], waitAfter: rng.Intn(4)}
			if s.kind == "action" && rng.Intn(2) == 0 {
				s.waitMinutes = 30 + rng.Intn(600)
			}
			for b := rng.Intn(4); b > 0; b-- {
				br := models.Branch{BranchID: uuid.NewString(), Conditions: conditions[rng.Intn(len(conditions))]}
				if rng.Intn(5) > 0 {
					target := pick()
					br.TargetSequenceID = &target
				}
				switch rng.Intn(3) {
				case 1:
					v := true
					br.Instant = &v
				case 2:
					v := false
					br.Instant = &v
				}
				s.bc.Branches = append(s.bc.Branches, br)
			}
			cr.steps = append(cr.steps, s)
			cr.idxByID[id] = j
			if rng.Intn(3) == 0 {
				cr.replyFlowSteps[id] = true
			}
		}
		cr.entry = ids[0]
		floors := cr.dueFloors()

		in := routeInput{addedAt: maybe(ago(96)), hasReplied: rng.Intn(3) == 0, hasBooked: rng.Intn(3) == 0}
		if rng.Intn(4) > 0 {
			last := pick()
			in.lastSeq = &last
			in.sentAt = ago(24 * 6)
			in.openedAt, in.clickedAt, in.repliedAt = maybe(ago(48)), maybe(ago(48)), maybe(ago(48))
			in.replyClass = []string{"", "positive", "negative"}[rng.Intn(3)]
		}
		for k := rng.Intn(n + 1); k > 0; k-- {
			in.sentIDs = append(in.sentIDs, pick())
		}
		switch rng.Intn(4) {
		case 1:
			in.pausedAt = ago(72)
		case 2:
			in.pausedAt = ago(72)
			until := now.Add(time.Duration(rng.Intn(96)-48) * time.Hour)
			in.pausedUntil = &until
		}

		f := leadFacts{addedAt: in.addedAt}
		if in.lastSeq != nil {
			f.lastAt = in.sentAt
			f.lastSeqs = []uuid.UUID{*in.lastSeq}
			if rng.Intn(4) == 0 {
				// A tie on sent_at: hydration may read either step.
				f.lastSeqs = append(f.lastSeqs, pick())
			}
		}

		res := cr.route(uuid.New(), uuid.New(), in)
		heldForever := res.Hold != nil && res.Hold.Until == nil
		offered := !heldForever && res.WaitUntil == nil && res.Target != nil &&
			(res.DueAt == nil || !res.DueAt.After(cr.dueBy))
		if offered && !cr.mayBeDue(f, floors) {
			t.Fatalf("case %d: route offers %v (due %v) but mayBeDue is false", i, *res.Target, res.DueAt)
		}
		for _, excludeNew := range []bool{false, true} {
			if !cr.inert(f, excludeNew) {
				continue
			}
			if in.lastSeq == nil {
				if !excludeNew {
					t.Fatalf("case %d: a new lead is inert while new leads are allowed", i)
				}
				continue
			}
			if res.Target != nil || res.WaitUntil != nil || res.Hold != nil {
				t.Fatalf("case %d: inert lead routes to target=%v wait=%v hold=%v", i, res.Target, res.WaitUntil, res.Hold)
			}
		}
	}
}

// The floor is the least delay any branch can carry: an instant branch drops
// the target's wait, and a wait node adds its own minutes to every route out.
func TestRoutedPairsDueFloors(t *testing.T) {
	a, b, c, end := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	instant := true
	cr := &campaignRouter{
		steps: []routeStep{
			{id: a, kind: "email", bc: models.BranchConditions{Branches: []models.Branch{
				{TargetSequenceID: &b},
				{TargetSequenceID: &c, Conditions: []models.BranchCondition{{Field: "opened", Operator: "ever"}}, Instant: &instant},
			}}},
			{id: b, kind: "action", waitAfter: 3, waitMinutes: 90, bc: models.BranchConditions{Branches: []models.Branch{{TargetSequenceID: &end}}}},
			{id: c, kind: "email", waitAfter: 2, bc: models.BranchConditions{Branches: []models.Branch{{TargetSequenceID: nil}}}},
			{id: end, kind: "email", waitAfter: 1},
		},
		idxByID: map[uuid.UUID]int{a: 0, b: 1, c: 2, end: 3},
	}
	floors := cr.dueFloors()
	if got := floors[a]; got != 0 {
		t.Fatalf("floor(a) = %v, want 0 (instant branch)", got)
	}
	if got, want := floors[b], 24*time.Hour+90*time.Minute; got != want {
		t.Fatalf("floor(b) = %v, want %v", got, want)
	}
	if _, ok := floors[c]; ok {
		t.Fatal("a step whose only branch stops has a floor")
	}
	if _, ok := floors[end]; ok {
		t.Fatal("a step with no branches has a floor")
	}
	if got := cr.endSteps(); len(got) != 1 || got[0] != end {
		t.Fatalf("endSteps = %v, want [%v]", got, end)
	}
}
