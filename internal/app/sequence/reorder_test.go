package sequence

import (
	"encoding/json"
	"testing"

	"github.com/google/uuid"

	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
)

func chainStep(id uuid.UUID, subject string, wait int, branches ...models.Branch) models.Sequence {
	raw, _ := json.Marshal(models.BranchConditions{Branches: branches})
	return models.Sequence{ID: id, Subject: subject, WaitAfter: wait, Kind: "email", Conditions: raw}
}

func to(id uuid.UUID, branchID string) models.Branch {
	return models.Branch{BranchID: branchID, TargetSequenceID: &id}
}

func TestBuildReorderSwapsALinearChainAndKeepsWaitsInPlace(t *testing.T) {
	a, b, c := uuid.New(), uuid.New(), uuid.New()
	steps := []models.Sequence{
		chainStep(a, "Intro", 0, to(b, "ab")),
		chainStep(b, "Bump", 3, to(c, "bc")),
		chainStep(c, "", 4),
	}
	updates, xerr := buildReorder(steps, []string{b.String(), a.String(), c.String()})
	if xerr != nil {
		t.Fatalf("reorder refused: %v", xerr)
	}
	want := []struct {
		id     uuid.UUID
		wait   int
		next   *uuid.UUID
		branch string
	}{{b, 0, &a, "bc"}, {a, 3, &c, "ab"}, {c, 4, nil, ""}}
	for i, w := range want {
		u := updates[i]
		var bc models.BranchConditions
		_ = json.Unmarshal(u.Conditions, &bc)
		if u.ID != w.id || u.Position != i+1 || u.WaitAfter != w.wait {
			t.Fatalf("slot %d = %v pos %d wait %d, want %v pos %d wait %d", i, u.ID, u.Position, u.WaitAfter, w.id, i+1, w.wait)
		}
		if w.next == nil {
			if len(bc.Branches) != 0 {
				t.Fatalf("last step kept a connection: %+v", bc.Branches)
			}
			continue
		}
		if len(bc.Branches) != 1 || *bc.Branches[0].TargetSequenceID != *w.next || bc.Branches[0].BranchID != w.branch {
			t.Fatalf("slot %d connections = %+v, want one to %v with id %q", i, bc.Branches, *w.next, w.branch)
		}
	}
}

func TestBuildReorderRefuses(t *testing.T) {
	a, b, c := uuid.New(), uuid.New(), uuid.New()
	linear := []models.Sequence{chainStep(a, "Intro", 0, to(b, "ab")), chainStep(b, "", 3, to(c, "bc")), chainStep(c, "Third", 4)}
	conditional := to(c, "if-opened")
	conditional.Conditions = []models.BranchCondition{{Field: "opened", Operator: "ever"}}
	cases := []struct {
		name  string
		steps []models.Sequence
		order []string
		want  *errx.Error
	}{
		{"a step missing", linear, []string{b.String(), a.String()}, errx.ErrSequenceOrder},
		{"a step twice", linear, []string{b.String(), a.String(), a.String()}, errx.ErrSequenceOrder},
		{"an unknown step", linear, []string{b.String(), a.String(), uuid.NewString()}, errx.ErrSequenceOrder},
		{"a condition", []models.Sequence{chainStep(a, "Intro", 0, conditional, to(b, "ab")), linear[1], linear[2]}, []string{b.String(), a.String(), c.String()}, errx.ErrSequenceOrderBranched},
		{"a detached step", []models.Sequence{chainStep(a, "Intro", 0, to(b, "ab")), chainStep(b, "Bump", 3), chainStep(c, "Third", 4)}, []string{a.String(), c.String(), b.String()}, errx.ErrSequenceOrderBranched},
		{"a jump past the next step", []models.Sequence{chainStep(a, "Intro", 0, to(c, "ac")), chainStep(b, "Bump", 3, to(c, "bc")), chainStep(c, "Third", 4)}, []string{b.String(), a.String(), c.String()}, errx.ErrSequenceOrderBranched},
		{"a first email with no subject", linear, []string{b.String(), a.String(), c.String()}, errx.ErrSequenceOrderSubject},
	}
	for _, tc := range cases {
		if _, xerr := buildReorder(tc.steps, tc.order); xerr != tc.want {
			t.Errorf("%s: got %v, want %v", tc.name, xerr, tc.want)
		}
	}
}
