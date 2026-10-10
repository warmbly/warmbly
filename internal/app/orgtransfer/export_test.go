package orgtransfer

import (
	"encoding/json"
	"slices"
	"testing"
)

func TestCampaignReplayIntentResetsOnWorkspaceImport(t *testing.T) {
	for _, tbl := range Tables {
		if tbl.Name == "campaign_tasks" {
			if !slices.Contains(tbl.ResetOnImport, "dispatch_intent") {
				t.Fatal("workspace import grants source-instance replay intent")
			}
			return
		}
	}
	t.Fatal("campaign tasks missing from portable data registry")
}

// An archive is a customer download. The risk verdict and the evidence behind
// it are operator-facing, so they must not ride along in the manifest, and the
// same list must refuse them on the way back in.
func TestOrgRowStripsTheOperatorVerdict(t *testing.T) {
	row := map[string]json.RawMessage{
		"name":                 json.RawMessage(`"Acme"`),
		"risk_state":           json.RawMessage(`"suspended"`),
		"risk_score":           json.RawMessage(`90`),
		"risk_reason":          json.RawMessage(`"confirmed abuse"`),
		"risk_signals":         json.RawMessage(`{"signup":{"weight":35}}`),
		"risk_evaluated_at":    json.RawMessage(`"2026-01-01T00:00:00Z"`),
		"risk_override":        json.RawMessage(`"trusted"`),
		"risk_override_reason": json.RawMessage(`"reviewed, ticket 4471"`),
		"risk_override_by":     json.RawMessage(`"00000000-0000-0000-0000-000000000001"`),
		"risk_override_at":     json.RawMessage(`"2026-01-01T00:00:00Z"`),
	}

	out := toAnyMap(row)
	if out["name"] != "Acme" {
		t.Errorf("ordinary columns must still travel, got %v", out["name"])
	}
	for name := range OrgRiskColumns {
		if _, present := out[name]; present {
			t.Errorf("%s reached the manifest", name)
		}
		if !orgMergeExcluded[name] {
			t.Errorf("%s is stripped on export but would be accepted on import", name)
		}
	}
	if len(out) != 1 {
		t.Errorf("manifest carries %d columns, want only the ordinary one", len(out))
	}
}

func TestArchiveCannotSetTestWorkspaceCategory(t *testing.T) {
	if !orgMergeExcluded["category"] {
		t.Fatal("workspace imports must not overwrite the instance's test classification")
	}
}
