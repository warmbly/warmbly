package handler

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/warmbly/warmbly/internal/api/middleware"
	"github.com/warmbly/warmbly/internal/app/credits"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/pkg/generation"
)

// streamProvider writes deltas through CompleteStream, then fails with err when set.
type streamProvider struct {
	editProvider
	deltas []string
	err    error
}

func (p *streamProvider) CompleteStream(ctx context.Context, req generation.CompletionRequest, onDelta func(string)) (*generation.WritingResult, error) {
	p.got = req
	for _, d := range p.deltas {
		onDelta(d)
	}
	if p.err != nil {
		return nil, p.err
	}
	return &generation.WritingResult{Text: strings.Join(p.deltas, ""), Model: "test-model", TokensUsed: 9}, nil
}

type countingCredits struct {
	editCredits
	charges int
}

func (r *countingCredits) Charge(context.Context, uuid.UUID, credits.Usage, string, string) (models.Micro, error) {
	r.charges++
	return 20_000, nil
}

func streamEdit(t *testing.T, h *Handler, ctx context.Context) []writingEvent {
	t.Helper()
	gin.SetMode(gin.TestMode)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Set(middleware.OrganizationIDKey, uuid.New())
	c.Request = httptest.NewRequestWithContext(ctx, http.MethodPost, "/generation/edit",
		strings.NewReader(`{"text":"Hi {{.FirstName}}, quick one.","instruction":"shorten"}`))
	c.Request.Header.Set("Content-Type", "application/json")
	c.Request.Header.Set("Accept", "text/event-stream")

	h.GenerateEdit(c)

	if ct := rec.Header().Get("Content-Type"); ct != "text/event-stream" {
		t.Fatalf("content type %q: %s", ct, rec.Body.String())
	}
	var events []writingEvent
	for _, frame := range strings.Split(strings.TrimSpace(rec.Body.String()), "\n\n") {
		var ev writingEvent
		if err := json.Unmarshal([]byte(strings.TrimPrefix(frame, "data: ")), &ev); err != nil {
			t.Fatalf("frame %q: %v", frame, err)
		}
		events = append(events, ev)
	}
	return events
}

func TestGenerateEditStreamsTheTextAsItIsWritten(t *testing.T) {
	p := &streamProvider{deltas: []string{"Hi {{.FirstName}}", ", one ", "question."}}
	h := &Handler{FeatureGateService: editGate{}, CreditService: editCredits{}, AIProvider: p}

	events := streamEdit(t, h, context.Background())

	if len(events) != 4 {
		t.Fatalf("want 3 deltas and done, got %+v", events)
	}
	for i, d := range p.deltas {
		if events[i].Type != "delta" || events[i].Text != d {
			t.Errorf("event %d = %+v, want delta %q", i, events[i], d)
		}
	}
	done := events[3]
	if done.Type != "done" || done.Text != "Hi {{.FirstName}}, one question." || done.CreditsCharged == nil || *done.CreditsCharged != 1 ||
		done.ChargedUSD == nil || *done.ChargedUSD != 0.02 || done.BalanceUSD == nil || *done.BalanceUSD != 0.82 {
		t.Errorf("unexpected done event: %+v", done)
	}
	if p.got.System != generation.BuildEditRules(generation.VoiceContext{}) {
		t.Error("the streamed edit is not running on the edit rules")
	}
}

func TestStreamedFailureChargesNothing(t *testing.T) {
	cases := []struct {
		name   string
		deltas []string
		cancel bool
	}{
		{"provider failed mid-stream", []string{"Hi"}, false},
		{"caller left after reading", []string{"Hi"}, true},
		{"caller left before any text", nil, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if tc.cancel {
				cancel()
			}
			credits := &countingCredits{}
			p := &streamProvider{deltas: tc.deltas, err: errors.New("boom")}
			h := &Handler{FeatureGateService: editGate{}, CreditService: credits, AIProvider: p}

			events := streamEdit(t, h, ctx)

			if last := events[len(events)-1]; last.Type != "error" {
				t.Errorf("last event %+v, want error", last)
			}
			if credits.charges != 0 {
				t.Errorf("a failed generation was charged %d times", credits.charges)
			}
		})
	}
}
