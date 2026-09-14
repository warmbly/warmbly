package generation

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

// A model can refuse one parameter per 400, so a backend that dislikes all of
// them must still be walked to a shape that works. gpt-5.6-luna is the real
// case: it rejects max_tokens, then any non-default temperature, then refuses
// function tools unless reasoning is switched off. Before the retry budget
// covered every flag, the third refusal ended the call and the agent loop was
// unusable on that model.
func TestCompleteAdaptsEveryRejectedParam(t *testing.T) {
	var bodies []map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		var body map[string]any
		_ = json.Unmarshal(raw, &body)
		bodies = append(bodies, body)

		reject := func(param, msg string) {
			w.WriteHeader(http.StatusBadRequest)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"error": map[string]any{"type": "invalid_request_error", "param": param, "message": msg},
			})
		}
		switch {
		case body["max_tokens"] != nil:
			reject("max_tokens", "Unsupported parameter: 'max_tokens' is not supported with this model. Use 'max_completion_tokens' instead.")
		case body["temperature"] != nil:
			reject("temperature", "Unsupported value: 'temperature' does not support 0.7 with this model.")
		case body["tools"] != nil && body["reasoning_effort"] == nil:
			reject("reasoning_effort", "Function tools with reasoning_effort are not supported for this model in /v1/chat/completions. To use function tools, use /v1/responses or set reasoning_effort to 'none'.")
		default:
			_ = json.NewEncoder(w).Encode(map[string]any{
				"choices": []map[string]any{{"message": map[string]any{"role": "assistant", "content": "ok"}}},
			})
		}
	}))
	defer srv.Close()

	temp := 0.7
	p := &openAIProvider{apiKey: "k", baseURL: srv.URL, http: srv.Client()}
	tools := []oaiTool{{Type: "function"}}

	resp, err := p.complete(context.Background(), "gpt-5.6-luna", 64, []oaiMessage{{Role: "user", Content: "hi"}}, tools, &temp)
	if err != nil {
		t.Fatalf("complete after adaptation: %v", err)
	}
	if got := resp.Choices[0].Message.Content; got != "ok" {
		t.Fatalf("content = %q, want ok", got)
	}
	if len(bodies) != 4 {
		t.Fatalf("attempts = %d, want 4 (three rejections then success)", len(bodies))
	}
	final := bodies[len(bodies)-1]
	if final["max_tokens"] != nil {
		t.Error("final request still sent max_tokens")
	}
	if final["max_completion_tokens"] == nil {
		t.Error("final request did not send max_completion_tokens")
	}
	if final["temperature"] != nil {
		t.Error("final request still sent temperature")
	}
	if final["reasoning_effort"] != "none" {
		t.Errorf("final reasoning_effort = %v, want none", final["reasoning_effort"])
	}

	// The flags are sticky, so the next call starts in the working shape
	// rather than paying for the same three rejections again.
	before := len(bodies)
	if _, err := p.complete(context.Background(), "gpt-5.6-luna", 64, []oaiMessage{{Role: "user", Content: "hi"}}, tools, &temp); err != nil {
		t.Fatalf("second call: %v", err)
	}
	if got := len(bodies) - before; got != 1 {
		t.Fatalf("second call made %d requests, want 1", got)
	}
}

// A backend that keeps naming the same parameter must not be retried forever.
func TestAdaptParamsRefusesToRepeatAFlag(t *testing.T) {
	p := &openAIProvider{}
	e := &oaiError{Param: "reasoning_effort", Message: "set reasoning_effort to 'none'"}
	if !p.adaptParams(e) {
		t.Fatal("first reasoning_effort rejection should adapt")
	}
	if p.adaptParams(e) {
		t.Fatal("second identical rejection should not adapt again")
	}
}
