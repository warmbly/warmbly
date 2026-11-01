package generation

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/openai/openai-go/v2"
	"github.com/openai/openai-go/v2/option"
)

func TestWarmupReviewUsesConfiguredModelAndRefusesIncompleteVerdicts(t *testing.T) {
	for _, tc := range []struct {
		name, response string
		valid          bool
	}{
		{"approved", `{"contains_pitch":false,"unnatural":false,"coherent":true,"confidence":0.95}`, true},
		{"missing_checks", `{"coherent":true,"confidence":0.95}`, false},
		{"null_check", `{"contains_pitch":null,"unnatural":false,"coherent":true,"confidence":0.95}`, false},
		{"invalid_confidence", `{"contains_pitch":false,"unnatural":false,"coherent":true,"confidence":2}`, false},
		{"refusal", `Unable to review`, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			thread := diagnosticFixture()
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var request struct {
					Model    string                     `json:"model"`
					Messages []struct{ Content string } `json:"messages"`
				}
				if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
					t.Error(err)
				}
				if r.URL.Path != "/chat/completions" || request.Model != "configured-model" || len(request.Messages) != 2 {
					t.Errorf("incorrect review request: %+v", request)
				} else if !strings.Contains(request.Messages[1].Content, thread.Messages[len(thread.Messages)-1]) {
					t.Error("review omitted the final turn")
				}
				w.Header().Set("Content-Type", "application/json")
				_ = json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"message": map[string]any{"role": "assistant", "content": tc.response}}}})
			}))
			defer server.Close()
			client := &GenerationClient{client: openai.NewClient(option.WithAPIKey("fixture"), option.WithBaseURL(server.URL+"/"), option.WithMaxRetries(0))}
			got, err := client.ReviewConversation(t.Context(), thread, "configured-model")
			if (err == nil) != tc.valid || tc.valid && (got == nil || !got.Coherent || got.Confidence != 0.95) {
				t.Fatalf("unexpected verdict: %+v, %v", got, err)
			}
		})
	}
}
