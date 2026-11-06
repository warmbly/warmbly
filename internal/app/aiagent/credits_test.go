package aiagent

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"

	"github.com/warmbly/warmbly/internal/app/aitools"
	"github.com/warmbly/warmbly/internal/app/credits"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/pkg/generation"
)

type runCreditLedger struct {
	credits.CreditService
	balance         models.Micro
	allowed, checks int
	charges         []credits.Usage
	keys            []string
}

// Reserve refuses once the fixture's allowed number of iterations is used up.
func (l *runCreditLedger) Reserve(ctx context.Context, _ uuid.UUID, _ models.Micro) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	l.checks++
	if l.checks > l.allowed {
		return credits.ErrInsufficientCredits
	}
	return nil
}

// Charge records the usage and key, priced at one micro-dollar per token so no external price list is involved.
func (l *runCreditLedger) Charge(ctx context.Context, _ uuid.UUID, u credits.Usage, _, key string) (models.Micro, error) {
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	amount := models.Micro(u.InputTokens + u.OutputTokens)
	l.balance -= amount
	l.charges = append(l.charges, u)
	l.keys = append(l.keys, key)
	return amount, nil
}

func (l *runCreditLedger) GetBalance(context.Context, uuid.UUID) (models.Micro, *errx.Error) {
	return l.balance, nil
}

// TestRunMessageChargesReservedAnswerWithinBudget covers successful answers, a failed final turn, and a depleted balance.
func TestRunMessageChargesReservedAnswerWithinBudget(t *testing.T) {
	for _, tc := range []struct {
		name                            string
		allowed, wantRequests, wantRows int
		wantInput, wantOutput           int
		wantCode                        string
	}{
		{name: "answer", allowed: 50, wantRequests: 20, wantInput: 120, wantOutput: 80, wantRows: 40},
		{name: "empty_answer", allowed: 50, wantRequests: 20, wantInput: 120, wantOutput: 80, wantRows: 39, wantCode: "provider_error"},
		{name: "no_credit_for_answer", allowed: 19, wantRequests: 19, wantInput: 114, wantOutput: 76, wantRows: 39, wantCode: "insufficient_credits"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			requests := 0
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requests++
				message := map[string]any{"role": "assistant", "content": "The mailbox is paused."}
				if requests < 20 {
					message["content"], message["tool_calls"] = "", []any{map[string]any{
						"id": fmt.Sprint(requests), "type": "function", "function": map[string]string{"name": "read", "arguments": `{}`},
					}}
				} else if tc.name == "empty_answer" {
					message["content"] = ""
				}
				w.Header().Set("Content-Type", "application/json")
				_ = json.NewEncoder(w).Encode(map[string]any{
					"choices": []any{map[string]any{"message": message}}, "usage": map[string]int{"prompt_tokens": 6, "completion_tokens": 4, "total_tokens": 10},
				})
			}))
			defer srv.Close()
			provider, err := generation.NewProvider(generation.ProviderConfig{
				OpenAIAPIKey: "fixture", OpenAIBaseURL: srv.URL, OpenAIModelPaid: "fixture-model",
			})
			if err != nil {
				t.Fatal(err)
			}
			registry := aitools.NewRegistry()
			registry.Register(aitools.Tool{Name: "read", Risk: generation.RiskRead, Handler: func(context.Context, aitools.Invocation, json.RawMessage) (string, error) {
				return `{"status":"paused"}`, nil
			}})
			repo := &runRepository{session: models.AgentSession{ID: uuid.New(), OrgID: uuid.New(), UserID: uuid.New()}}
			const startBalance = models.Micro(1_000_000)
			ledger := &runCreditLedger{balance: startBalance, allowed: tc.allowed}
			svc := NewService(repo, registry, provider, ledger, runFeatureGate{}, nil, nil, nil, nil)
			var last StreamEvent
			if xerr := svc.RunMessage(context.Background(), aitools.Invocation{OrgID: repo.session.OrgID, UserID: repo.session.UserID}, repo.session.ID, "paid-message", "Inspect sending", "", "", func(ev StreamEvent) { last = ev }); xerr != nil {
				t.Fatal(xerr)
			}
			if requests != tc.wantRequests || ledger.checks != 20 {
				t.Fatalf("requests=%d balance checks=%d", requests, ledger.checks)
			}
			// One charge per run, keyed by the message, for every turn that reached the provider.
			if len(ledger.charges) != 1 || ledger.keys[0] != "paid-message" {
				t.Fatalf("charges=%+v keys=%v", ledger.charges, ledger.keys)
			}
			if got := ledger.charges[0]; got.InputTokens != tc.wantInput || got.OutputTokens != tc.wantOutput || got.Model != "fixture-model" {
				t.Fatalf("charged usage=%+v", got)
			}
			wantBalance := startBalance - models.Micro(tc.wantInput+tc.wantOutput)
			if last.Code != tc.wantCode || last.BalanceUSD == nil || *last.BalanceUSD != wantBalance.USD() ||
				last.CreditsRemaining != models.CreditsFromMicro(wantBalance) || len(repo.rows) != tc.wantRows {
				t.Fatalf("event=%+v persisted rows=%d", last, len(repo.rows))
			}
		})
	}
}
