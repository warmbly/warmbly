package aiagent

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/warmbly/warmbly/internal/app/aitools"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/pkg/generation"
	"github.com/warmbly/warmbly/internal/repository"
)

type runRepository struct {
	repository.AgentRepository
	session     models.AgentSession
	rows        []models.AgentMessageRow
	policies    map[string]string
	afterAppend func(context.Context)
}

// GetSession returns a snapshot so an older run cannot mutate newer persisted approval state.
func (r *runRepository) GetSession(context.Context, uuid.UUID, uuid.UUID, uuid.UUID) (*models.AgentSession, error) {
	session := r.session
	return &session, nil
}

// LoadTranscript exposes persisted fixture rows in their per-session timestamp order.
func (r *runRepository) LoadTranscript(context.Context, uuid.UUID, uuid.UUID, uuid.UUID) ([]models.AgentMessageRow, error) {
	return r.rows, nil
}

// AppendMessages models cancellation and monotonic timestamps at the transcript storage boundary.
func (r *runRepository) AppendMessages(ctx context.Context, _, _, _ uuid.UUID, rows []models.AgentMessageRow) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	// Match the repository's strictly increasing per-session timestamps.
	for _, row := range rows {
		row.CreatedAt = time.Date(2026, 10, 1, 9, 0, 0, 0, time.UTC).Add(time.Duration(len(r.rows)) * time.Microsecond)
		r.rows = append(r.rows, row)
	}
	if r.afterAppend != nil {
		r.afterAppend(ctx)
	}
	return nil
}

// UpdateSessionContext persists approval state for subsequent run and resume calls.
func (r *runRepository) UpdateSessionContext(_ context.Context, _, _, _ uuid.UUID, sctx models.AgentSessionContext) error {
	r.session.Context = sctx
	return nil
}

// UpdateSessionTitle ignores title writes because these fixtures exercise run completion.
func (r *runRepository) UpdateSessionTitle(context.Context, uuid.UUID, uuid.UUID, uuid.UUID, string) error {
	return nil
}

// GetToolPolicies supplies the explicit always-allow policies needed to exercise completed writes.
func (r *runRepository) GetToolPolicies(context.Context, uuid.UUID) (map[string]string, error) {
	return r.policies, nil
}

// ClearPendingTool rejects canceled requests so stale cleanup cannot remove a newer approval.
func (r *runRepository) ClearPendingTool(ctx context.Context, _ uuid.UUID, _ uuid.UUID) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	r.session.Context.Pending = nil
	return nil
}

// ClaimPendingTool consumes only the matching approval, leaving replay attempts without an action.
func (r *runRepository) ClaimPendingTool(_ context.Context, _, _ uuid.UUID, callID string) (*models.PendingAgentTool, error) {
	pending := r.session.Context.Pending
	if pending == nil || pending.ToolCallID != callID {
		return nil, nil
	}
	r.session.Context.Pending = nil
	return pending, nil
}

type runFeatureGate struct{}

// IsPaidOrganization selects the paid model while each test chooses whether credits are metered.
func (runFeatureGate) IsPaidOrganization(context.Context, uuid.UUID) (bool, *errx.Error) {
	return true, nil
}

// TestRunMessageAnswersBeforeBudgetIsExhausted requires a streamed and persisted answer after repeated tool rounds.
func TestRunMessageAnswersBeforeBudgetIsExhausted(t *testing.T) {
	const answer = "No sending mailbox matched that address, so I could not verify why it did not send."
	requests, executions := 0, 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Tools []json.RawMessage `json:"tools"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Error(err)
		}
		requests++
		message := map[string]any{"role": "assistant", "content": answer}
		if len(req.Tools) > 0 {
			calls := make([]map[string]any, 3)
			for i := range calls {
				calls[i] = map[string]any{
					"id": fmt.Sprintf("call-%d-%d", requests, i), "type": "function",
					"function": map[string]string{"name": "list_mailboxes", "arguments": `{}`},
				}
			}
			message["content"], message["tool_calls"] = "", calls
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []any{map[string]any{"message": message}},
			"usage":   map[string]int{"total_tokens": 10},
		})
	}))
	defer srv.Close()
	provider, err := generation.NewProvider(generation.ProviderConfig{
		OpenAIAPIKey: "fixture", OpenAIBaseURL: srv.URL, Local: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	registry := aitools.NewRegistry()
	registry.Register(aitools.Tool{
		Name: "list_mailboxes", Risk: generation.RiskRead,
		InputSchema: map[string]any{"type": "object", "properties": map[string]any{}},
		Handler: func(context.Context, aitools.Invocation, json.RawMessage) (string, error) {
			executions++
			return `{"mailboxes":[],"count":0}`, nil
		},
	})
	repo := &runRepository{session: models.AgentSession{ID: uuid.New(), OrgID: uuid.New(), UserID: uuid.New()}}
	inv := aitools.Invocation{OrgID: repo.session.OrgID, UserID: repo.session.UserID}
	svc := NewService(repo, registry, provider, nil, runFeatureGate{}, nil, nil, nil, nil)
	var events []StreamEvent
	if xerr := svc.RunMessage(context.Background(), inv, repo.session.ID, "message-1", "Why did this mailbox not send?", "", "", func(ev StreamEvent) {
		events = append(events, ev)
	}); xerr != nil {
		t.Fatal(xerr)
	}
	if requests != 20 || executions != 57 {
		t.Errorf("requests=%d tool executions=%d, want 20 and 57 (one round reserved for the answer)", requests, executions)
	}
	if len(events) < 2 || events[len(events)-2].Type != evText || events[len(events)-2].Text != answer || events[len(events)-1].Type != evDone {
		t.Errorf("run did not emit its answer before done: last event=%+v", events[len(events)-1])
	}
	turns, _, xerr := svc.Transcript(context.Background(), inv, repo.session.ID)
	if xerr != nil {
		t.Fatal(xerr)
	}
	blocks := turns[len(turns)-1].Blocks
	if blocks[len(blocks)-1].Kind != "text" || blocks[len(blocks)-1].Text != answer {
		t.Errorf("reopened conversation has no final answer: last block=%+v", blocks[len(blocks)-1])
	}
}

// TestRunMessageReportsEmptyProviderAnswer surfaces an explicit error instead of silently completing the chat.
func TestRunMessageReportsEmptyProviderAnswer(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":""}}]}`))
	}))
	defer srv.Close()
	provider, err := generation.NewProvider(generation.ProviderConfig{
		OpenAIAPIKey: "fixture", OpenAIBaseURL: srv.URL, Local: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	repo := &runRepository{session: models.AgentSession{ID: uuid.New(), OrgID: uuid.New(), UserID: uuid.New()}}
	svc := NewService(repo, aitools.NewRegistry(), provider, nil, runFeatureGate{}, nil, nil, nil, nil)
	var events []StreamEvent
	if xerr := svc.RunMessage(context.Background(), aitools.Invocation{OrgID: repo.session.OrgID, UserID: repo.session.UserID}, repo.session.ID, "message-1", "Inspect sending", "", "", func(ev StreamEvent) {
		events = append(events, ev)
	}); xerr != nil {
		t.Fatal(xerr)
	}
	if last := events[len(events)-1]; last.Type != evError || last.Code != "provider_error" || last.Message == "" {
		t.Fatalf("empty answer did not produce a visible error: %+v", last)
	}
}

// TestRunMessagePreservesCompletedActionsWhenFinalResponseFails keeps completed writes visible after provider failure.
func TestRunMessagePreservesCompletedActionsWhenFinalResponseFails(t *testing.T) {
	for _, failure := range []string{"empty", "unexpected_tool", "provider_error"} {
		t.Run(failure, func(t *testing.T) {
			requests, writes := 0, 0
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requests++
				w.Header().Set("Content-Type", "application/json")
				message := map[string]any{"role": "assistant", "content": ""}
				tool := "list_mailboxes"
				if requests == 1 || failure == "unexpected_tool" && requests == 20 {
					tool = "update_contact_fields"
				}
				if requests < 20 || failure == "unexpected_tool" {
					message["tool_calls"] = []any{map[string]any{
						"id": fmt.Sprintf("call-%d", requests), "type": "function",
						"function": map[string]string{"name": tool, "arguments": `{}`},
					}}
				} else if failure == "provider_error" {
					w.WriteHeader(http.StatusBadGateway)
					_, _ = w.Write([]byte(`{"error":{"message":"fixture upstream unavailable"}}`))
					return
				}
				_ = json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"message": message}}})
			}))
			defer srv.Close()
			provider, err := generation.NewProvider(generation.ProviderConfig{OpenAIAPIKey: "fixture", OpenAIBaseURL: srv.URL, Local: true})
			if err != nil {
				t.Fatal(err)
			}
			registry := aitools.NewRegistry()
			registry.Register(aitools.Tool{Name: "update_contact_fields", Risk: generation.RiskWrite, Handler: func(context.Context, aitools.Invocation, json.RawMessage) (string, error) {
				writes++
				return `{"updated":true}`, nil
			}})
			registry.Register(aitools.Tool{Name: "list_mailboxes", Risk: generation.RiskRead, Handler: func(context.Context, aitools.Invocation, json.RawMessage) (string, error) {
				return `{"mailboxes":[],"count":0}`, nil
			}})
			repo := &runRepository{
				session:  models.AgentSession{ID: uuid.New(), OrgID: uuid.New(), UserID: uuid.New()},
				policies: map[string]string{"update_contact_fields": "always_allow"},
			}
			svc := NewService(repo, registry, provider, nil, runFeatureGate{}, nil, nil, nil, nil)
			inv := aitools.Invocation{OrgID: repo.session.OrgID, UserID: repo.session.UserID}
			var last StreamEvent
			if xerr := svc.RunMessage(context.Background(), inv, repo.session.ID, "message-1", "Update the contact and inspect sending", "", "", func(ev StreamEvent) { last = ev }); xerr != nil {
				t.Fatal(xerr)
			}
			if requests != 20 || writes != 1 || last.Type != evError {
				t.Fatalf("requests=%d writes=%d last=%+v", requests, writes, last)
			}
			turns, _, xerr := svc.Transcript(context.Background(), inv, repo.session.ID)
			if xerr != nil {
				t.Fatal(xerr)
			}
			if len(turns) != 2 || len(turns[1].Blocks) != 19 || turns[1].Blocks[0].Tool != "update_contact_fields" || turns[1].Blocks[0].Result != `{"updated":true}` {
				t.Fatalf("completed actions were lost after a failed final response: %+v", turns)
			}
		})
	}
}

// TestResumePreservesApprovedActionWhenProviderFails records the approved action once and refuses approval replay.
func TestResumePreservesApprovedActionWhenProviderFails(t *testing.T) {
	requests, writes := 0, 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		w.Header().Set("Content-Type", "application/json")
		if requests == 1 {
			_, _ = w.Write([]byte(`{"choices":[{"message":{"role":"assistant","tool_calls":[{"id":"approved-call","type":"function","function":{"name":"update_contact_fields","arguments":"{}"}}]}}]}`))
			return
		}
		_, _ = w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":""}}]}`))
	}))
	defer srv.Close()
	provider, err := generation.NewProvider(generation.ProviderConfig{OpenAIAPIKey: "fixture", OpenAIBaseURL: srv.URL, Local: true})
	if err != nil {
		t.Fatal(err)
	}
	registry := aitools.NewRegistry()
	registry.Register(aitools.Tool{Name: "update_contact_fields", Risk: generation.RiskWrite, Handler: func(context.Context, aitools.Invocation, json.RawMessage) (string, error) {
		writes++
		return `{"updated":true}`, nil
	}})
	repo := &runRepository{session: models.AgentSession{ID: uuid.New(), OrgID: uuid.New(), UserID: uuid.New()}}
	svc := NewService(repo, registry, provider, nil, runFeatureGate{}, nil, nil, nil, nil)
	inv := aitools.Invocation{OrgID: repo.session.OrgID, UserID: repo.session.UserID}
	var last StreamEvent
	emit := func(ev StreamEvent) { last = ev }
	if xerr := svc.RunMessage(context.Background(), inv, repo.session.ID, "approval-message", "Update the contact", "", "", emit); xerr != nil {
		t.Fatal(xerr)
	}
	if writes != 0 || requests != 1 || last.Message != "awaiting_approval" {
		t.Fatalf("action did not pause for approval: writes=%d requests=%d event=%+v", writes, requests, last)
	}
	if xerr := svc.Resume(context.Background(), inv, repo.session.ID, "another-call", "approve", emit); xerr != errApprovalNotPending {
		t.Fatalf("an unrelated approval was accepted: %v", xerr)
	}
	if xerr := svc.Resume(context.Background(), inv, repo.session.ID, "approved-call", "approve", emit); xerr != nil {
		t.Fatal(xerr)
	}
	if requests != 2 || writes != 1 || last.Type != evError || last.Code != "provider_error" {
		t.Fatalf("approved action was not executed once before the provider failure: writes=%d requests=%d event=%+v", writes, requests, last)
	}
	turns, pending, xerr := svc.Transcript(context.Background(), inv, repo.session.ID)
	if xerr != nil {
		t.Fatal(xerr)
	}
	if pending != nil || len(turns) != 2 || len(turns[1].Blocks) != 1 || turns[1].Blocks[0].Result != `{"updated":true}` {
		t.Fatalf("approved action was not retained: turns=%+v pending=%+v", turns, pending)
	}
	if xerr := svc.Resume(context.Background(), inv, repo.session.ID, "approved-call", "approve", emit); xerr != errApprovalNotPending || requests != 2 || writes != 1 {
		t.Fatalf("approval replay changed state: err=%v writes=%d requests=%d", xerr, writes, requests)
	}
}

// TestRunMessagePreservesCompletedActionsAfterCancellation protects receipts, refunds, and newer approvals during cleanup.
func TestRunMessagePreservesCompletedActionsAfterCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	requests, writes := 0, 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		w.Header().Set("Content-Type", "application/json")
		if requests == 1 {
			_, _ = w.Write([]byte(`{"choices":[{"message":{"role":"assistant","tool_calls":[{"id":"completed-write","type":"function","function":{"name":"update_contact_fields","arguments":"{}"}}]}}]}`))
			return
		}
		cancel()
		_, _ = w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":""}}]}`))
	}))
	defer srv.Close()
	provider, err := generation.NewProvider(generation.ProviderConfig{OpenAIAPIKey: "fixture", OpenAIBaseURL: srv.URL, OpenAIModelPaid: "fixture-model"})
	if err != nil {
		t.Fatal(err)
	}
	registry := aitools.NewRegistry()
	registry.Register(aitools.Tool{Name: "update_contact_fields", Risk: generation.RiskWrite, Handler: func(context.Context, aitools.Invocation, json.RawMessage) (string, error) {
		writes++
		return `{"updated":true}`, nil
	}})
	repo := &runRepository{
		session:  models.AgentSession{ID: uuid.New(), OrgID: uuid.New(), UserID: uuid.New()},
		policies: map[string]string{"update_contact_fields": "always_allow"},
	}
	ledger := &runCreditLedger{balance: 50}
	repo.afterAppend = func(receiptCtx context.Context) {
		if ctx.Err() != nil {
			if _, bounded := receiptCtx.Deadline(); !bounded {
				t.Error("canceled-run receipt persistence must have a deadline")
			}
			repo.session.Context.Pending = &models.PendingAgentTool{ToolCallID: "newer-run-call", ToolName: "update_contact_fields", Risk: "write"}
		}
	}
	svc := NewService(repo, registry, provider, ledger, runFeatureGate{}, nil, nil, nil, nil)
	inv := aitools.Invocation{OrgID: repo.session.OrgID, UserID: repo.session.UserID}
	if xerr := svc.RunMessage(ctx, inv, repo.session.ID, "canceled-message", "Update the contact", "", "", func(StreamEvent) {}); xerr != nil {
		t.Fatal(xerr)
	}
	if ctx.Err() == nil || requests != 2 || writes != 1 {
		t.Fatalf("fixture did not cancel after one completed action: canceled=%v requests=%d writes=%d", ctx.Err(), requests, writes)
	}
	if ledger.refunded != 1 || ledger.balance != 49 {
		t.Errorf("cancellation lost the failed iteration refund: refunded=%d balance=%d", ledger.refunded, ledger.balance)
	}
	turns, pending, xerr := svc.Transcript(context.Background(), inv, repo.session.ID)
	if xerr != nil {
		t.Fatal(xerr)
	}
	if len(turns) != 2 || len(turns[1].Blocks) != 1 || turns[1].Blocks[0].Result != `{"updated":true}` {
		t.Fatalf("request cancellation discarded a completed action: %+v", turns)
	}
	if pending == nil || pending.ToolCallID != "newer-run-call" {
		t.Fatalf("canceled run cleared a newer run's approval: %+v", pending)
	}
}
