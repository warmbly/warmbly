package repository

import (
	"encoding/json"
	"slices"
	"testing"

	"github.com/google/uuid"
)

func TestScrubForeignStepReferences(t *testing.T) {
	cat, seg, pipe, stage, auto, who := uuid.New(), uuid.New(), uuid.New(), uuid.New(), uuid.New(), uuid.New()
	cases := []struct {
		name        string
		in          map[string]any
		wantCleared []string
		wantKept    map[string]any
		wantAllowed []string
	}{
		{
			name:        "tag action loses its tag",
			in:          map[string]any{"type": "add_tag", "category_id": cat.String()},
			wantCleared: []string{"category_id"},
			wantKept:    map[string]any{"type": "add_tag"},
		},
		{
			name: "task keeps its copy, loses its assignee",
			in: map[string]any{
				"type": "create_task", "task_title": "Call {{.FirstName}}", "task_priority": "high",
				"task_assigned_to": who.String(),
			},
			wantCleared: []string{"task_assigned_to"},
			wantKept:    map[string]any{"type": "create_task", "task_title": "Call {{.FirstName}}", "task_priority": "high"},
		},
		{
			name: "every workspace row is cleared",
			in: map[string]any{
				"type": "create_deal", "segment_id": seg.String(), "deal_pipeline_id": pipe.String(),
				"deal_stage_id": stage.String(), "automation_id": auto.String(), "label_ids": []string{cat.String()},
				"deal_name": "{{.Company}}",
			},
			wantCleared: []string{"segment_id", "label_ids", "deal_pipeline_id", "deal_stage_id", "automation_id"},
			wantKept:    map[string]any{"type": "create_deal", "deal_name": "{{.Company}}"},
		},
		{
			name: "an AI agent loses the tools that needed a cleared setting",
			in: map[string]any{
				"type": "ai_step", "ai_instruction": "Tag hot leads",
				"ai_allowed_actions": []string{"add_tag", "unsubscribe", "create_deal"},
				"ai_add_tags":        []map[string]string{{"id": cat.String(), "name": "Hot"}},
				"deal_pipeline_id":   pipe.String(), "deal_stage_id": stage.String(),
			},
			wantCleared: []string{"deal_pipeline_id", "deal_stage_id", "ai_add_tags", "ai_allowed_actions"},
			wantKept:    map[string]any{"type": "ai_step", "ai_instruction": "Tag hot leads"},
			wantAllowed: []string{"unsubscribe"},
		},
		{
			name:     "a wait carries nothing to clear",
			in:       map[string]any{"type": "wait", "wait_minutes": 60},
			wantKept: map[string]any{"type": "wait", "wait_minutes": float64(60)},
		},
		{
			name:     "empty placeholders are not reported",
			in:       map[string]any{"type": "notify", "label_ids": []string{}, "category_id": nil},
			wantKept: map[string]any{"type": "notify"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			raw, _ := json.Marshal(tc.in)
			out, cleared, err := ScrubForeignStepReferences(raw)
			if err != nil {
				t.Fatal(err)
			}
			slices.Sort(cleared)
			want := slices.Clone(tc.wantCleared)
			slices.Sort(want)
			if !slices.Equal(cleared, want) {
				t.Fatalf("cleared = %v, want %v", cleared, want)
			}
			var got map[string]any
			if err := json.Unmarshal(out, &got); err != nil {
				t.Fatal(err)
			}
			allowed, _ := got["ai_allowed_actions"].([]any)
			delete(got, "ai_allowed_actions")
			for k, v := range tc.wantKept {
				if g, ok := got[k]; !ok || g != v {
					t.Errorf("%s = %v, want %v", k, g, v)
				}
			}
			for _, k := range stepForeignRefs {
				if _, ok := got[k]; ok {
					t.Errorf("%s survived the scrub", k)
				}
			}
			if tc.wantAllowed != nil {
				var names []string
				for _, a := range allowed {
					names = append(names, a.(string))
				}
				if !slices.Equal(names, tc.wantAllowed) {
					t.Errorf("ai_allowed_actions = %v, want %v", names, tc.wantAllowed)
				}
			}
		})
	}
}

func TestScrubForeignStepReferencesEmpty(t *testing.T) {
	for _, in := range [][]byte{nil, []byte(`null`), []byte(`{}`)} {
		out, cleared, err := ScrubForeignStepReferences(in)
		if err != nil || len(cleared) != 0 || string(out) != `{}` {
			t.Fatalf("%q -> %q %v %v", in, out, cleared, err)
		}
	}
}
