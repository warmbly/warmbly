package aiagent

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
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

// TestRunMessagePreservesCompletedActionsAfterCancellation protects receipts, the charge, and newer approvals during cleanup.
func TestRunMessagePreservesCompletedActionsAfterCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	requests, writes := 0, 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		w.Header().Set("Content-Type", "application/json")
		if requests == 1 {
			_, _ = w.Write([]byte(`{"choices":[{"message":{"role":"assistant","tool_calls":[{"id":"completed-write","type":"function","function":{"name":"update_contact_fields","arguments":"{}"}}]}}],"usage":{"prompt_tokens":6,"completion_tokens":4,"total_tokens":10}}`))
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
	ledger := &runCreditLedger{balance: 50, allowed: 50}
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
	if len(ledger.charges) != 1 || ledger.keys[0] != "canceled-message" || ledger.charges[0].InputTokens < 6 {
		t.Errorf("cancellation lost the completed turn's charge: charges=%+v keys=%v", ledger.charges, ledger.keys)
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

func openAIChoice(w http.ResponseWriter, content, tool, callID string) {
	w.Header().Set("Content-Type", "application/json")
	msg := map[string]any{"role": "assistant", "content": content}
	if tool != "" {
		msg["tool_calls"] = []any{map[string]any{
			"id": callID, "type": "function",
			"function": map[string]string{"name": tool, "arguments": `{}`},
		}}
	}
	_ = json.NewEncoder(w).Encode(map[string]any{
		"choices": []any{map[string]any{"message": msg}},
		"usage":   map[string]int{"prompt_tokens": 6, "completion_tokens": 4, "total_tokens": 10},
	})
}

func localOpenAI(t *testing.T, h http.HandlerFunc) generation.Provider {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	p, err := generation.NewProvider(generation.ProviderConfig{
		OpenAIAPIKey: "fixture", OpenAIBaseURL: srv.URL, Local: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func newRunSession() (*runRepository, aitools.Invocation) {
	repo := &runRepository{session: models.AgentSession{ID: uuid.New(), OrgID: uuid.New(), UserID: uuid.New()}}
	return repo, aitools.Invocation{OrgID: repo.session.OrgID, UserID: repo.session.UserID}
}

func decodeAgentRow(t *testing.T, row models.AgentMessageRow) generation.AgentMessage {
	t.Helper()
	var m generation.AgentMessage
	if err := json.Unmarshal(row.Content, &m); err != nil {
		t.Fatalf("decode row: %v", err)
	}
	return m
}

func countingTool(name string, risk generation.RiskClass, n *int, out string) aitools.Tool {
	return aitools.Tool{
		Name: name,
		Risk: risk,
		Handler: func(context.Context, aitools.Invocation, json.RawMessage) (string, error) {
			if n != nil {
				*n++
			}
			return out, nil
		},
	}
}

func TestRunMessageKeepsApprovalCommentary(t *testing.T) {
	const sentence = "I'll update the contact."
	requests, writes := 0, 0
	p := localOpenAI(t, func(w http.ResponseWriter, r *http.Request) {
		requests++
		openAIChoice(w, sentence, "update_contact_fields", "call-1")
	})
	repo, inv := newRunSession()
	reg := aitools.NewRegistry()
	reg.Register(countingTool("update_contact_fields", generation.RiskWrite, &writes, `{"updated":true}`))
	svc := NewService(repo, reg, p, nil, runFeatureGate{}, nil, nil, nil, nil)
	var events []StreamEvent
	if xerr := svc.RunMessage(context.Background(), inv, repo.session.ID, "m1", "Update the contact", "", "", func(ev StreamEvent) {
		events = append(events, ev)
	}); xerr != nil {
		t.Fatal(xerr)
	}
	last := events[len(events)-1]
	if requests != 1 || writes != 0 || last.Type != evDone || last.Message != "awaiting_approval" {
		t.Fatalf("requests=%d writes=%d last=%+v", requests, writes, last)
	}
	turns, pending, xerr := svc.Transcript(context.Background(), inv, repo.session.ID)
	if xerr != nil {
		t.Fatal(xerr)
	}
	if pending == nil || pending.ToolName != "update_contact_fields" {
		t.Fatalf("pending=%+v", pending)
	}
	if len(turns) != 2 || len(turns[1].Blocks) != 1 || turns[1].Blocks[0].Kind != "text" || turns[1].Blocks[0].Text != sentence {
		t.Fatalf("transcript=%+v", turns)
	}
	first := decodeAgentRow(t, repo.rows[0])
	if first.Role != "user" || first.Content != "Update the contact" {
		t.Fatalf("user row=%+v", first)
	}
}

func TestStrictApprovalIgnoresAlwaysAllow(t *testing.T) {
	for _, strict := range []bool{false, true} {
		name := "plain"
		if strict {
			name = "strict"
		}
		t.Run(name, func(t *testing.T) {
			requests, writes := 0, 0
			p := localOpenAI(t, func(w http.ResponseWriter, r *http.Request) {
				requests++
				if requests == 1 {
					openAIChoice(w, "", "update_contact_fields", "call-1")
					return
				}
				openAIChoice(w, "Done.", "", "")
			})
			repo, inv := newRunSession()
			repo.policies = map[string]string{"update_contact_fields": "always_allow"}
			reg := aitools.NewRegistry()
			reg.Register(countingTool("update_contact_fields", generation.RiskWrite, &writes, `{"updated":true}`))
			svc := NewService(repo, reg, p, nil, runFeatureGate{}, nil, nil, nil, nil)
			ctx := context.Background()
			if strict {
				ctx = WithStrictApproval(ctx)
			}
			var events []StreamEvent
			if xerr := svc.RunMessage(ctx, inv, repo.session.ID, "m", "Update the contact", "", "", func(ev StreamEvent) {
				events = append(events, ev)
			}); xerr != nil {
				t.Fatal(xerr)
			}
			last := events[len(events)-1]
			if strict {
				if requests != 1 || writes != 0 || last.Type != evDone || last.Message != "awaiting_approval" {
					t.Fatalf("requests=%d writes=%d last=%+v", requests, writes, last)
				}
				if repo.session.Context.Pending == nil || repo.session.Context.Pending.ToolName != "update_contact_fields" {
					t.Fatalf("pending=%+v", repo.session.Context.Pending)
				}
				return
			}
			if requests != 2 || writes != 1 || last.Type != evDone || last.Message != "" {
				t.Fatalf("requests=%d writes=%d last=%+v", requests, writes, last)
			}
		})
	}
}

func TestResumeDenySkipsTheToolAndContinues(t *testing.T) {
	const denied = `{"status":"denied","note":"The user declined to run this action."}`
	requests, writes := 0, 0
	p := localOpenAI(t, func(w http.ResponseWriter, r *http.Request) {
		requests++
		if requests == 1 {
			openAIChoice(w, "", "update_contact_fields", "call-1")
			return
		}
		openAIChoice(w, "Stopped.", "", "")
	})
	repo, inv := newRunSession()
	reg := aitools.NewRegistry()
	reg.Register(countingTool("update_contact_fields", generation.RiskWrite, &writes, `{"updated":true}`))
	svc := NewService(repo, reg, p, nil, runFeatureGate{}, nil, nil, nil, nil)
	var events []StreamEvent
	emit := func(ev StreamEvent) { events = append(events, ev) }
	if xerr := svc.RunMessage(context.Background(), inv, repo.session.ID, "m", "Update the contact", "", "", emit); xerr != nil {
		t.Fatal(xerr)
	}
	if requests != 1 || writes != 0 || events[len(events)-1].Message != "awaiting_approval" {
		t.Fatalf("requests=%d writes=%d last=%+v", requests, writes, events[len(events)-1])
	}
	if xerr := svc.Resume(context.Background(), inv, repo.session.ID, "other-call", "deny", emit); xerr != errApprovalNotPending || requests != 1 {
		t.Fatalf("wrong id: err=%v requests=%d", xerr, requests)
	}
	events = nil
	if xerr := svc.Resume(context.Background(), inv, repo.session.ID, "call-1", "deny", emit); xerr != nil {
		t.Fatal(xerr)
	}
	var sawText bool
	for _, ev := range events {
		if ev.Type == evTool || ev.Type == evToolDone {
			t.Fatalf("deny emitted a tool event: %+v", ev)
		}
		if ev.Type == evText && ev.Text == "Stopped." {
			sawText = true
		}
	}
	if !sawText || writes != 0 {
		t.Fatalf("text=%v writes=%d events=%+v", sawText, writes, events)
	}
	var toolBody string
	for _, row := range repo.rows {
		if m := decodeAgentRow(t, row); m.Role == "tool" {
			toolBody = m.Content
		}
	}
	if toolBody != denied {
		t.Fatalf("stored tool=%s", toolBody)
	}
	turns, _, xerr := svc.Transcript(context.Background(), inv, repo.session.ID)
	if xerr != nil {
		t.Fatal(xerr)
	}
	var result string
	for _, b := range turns[len(turns)-1].Blocks {
		if b.Kind == "tool" {
			result = b.Result
		}
	}
	if result != denied {
		t.Fatalf("hydrated result=%s", result)
	}
}

func TestRunMessageShowsASecretOnce(t *testing.T) {
	calls := 0
	p := localOpenAI(t, func(w http.ResponseWriter, r *http.Request) {
		calls++
		if calls == 1 {
			openAIChoice(w, "", "issue_token", "sec-1")
			return
		}
		openAIChoice(w, "Done.", "", "")
	})
	repo, inv := newRunSession()
	reg := aitools.NewRegistry()
	reg.Register(aitools.Tool{
		Name:         "issue_token",
		Risk:         generation.RiskRead,
		SecretFields: []string{"token"},
		Handler: func(context.Context, aitools.Invocation, json.RawMessage) (string, error) {
			return `{"token":"sekret","ok":true}`, nil
		},
	})
	svc := NewService(repo, reg, p, nil, runFeatureGate{}, nil, nil, nil, nil)
	var events []StreamEvent
	if xerr := svc.RunMessage(context.Background(), inv, repo.session.ID, "m", "Issue a token", "", "", func(ev StreamEvent) {
		events = append(events, ev)
	}); xerr != nil {
		t.Fatal(xerr)
	}
	var sawSecret bool
	for _, ev := range events {
		if ev.Type == evSecret && ev.Secrets["token"] == "sekret" {
			sawSecret = true
		}
		if ev.Type == evToolDone && strings.Contains(ev.Result, "sekret") {
			t.Fatalf("tool result leaked the secret: %s", ev.Result)
		}
	}
	if !sawSecret {
		t.Fatal("tool_secret was not emitted")
	}
	var toolBody string
	for _, row := range repo.rows {
		if m := decodeAgentRow(t, row); m.Role == "tool" {
			toolBody = m.Content
		}
	}
	if strings.Contains(toolBody, "sekret") || !strings.Contains(toolBody, secretPlaceholder) {
		t.Fatalf("stored tool=%s", toolBody)
	}
}

func TestRunMessageWritesTheUserTurnAndThePage(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		p := localOpenAI(t, func(w http.ResponseWriter, r *http.Request) {
			openAIChoice(w, "The mailbox is paused.", "", "")
		})
		repo, inv := newRunSession()
		svc := NewService(repo, aitools.NewRegistry(), p, nil, runFeatureGate{}, nil, nil, nil, nil)
		var events []StreamEvent
		if xerr := svc.RunMessage(context.Background(), inv, repo.session.ID, "m", "Inspect sending", "/app/campaigns/x", "campaign:abc", func(ev StreamEvent) {
			events = append(events, ev)
		}); xerr != nil {
			t.Fatal(xerr)
		}
		first := decodeAgentRow(t, repo.rows[0])
		if first.Role != "user" || first.Content != "Inspect sending" {
			t.Fatalf("user row=%+v", first)
		}
		if len(repo.rows) < 2 || decodeAgentRow(t, repo.rows[1]).Role != "assistant" {
			t.Fatalf("rows=%d", len(repo.rows))
		}
		if repo.session.Context.Page != "/app/campaigns/x" || repo.session.Context.Resource != "campaign:abc" || repo.session.Context.Pending != nil {
			t.Fatalf("context=%+v", repo.session.Context)
		}
		iter, textAt := 0, -1
		for i, ev := range events {
			if ev.Type == "iteration" {
				iter++
				if ev.Budget != DefaultRunBudget {
					t.Fatalf("budget=%d", ev.Budget)
				}
			}
			if ev.Type == evText && textAt < 0 {
				textAt = i
			}
		}
		var iterAt int
		for i, ev := range events {
			if ev.Type == "iteration" {
				iterAt = i
				break
			}
		}
		if iter != 1 || textAt < iterAt || events[len(events)-1].Type != evDone {
			t.Fatalf("events=%+v", events)
		}
	})
	t.Run("provider error", func(t *testing.T) {
		p := localOpenAI(t, func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadGateway)
			_, _ = w.Write([]byte(`{"error":{"message":"down"}}`))
		})
		repo, inv := newRunSession()
		svc := NewService(repo, aitools.NewRegistry(), p, nil, runFeatureGate{}, nil, nil, nil, nil)
		var events []StreamEvent
		if xerr := svc.RunMessage(context.Background(), inv, repo.session.ID, "m", "Inspect sending", "", "", func(ev StreamEvent) {
			events = append(events, ev)
		}); xerr != nil {
			t.Fatal(xerr)
		}
		last := events[len(events)-1]
		if last.Type != evError || last.Code != "provider_error" || last.Message != "Remie hit an error. Please try again." {
			t.Fatalf("last=%+v", last)
		}
		if len(repo.rows) != 1 {
			t.Fatalf("rows=%d", len(repo.rows))
		}
		first := decodeAgentRow(t, repo.rows[0])
		if first.Role != "user" || first.Content != "Inspect sending" {
			t.Fatalf("user row=%+v", first)
		}
	})
}
