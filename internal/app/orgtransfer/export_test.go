package orgtransfer

import (
	"context"
	"encoding/json"
	"reflect"
	"slices"
	"testing"
)

func TestMailTransportExportJSONExclusionPreservesEvidence(t *testing.T) {
	row := []byte(`{"registered":{"transport_generation":"proof","task_id":"kept","success":false,"message_id":"accepted","error":{"code":"KEPT","message":"transport_generation is quoted text","details":[{"transport_generation":"nested","count":9007199254740993},null,true,"transport_generation"]},"future":[{"result":{"transport_generation":"proof","sent_at":"kept"}}]},"unregistered":{"transport_generation":"user-content"},"historical":"kept"}`)
	out, err := (&service{}).exportRow(context.Background(), &Table{Name: "fixture", ExcludeJSONOnExport: []JSONColumnExclusion{{Column: "registered", Keys: []string{"transport_generation"}}}}, row, nil, nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	var actual map[string]json.RawMessage
	if err := json.Unmarshal(out, &actual); err != nil {
		t.Fatal(err)
	}
	expected := []byte(`{"task_id":"kept","success":false,"message_id":"accepted","error":{"code":"KEPT","message":"transport_generation is quoted text","details":[{"count":9007199254740993},null,true,"transport_generation"]},"future":[{"result":{"sent_at":"kept"}}]}`)
	assertExportJSONEqual(t, actual["registered"], expected)
	assertExportJSONEqual(t, actual["unregistered"], []byte(`{"transport_generation":"user-content"}`))
	for _, raw := range []string{"null", "[]", `"ordinary scalar"`, "9007199254740993"} {
		out, err := excludeExportJSONKeys(json.RawMessage(raw), []string{"transport_generation"})
		if err != nil || string(out) != raw {
			t.Fatal("scalar/null/array changed", raw, err)
		}
	}
	for table, column := range map[string]string{"tasks": "send_executor_result", "warmup_tasks": "dispatch_result", "send_recovery_resolutions": "confirmed_result"} {
		if !reflect.DeepEqual(TableByName[table].ExcludeJSONOnExport, []JSONColumnExclusion{{Column: column, Keys: []string{"transport_generation"}}}) {
			t.Fatal("missing or broad JSON exclusion", table)
		}
	}
}

func TestMailTransportExportExclusionIsRegistryDirected(t *testing.T) {
	row := []byte(`{"id":"portable","local_only":"do-not-export","worker_id":"source-worker","historical":"preserved"}`)
	out, err := (&service{}).exportRow(context.Background(), &Table{Name: "fixture", ExcludeOnExport: []string{"local_only"}, ResetOnImport: []string{"worker_id"}}, row, nil, nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	var fields map[string]string
	if err := json.Unmarshal(out, &fields); err != nil {
		t.Fatal(err)
	}
	if _, ok := fields["local_only"]; ok {
		t.Fatal("configured excluded field exported")
	}
	if len(fields) != 3 || fields["worker_id"] != "source-worker" || fields["historical"] != "preserved" || fields["id"] != "portable" {
		t.Fatal("exclusion changed unrelated/history/reset-on-import fields")
	}
	mailbox := TableByName["email_accounts"]
	if !slices.Equal(mailbox.ExcludeOnExport, []string{"transport_generation"}) || !slices.Contains(mailbox.ResetOnImport, "transport_generation") || !slices.Contains(mailbox.ResetColumnsOnOverwrite, "transport_generation") {
		t.Fatal("generation export/import policy incomplete")
	}
}

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
