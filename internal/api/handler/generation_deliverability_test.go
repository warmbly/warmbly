package handler

import (
	"context"
	"strings"
	"testing"

	"github.com/warmbly/warmbly/internal/pkg/generation"
)

// rewordProvider answers every completion with reply and counts the calls.
type rewordProvider struct {
	generation.Provider
	reply string
	calls int
	got   string
}

func (p *rewordProvider) Complete(_ context.Context, req generation.CompletionRequest) (*generation.WritingResult, error) {
	p.calls++
	p.got = req.Prompt
	return &generation.WritingResult{Text: p.reply, TokensUsed: 7}, nil
}

func TestRewordFlaggedCleansWhatTheScoreFlags(t *testing.T) {
	draft := "Subject: a cheap check\n\nHi {{.FirstName}}, one cheap check before {{.Company}} sends?!"
	cases := []struct {
		name, source, reply, want string
		calls                     int
	}{
		{"rewords the flagged terms", "", "Subject: a quick check\n\nHi {{.FirstName}}, one quick check before {{.Company}} sends?", "Subject: a quick check\n\nHi {{.FirstName}}, one quick check before {{.Company}} sends?", 1},
		{"keeps the draft when a merge field is lost", "", "Subject: a quick check\n\nHi there, one quick check before sending?", draft, 1},
		{"keeps the draft when nothing improved", "", draft, draft, 1},
		{"leaves the author's own words alone", "cheap check, sends?!", "", draft, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p := &rewordProvider{reply: tc.reply}
			h := &Handler{AIProvider: p}
			got, _, _ := h.rewordFlagged(context.Background(), "m", generation.VoiceContext{}, draft, tc.source, nil)
			if got != tc.want || p.calls != tc.calls {
				t.Fatalf("got %q after %d calls, want %q after %d", got, p.calls, tc.want, tc.calls)
			}
			if tc.calls > 0 && !strings.Contains(p.got, "cheap") {
				t.Fatalf("the fix prompt does not name the flagged term:\n%s", p.got)
			}
		})
	}
}

func TestEveryWritingPromptCarriesTheDeliverabilityRules(t *testing.T) {
	for name, build := range map[string]func(generation.VoiceContext) string{
		"voice": generation.BuildVoiceRules, "compose": generation.BuildComposeRules, "edit": generation.BuildEditRules,
		"agent": generation.BuildAgentVoiceRules, "snippet": generation.BuildInlineSnippetRules, "reply": generation.BuildReplyRules,
	} {
		if p := build(generation.VoiceContext{}); !strings.Contains(p, "DELIVERABILITY") || !strings.Contains(p, "cheap") {
			t.Errorf("%s prompt lacks the deliverability rules", name)
		}
	}
}
