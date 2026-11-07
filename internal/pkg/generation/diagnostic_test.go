package generation

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/openai/openai-go/v2"
	"github.com/openai/openai-go/v2/option"
)

func diagnosticFixture() Conversation {
	return Conversation{Subject: "Paper notes again", Description: "I've gone back to paper notes for meetings and I'm remembering more. Do you still keep yours digital?", Messages: []string{"Digital, mostly, though I copy the action items onto a card.", "That's a neat middle ground. Thanks,"}}
}

func TestCanonicalIdentityPreservesPunctuationAndUnicode(t *testing.T) {
	for _, name := range []string{"Support <EMEA>", "Árvíztűrő {Ops} \"Team\"", "Support!!!"} {
		body, err := RenderCanonicalTurn("This hypothetical example remains closed.", name)
		if err != nil || !strings.HasSuffix(body, "\n"+name) {
			t.Fatalf("identity %q changed: %q, %v", name, body, err)
		}
	}
	for _, name := range []string{"Team\rBcc:x@y.test", "Team\nInjected", "Team\x00Bad"} {
		if _, err := RenderCanonicalTurn("Hypothetical example.", name); err == nil {
			t.Fatal("accepted identity control character")
		}
	}
	for _, body := range []string{"<b>Example</b>", "Example {{Sender}}"} {
		if _, err := RenderCanonicalTurn(body, "Support <EMEA>"); err == nil {
			t.Fatal("accepted markup in generated text")
		}
	}
}

func TestDiagnosticRenderingPreservesCanonicalFactsAndRoles(t *testing.T) {
	c := diagnosticFixture()
	rendered, err := RenderDiagnostic(c, [2]string{"Ada A", "Ben B"})
	if err != nil {
		t.Fatal(err)
	}
	if rendered.Subject != c.Subject || rendered.Description != c.Description+"\n\nAda A" {
		t.Fatalf("opening or subject altered: %+v", rendered)
	}
	for i, body := range c.Messages {
		name, sep := "Ben B", "\n\n"
		if i%2 == 1 {
			name = "Ada A"
		}
		if strings.HasSuffix(body, ",") {
			sep = "\n"
		}
		if rendered.Messages[i] != body+sep+name || StripCanonicalSignature(rendered.Messages[i], name) != body {
			t.Fatalf("turn %d altered: %s", i, rendered.Messages[i])
		}
	}
	if c.Messages[0] != "Digital, mostly, though I copy the action items onto a card." {
		t.Fatal("input mutated")
	}
}

func TestDiagnosticRejectsBrokenRenderedContract(t *testing.T) {
	for _, mutate := range []func(*Conversation){
		func(c *Conversation) { c.Subject += "\nBcc: other@example.test" },
		func(c *Conversation) { c.Messages[0] = "" },
		func(c *Conversation) { c.Messages[0] = "{14:00|15:00}" },
		func(c *Conversation) { c.Messages[1] = "And tomorrow?" },
	} {
		c := diagnosticFixture()
		mutate(&c)
		if err := ValidateDiagnostic(c); err == nil {
			t.Fatalf("accepted invalid contract: %+v", c)
		}
	}
	if _, err := RenderDiagnostic(diagnosticFixture(), [2]string{"Alice\nImpersonator", "Ben"}); err == nil {
		t.Fatal("invalid sender accepted")
	}
}

func TestSelfDescribingWordingNeverBecomesFreshSupply(t *testing.T) {
	c := diagnosticFixture()
	if err := ValidateFreshWarmup(c); err != nil {
		t.Fatalf("ordinary thread rejected: %v", err)
	}
	c.Subject = "Simulated diagnostic: date example"
	c.Description = "Simulated diagnostic. In this hypothetical example, a date stays unchanged."
	if _, err := RenderDiagnostic(c, [2]string{"Ada", "Ben"}); err != nil {
		t.Fatalf("historical thread cannot be read: %v", err)
	}
	for _, opening := range []string{
		"Simulated diagnostic. In this hypothetical example, a date stays unchanged.",
		"An automated warmup exchange about planning.",
		"This warm-up note is about planning.",
		"Quick test email about planning, nothing else.",
		"A fictional date is the topic here.",
	} {
		c.Subject, c.Description = "Planning notes", opening
		if err := ValidateFreshWarmup(c); err == nil {
			t.Fatalf("self-describing opening accepted: %q", opening)
		}
	}
}

func TestConfiguredModelVisibilityDoesNotInventFallback(t *testing.T) {
	for _, available := range []bool{true, false} {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/models/stored-model" {
				t.Errorf("unexpected model lookup: %s", r.URL.Path)
			}
			w.Header().Set("Content-Type", "application/json")
			if !available {
				w.WriteHeader(http.StatusNotFound)
				_, _ = w.Write([]byte(`{"error":{"message":"sensitive-provider-detail"}}`))
				return
			}
			_, _ = w.Write([]byte(`{"id":"stored-model","object":"model","created":1,"owned_by":"provider"}`))
		}))
		client := &GenerationClient{client: openai.NewClient(option.WithAPIKey("local-fixture"), option.WithBaseURL(server.URL+"/"), option.WithMaxRetries(0))}
		err := client.CheckModel(context.Background(), "stored-model")
		server.Close()
		if available && err != nil {
			t.Fatal(err)
		}
		if !available && (err == nil || strings.Contains(err.Error(), "sensitive-provider-detail")) {
			t.Fatalf("unavailable model or error privacy incorrect: %v", err)
		}
	}
}

func TestGenerateConversationChecksConfiguredModelAndCanonicalTurns(t *testing.T) {
	for _, wrongCount := range []bool{false, true} {
		calls := 0
		fixture := diagnosticFixture()
		if wrongCount {
			fixture.Messages = append(fixture.Messages, "This remains closed.")
		}
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			if r.URL.Path == "/models/stored-model" {
				_, _ = w.Write([]byte(`{"id":"stored-model","object":"model","created":1,"owned_by":"provider"}`))
				return
			}
			if r.URL.Path != "/chat/completions" {
				t.Errorf("unexpected provider endpoint %s", r.URL.Path)
				w.WriteHeader(404)
				return
			}
			calls++
			var request struct {
				Model    string `json:"model"`
				Messages []struct {
					Content string `json:"content"`
				} `json:"messages"`
			}
			if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
				t.Error(err)
				w.WriteHeader(400)
				return
			}
			if request.Model != "stored-model" || len(request.Messages) == 0 || !strings.Contains(request.Messages[0].Content, "exactly 2") || !strings.Contains(request.Messages[0].Content, "Never mention or hint that the mail is automated") {
				t.Error("configured model, turn count or natural-mail contract changed")
			}
			body, _ := json.Marshal(fixture)
			_ = json.NewEncoder(w).Encode(map[string]any{"id": "fixture-chat", "object": "chat.completion", "created": 1, "model": "stored-model", "choices": []any{map[string]any{"index": 0, "message": map[string]any{"role": "assistant", "content": string(body)}, "finish_reason": "stop"}}})
		}))
		client := &GenerationClient{client: openai.NewClient(option.WithAPIKey("local-fixture"), option.WithBaseURL(server.URL+"/"), option.WithMaxRetries(0))}
		got, err := client.GenerateConversation(context.Background(), "fictional date", "stored-model", 2)
		server.Close()
		if calls != 1 {
			t.Fatalf("provider completion calls: %d", calls)
		}
		if wrongCount {
			if err == nil {
				t.Fatal("configured reply count ignored")
			}
			continue
		}
		if err != nil {
			t.Fatal(err)
		}
		rendered, err := RenderDiagnostic(*got, [2]string{"Ada A", "Ben B"})
		if err != nil || rendered.Subject != fixture.Subject || StripCanonicalSignature(rendered.Messages[0], "Ben B") != fixture.Messages[0] {
			t.Fatalf("AI rendered facts, subject or roles changed: %v", err)
		}
	}
}

func TestOutgoingWarmupRejectsHistoricalWordingInAnyTurn(t *testing.T) {
	for _, text := range []string{"Simulated diagnostic: clock", "A reply. SIMULATED DIAGNOSTIC.", "A reply. Simulated\n\tdiagnostic.", "Part of our automated warmup."} {
		if err := ValidateWarmupWording("A topic", text); err == nil {
			t.Fatalf("self-describing wording remained sendable: %q", text)
		}
	}
	if err := ValidateWarmupWording("Weekend plans", "We tested a new trail and it was lovely."); err != nil {
		t.Fatalf("ordinary wording refused: %v", err)
	}
}
