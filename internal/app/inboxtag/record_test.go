package inboxtag

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/warmbly/warmbly/internal/config"
)

// recordedResponses is where new recordings land, so the hand-checked
// responses.json is never rewritten by a run.
const recordedResponses = "responses_recorded.json"

// TestRecordFixtures asks the model once for every fixture that has no cached
// response and stores what came back, so a new fixture (a German reply, say)
// is pinned to the model's real answer rather than a guess. It spends money,
// so it runs only when asked:
//
//	INBOXTAG_RECORD=1 TYPESAFE_API_KEY=... go test ./internal/app/inboxtag -run TestRecordFixtures -v
//
// Set each new fixture's expect_kind and expect_intent to what it prints.
func TestRecordFixtures(t *testing.T) {
	if os.Getenv("INBOXTAG_RECORD") != "1" {
		t.Skip("set INBOXTAG_RECORD=1 and TYPESAFE_API_KEY to record fixture responses")
	}
	key := config.TypeSafeAPIKey()
	if key == "" {
		t.Fatal("TYPESAFE_API_KEY is required to record")
	}
	fx, responses := loadFixtures(t)

	path := filepath.Join("testdata", recordedResponses)
	recorded := map[string]Response{}
	if raw, err := os.ReadFile(path); err == nil {
		if err := json.Unmarshal(raw, &recorded); err != nil {
			t.Fatalf("parse %s: %v", path, err)
		}
	}

	client := NewClient(key)
	added := 0
	for _, f := range fx {
		if _, ok := responses[f.Name]; ok {
			continue
		}
		var langs []string
		if f.Language != "" {
			langs = []string{f.Language}
		}
		state := BuildState(f.Subject, f.Body, f.Previous, "", langs...)
		state.Language = LanguageHint(langs)
		resp, err := client.Ask(context.Background(), state, Questions())
		if err != nil {
			t.Fatalf("%s: %v", f.Name, err)
		}
		recorded[f.Name] = *resp
		added++
		d := Decide(resp.Answers, Facts{})
		fmt.Printf("%s: expect_kind %q (%.2f), expect_intent %q (%.2f)\n",
			f.Name, d.Kind, d.KindConfidence, d.Intent, d.IntentConfidence)
	}
	if added == 0 {
		t.Log("every fixture already has a response")
		return
	}
	raw, err := json.MarshalIndent(recorded, "", " ")
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	if err := os.WriteFile(path, append(raw, '\n'), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
	t.Logf("recorded %d responses into %s", added, path)
}
