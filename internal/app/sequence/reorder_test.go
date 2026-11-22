package sequence

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/google/uuid"

	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/repository"
)

// mockSequenceRepo is an in-memory SequenceRepository for reorder tests. It
// records the updates the service asks the repository to write so the tests can
// assert on the repositioning, catch-all reconnection and branch preservation.
type mockSequenceRepo struct {
	repository.SequenceRepository
	orgID     string
	campaign  string
	steps     []models.Sequence
	reordered bool
	updates   []repository.SequenceReorder
}

func (r *mockSequenceRepo) Get(_ context.Context, orgID, campaign string) ([]models.Sequence, *errx.Error) {
	if orgID != r.orgID || campaign != r.campaign {
		return nil, errx.ErrNotFound
	}
	out := make([]models.Sequence, len(r.steps))
	copy(out, r.steps)
	return out, nil
}

func (r *mockSequenceRepo) Reorder(_ context.Context, orgID, campaign string, plan repository.ReorderPlan) (*models.Sequence, *errx.Error) {
	if orgID != r.orgID || campaign != r.campaign {
		return nil, errx.ErrNotFound
	}
	// Read the steps under the same lock the real repository takes, then run the
	// service's plan against that snapshot.
	snapshot := make([]models.Sequence, len(r.steps))
	copy(snapshot, r.steps)
	updates, xerr := plan(snapshot)
	if xerr != nil {
		return nil, xerr
	}
	r.reordered = true
	r.updates = updates

	// Return the step now at position 1, with its rewritten conditions, the way
	// the real UPDATE ... RETURNING does.
	var entry models.Sequence
	for _, u := range updates {
		if u.Position != 1 {
			continue
		}
		for _, st := range snapshot {
			if st.ID == u.ID {
				entry = st
				entry.Position = 1
				entry.Conditions = u.Conditions
			}
		}
	}
	return &entry, nil
}

func uuidPtr(u uuid.UUID) *uuid.UUID { return &u }

func step(id string, position int, conditions *models.BranchConditions) models.Sequence {
	s := models.Sequence{ID: uuid.MustParse(id), Position: position}
	if conditions != nil {
		raw, _ := json.Marshal(conditions)
		s.Conditions = raw
	}
	return s
}

func branch(field string, target *uuid.UUID) models.Branch {
	return models.Branch{BranchID: field, TargetSequenceID: target, Conditions: []models.BranchCondition{{Field: field, Operator: "ever"}}}
}

func catchAll(target *uuid.UUID) models.Branch {
	return models.Branch{BranchID: "otherwise", TargetSequenceID: target, Conditions: nil}
}

func updatesByID(updates []repository.SequenceReorder) map[string]repository.SequenceReorder {
	m := make(map[string]repository.SequenceReorder, len(updates))
	for _, u := range updates {
		m[u.ID.String()] = u
	}
	return m
}

func positions(updates []repository.SequenceReorder) map[string]int {
	m := make(map[string]int, len(updates))
	for _, u := range updates {
		m[u.ID.String()] = u.Position
	}
	return m
}

func TestReorderRepositionsStepsInOrder(t *testing.T) {
	a, b, c := uuid.New(), uuid.New(), uuid.New()
	repo := &mockSequenceRepo{
		orgID: "org", campaign: "c",
		steps: []models.Sequence{
			step(a.String(), 1, nil),
			step(b.String(), 2, nil),
			step(c.String(), 3, nil),
		},
	}
	svc := NewService(repo)
	if _, xerr := svc.ReorderSteps(context.Background(), "org", "c", []string{b.String(), c.String(), a.String()}); xerr != nil {
		t.Fatalf("reorder failed: %v", xerr)
	}
	if !repo.reordered {
		t.Fatal("repo.Reorder was not called")
	}
	got := positions(repo.updates)
	want := map[string]int{b.String(): 1, c.String(): 2, a.String(): 3}
	for id, pos := range want {
		if got[id] != pos {
			t.Errorf("step %s: position %d, want %d", id, got[id], pos)
		}
	}
	// Positions must be gap-free, contiguous, duplicate-free.
	seen := map[int]bool{}
	for _, u := range repo.updates {
		if seen[u.Position] {
			t.Errorf("duplicate position %d", u.Position)
		}
		seen[u.Position] = true
	}
}

func TestReorderReconnectsCatchAllToNext(t *testing.T) {
	a, b, c := uuid.New(), uuid.New(), uuid.New()
	// A has a conditional branch (opened -> B) and a catch-all (-> C).
	cond := &models.BranchConditions{Branches: []models.Branch{
		branch("opened", uuidPtr(b)),
		catchAll(uuidPtr(c)),
	}}
	repo := &mockSequenceRepo{
		orgID: "org", campaign: "c",
		steps: []models.Sequence{
			step(a.String(), 1, cond),
			step(b.String(), 2, nil),
			step(c.String(), 3, nil),
		},
	}
	svc := NewService(repo)
	// New order: C, A, B.
	if _, xerr := svc.ReorderSteps(context.Background(), "org", "c", []string{c.String(), a.String(), b.String()}); xerr != nil {
		t.Fatalf("reorder failed: %v", xerr)
	}
	byID := updatesByID(repo.updates)

	// A is now at position 2, next is B (position 3). A's catch-all must point to B.
	aCond := parseConditions(t, byID[a.String()].Conditions)
	if len(aCond.Branches) != 2 {
		t.Fatalf("A should keep 2 branches, got %d", len(aCond.Branches))
	}
	var conditionalTarget, catchAllTarget *uuid.UUID
	for _, br := range aCond.Branches {
		if br.TargetSequenceID == nil {
			continue
		}
		if len(br.Conditions) > 0 {
			conditionalTarget = br.TargetSequenceID
		} else {
			catchAllTarget = br.TargetSequenceID
		}
	}
	if conditionalTarget == nil || *conditionalTarget != b {
		t.Errorf("A's conditional branch (opened -> B) was not preserved, conditional target = %v", conditionalTarget)
	}
	if catchAllTarget == nil || *catchAllTarget != b {
		t.Errorf("A's catch-all was not reconnected to the next step (B), got %v", catchAllTarget)
	}

	// C is now at position 1, next is A. C had no branches, so a catch-all -> A is added.
	cCond := parseConditions(t, byID[c.String()].Conditions)
	if len(cCond.Branches) != 1 || cCond.Branches[0].TargetSequenceID == nil || *cCond.Branches[0].TargetSequenceID != a {
		t.Errorf("C should gain a catch-all -> A, got %+v", cCond.Branches)
	}

	// B is last: no catch-all, flow ends.
	bCond := parseConditions(t, byID[b.String()].Conditions)
	if len(bCond.Branches) != 0 {
		t.Errorf("last step B should have no outgoing branch, got %+v", bCond.Branches)
	}
}

func TestReorderRejectsUnknownStep(t *testing.T) {
	a, b := uuid.New(), uuid.New()
	repo := &mockSequenceRepo{
		orgID: "org", campaign: "c",
		steps: []models.Sequence{step(a.String(), 1, nil), step(b.String(), 2, nil)},
	}
	svc := NewService(repo)
	_, xerr := svc.ReorderSteps(context.Background(), "org", "c", []string{a.String(), uuid.New().String()})
	if xerr == nil || xerr.Code != errx.NotFound {
		t.Fatalf("unknown step should be rejected with 404, got %v", xerr)
	}
	if repo.reordered {
		t.Fatal("repo.Reorder must not run when the order is invalid")
	}
}

func TestReorderRejectsDuplicateStep(t *testing.T) {
	a, b := uuid.New(), uuid.New()
	repo := &mockSequenceRepo{
		orgID: "org", campaign: "c",
		steps: []models.Sequence{step(a.String(), 1, nil), step(b.String(), 2, nil)},
	}
	svc := NewService(repo)
	_, xerr := svc.ReorderSteps(context.Background(), "org", "c", []string{a.String(), a.String()})
	if xerr == nil || xerr.Code != errx.BadRequest {
		t.Fatalf("duplicate step should be rejected with 400, got %v", xerr)
	}
	if repo.reordered {
		t.Fatal("repo.Reorder must not run when the order has duplicates")
	}
}

func TestReorderRejectsMissingStep(t *testing.T) {
	a, b, c := uuid.New(), uuid.New(), uuid.New()
	repo := &mockSequenceRepo{
		orgID: "org", campaign: "c",
		steps: []models.Sequence{step(a.String(), 1, nil), step(b.String(), 2, nil), step(c.String(), 3, nil)},
	}
	svc := NewService(repo)
	// Omits C.
	_, xerr := svc.ReorderSteps(context.Background(), "org", "c", []string{a.String(), b.String()})
	if xerr == nil || xerr.Code != errx.BadRequest {
		t.Fatalf("missing step should be rejected with 400, got %v", xerr)
	}
	if repo.reordered {
		t.Fatal("repo.Reorder must not run when the order omits a step")
	}
}

func TestReorderRejectsRoutingCycle(t *testing.T) {
	a, b, c := uuid.New(), uuid.New(), uuid.New()
	// A routes (conditionally) to C. Reordering to C, A, B would make C's
	// catch-all point to A and A's conditional branch point to C: a cycle.
	cond := &models.BranchConditions{Branches: []models.Branch{
		branch("opened", uuidPtr(c)),
	}}
	repo := &mockSequenceRepo{
		orgID: "org", campaign: "c",
		steps: []models.Sequence{
			step(a.String(), 1, cond),
			step(b.String(), 2, nil),
			step(c.String(), 3, nil),
		},
	}
	svc := NewService(repo)
	_, xerr := svc.ReorderSteps(context.Background(), "org", "c", []string{c.String(), a.String(), b.String()})
	if xerr == nil {
		t.Fatal("a reorder that creates a routing cycle must be rejected")
	}
	if repo.reordered {
		t.Fatal("repo.Reorder must not run when the reorder creates a cycle")
	}
}

func TestReorderOrgScoped(t *testing.T) {
	a, b := uuid.New(), uuid.New()
	repo := &mockSequenceRepo{
		orgID: "org", campaign: "c",
		steps: []models.Sequence{step(a.String(), 1, nil), step(b.String(), 2, nil)},
	}
	svc := NewService(repo)
	_, xerr := svc.ReorderSteps(context.Background(), "other-org", "c", []string{b.String(), a.String()})
	if xerr == nil || xerr.Code != errx.NotFound {
		t.Fatalf("reorder for another org must be rejected with 404, got %v", xerr)
	}
	if repo.reordered {
		t.Fatal("repo.Reorder must not run for another organization")
	}
}

func parseConditions(t *testing.T, raw json.RawMessage) models.BranchConditions {
	t.Helper()
	var bc models.BranchConditions
	if len(raw) == 0 {
		return bc
	}
	if err := json.Unmarshal(raw, &bc); err != nil {
		t.Fatalf("parse conditions: %v", err)
	}
	return bc
}
