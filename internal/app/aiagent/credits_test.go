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
	balance, attempts, refunded, settlements, settledCredits, settledTokens int
	keys                                                                    []string
}

// Consume enforces the fixture balance and records the idempotency key of each charged turn.
func (l *runCreditLedger) Consume(ctx context.Context, _ uuid.UUID, amount int, _, _ string, _ int, key string) (int, error) {
	if err := ctx.Err(); err != nil {
		return l.balance, err
	}
	l.attempts++
	if l.balance < amount {
		return l.balance, credits.ErrInsufficientCredits
	}
	l.balance -= amount
	l.keys = append(l.keys, key)
	return l.balance, nil
}

// Grant records failed-turn refunds and rejects canceled storage contexts.
func (l *runCreditLedger) Grant(ctx context.Context, _ uuid.UUID, amount int, _ string) (int, *errx.Error) {
	if ctx.Err() != nil {
		return l.balance, errx.New(errx.Internal, "credit context is canceled")
	}
	l.refunded += amount
	l.balance += amount
	return l.balance, nil
}

// SettleUsage records the charged turns and token usage without introducing external pricing into the fixture.
func (l *runCreditLedger) SettleUsage(ctx context.Context, _ uuid.UUID, charged int, _ string, tokens int, _, _ string) (int, error) {
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	l.settlements++
	l.settledCredits, l.settledTokens = charged, tokens
	return 0, nil
}

// TestRunMessageChargesReservedAnswerWithinBudget covers successful answers, failed-turn refunds, and depleted credits.
func TestRunMessageChargesReservedAnswerWithinBudget(t *testing.T) {
	for _, tc := range []struct {
		name                                                                                   string
		balance, wantRequests, wantCharged, wantRefund, wantBalance, wantRows, wantSettlements int
		wantCode                                                                               string
	}{
		{name: "answer", balance: 50, wantRequests: 20, wantCharged: 20, wantBalance: 30, wantRows: 40, wantSettlements: 1},
		{name: "empty_answer", balance: 50, wantRequests: 20, wantCharged: 20, wantRefund: 1, wantBalance: 31, wantRows: 39, wantCode: "provider_error"},
		{name: "no_credit_for_answer", balance: 19, wantRequests: 19, wantCharged: 19, wantBalance: 0, wantRows: 39, wantSettlements: 1, wantCode: "insufficient_credits"},
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
					"choices": []any{map[string]any{"message": message}}, "usage": map[string]int{"total_tokens": 10},
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
			ledger := &runCreditLedger{balance: tc.balance}
			svc := NewService(repo, registry, provider, ledger, runFeatureGate{}, nil, nil, nil, nil)
			var last StreamEvent
			if xerr := svc.RunMessage(context.Background(), aitools.Invocation{OrgID: repo.session.OrgID, UserID: repo.session.UserID}, repo.session.ID, "paid-message", "Inspect sending", "", "", func(ev StreamEvent) { last = ev }); xerr != nil {
				t.Fatal(xerr)
			}
			if requests != tc.wantRequests || ledger.attempts != 20 || len(ledger.keys) != tc.wantCharged || ledger.refunded != tc.wantRefund || ledger.balance != tc.wantBalance {
				t.Fatalf("requests=%d attempts=%d charged=%d refunded=%d balance=%d", requests, ledger.attempts, len(ledger.keys), ledger.refunded, ledger.balance)
			}
			if ledger.keys[0] != "paid-message:1" || ledger.keys[len(ledger.keys)-1] != fmt.Sprintf("paid-message:%d", tc.wantCharged) {
				t.Fatalf("credit idempotency keys changed: %v", ledger.keys)
			}
			if ledger.settlements != tc.wantSettlements || tc.wantSettlements > 0 && (ledger.settledCredits != tc.wantCharged || ledger.settledTokens != tc.wantRequests*10) {
				t.Fatalf("settlements=%d credits=%d tokens=%d", ledger.settlements, ledger.settledCredits, ledger.settledTokens)
			}
			if last.Code != tc.wantCode || last.CreditsRemaining != tc.wantBalance || len(repo.rows) != tc.wantRows {
				t.Fatalf("event=%+v persisted rows=%d", last, len(repo.rows))
			}
		})
	}
}
