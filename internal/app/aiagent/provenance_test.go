package aiagent

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/google/uuid"

	"github.com/warmbly/warmbly/internal/app/aitools"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/pkg/generation"
	"github.com/warmbly/warmbly/internal/repository"
)

// fakeAgentRepo answers only what RunMessage/Resume touch; every other
// embedded method panics if a new code path reaches it unexpectedly.
type fakeAgentRepo struct {
	repository.AgentRepository
	sess        *models.AgentSession
	policies    map[string]string
	contexts    []models.AgentSessionContext
	policyCalls []string
}

func (f *fakeAgentRepo) GetSession(ctx context.Context, orgID, userID, sessionID uuid.UUID) (*models.AgentSession, error) {
	if f.sess == nil || f.sess.OrgID != orgID {
		return nil, nil
	}
	return f.sess, nil
}

func (f *fakeAgentRepo) UpdateSessionContext(ctx context.Context, orgID, userID, sessionID uuid.UUID, sctx models.AgentSessionContext) error {
	f.contexts = append(f.contexts, sctx)
	return nil
}

func (f *fakeAgentRepo) UpdateSessionTitle(ctx context.Context, orgID, userID, sessionID uuid.UUID, title string) error {
	return nil
}

func (f *fakeAgentRepo) LoadTranscript(ctx context.Context, orgID, userID, sessionID uuid.UUID) ([]models.AgentMessageRow, error) {
	return nil, nil
}

func (f *fakeAgentRepo) AppendMessages(ctx context.Context, orgID, userID, sessionID uuid.UUID, msgs []models.AgentMessageRow) error {
	return nil
}

func (f *fakeAgentRepo) GetToolPolicies(ctx context.Context, orgID uuid.UUID) (map[string]string, error) {
	return f.policies, nil
}

func (f *fakeAgentRepo) SetToolPolicy(ctx context.Context, orgID uuid.UUID, toolName, decision string, createdBy uuid.UUID) error {
	f.policyCalls = append(f.policyCalls, toolName+"="+decision)
	return nil
}

type fakeGate struct{}

func (fakeGate) IsPaidOrganization(ctx context.Context, orgID uuid.UUID) (bool, *errx.Error) {
	return false, nil
}

// scriptedProvider runs one scripted RunAgent per test; IsLocal keeps the
// credit service (nil here) out of the loop entirely.
type scriptedProvider struct {
	generation.Provider
	script func(ctx context.Context, req generation.AgentRequest) (*generation.AgentResult, error)
}

func (p scriptedProvider) RunAgent(ctx context.Context, req generation.AgentRequest) (*generation.AgentResult, error) {
	return p.script(ctx, req)
}

func (p scriptedProvider) ModelForTier(bool) string { return "fake-model" }
func (p scriptedProvider) Name() string             { return "fake" }
func (p scriptedProvider) IsLocal() bool            { return true }

// probeRegistry registers a read and a write tool that capture the invocation
// they actually execute under.
func probeRegistry(read, write *[]aitools.Invocation) *aitools.Registry {
	r := aitools.NewRegistry()
	r.Register(aitools.Tool{
		Name:        "probe_read",
		Description: "read probe",
		InputSchema: map[string]any{"type": "object"},
		Risk:        generation.RiskRead,
		Handler: func(ctx context.Context, inv aitools.Invocation, _ json.RawMessage) (string, error) {
			*read = append(*read, inv)
			return "{}", nil
		},
	})
	r.Register(aitools.Tool{
		Name:        "probe_write",
		Description: "write probe",
		InputSchema: map[string]any{"type": "object"},
		Risk:        generation.RiskWrite,
		Handler: func(ctx context.Context, inv aitools.Invocation, _ json.RawMessage) (string, error) {
			*write = append(*write, inv)
			return "{}", nil
		},
	})
	return r
}

// requestTool mirrors the real provider loop: read tools run directly, every
// write/send tool passes the Approve hook before any handler runs, and
// ErrApprovalRequired pauses the whole round.
func requestTool(ctx context.Context, req generation.AgentRequest, name string) error {
	for _, d := range req.Tools {
		if d.Name != name {
			continue
		}
		call := generation.ToolCall{ID: "call-1", Name: name, Args: json.RawMessage(`{}`)}
		if d.Risk != generation.RiskRead && req.Approve != nil {
			if err := req.Approve(ctx, d, call); err != nil {
				return err
			}
		}
		_, _ = d.Handler(ctx, call.Args)
		return nil
	}
	return nil
}

func noopEmit(StreamEvent) {}

// TestAgentRunProvenance pins the dashboard agent's provenance semantics: the
// segment invocation is stamped agent/always_allow (only policy-pre-approved
// writes can run there), a policy-less write pauses and never executes, and
// the post-approval execution — and only that execution — carries
// human_approved.
func TestAgentRunProvenance(t *testing.T) {
	org, user, sessID := uuid.New(), uuid.New(), uuid.New()

	newSess := func() *models.AgentSession {
		return &models.AgentSession{ID: sessID, OrgID: org, UserID: user}
	}

	t.Run("a write tool with no policy pauses and is never executed", func(t *testing.T) {
		var read, write []aitools.Invocation
		repo := &fakeAgentRepo{sess: newSess()}
		script := func(ctx context.Context, req generation.AgentRequest) (*generation.AgentResult, error) {
			if err := requestTool(ctx, req, "probe_read"); err != nil {
				t.Errorf("read tool should auto-run: %v", err)
			}
			if err := requestTool(ctx, req, "probe_write"); err == nil {
				t.Error("write tool without a policy must pause, not run")
			}
			return &generation.AgentResult{StopReason: "approval_required"}, nil
		}
		svc := NewService(repo, probeRegistry(&read, &write), scriptedProvider{script: script}, nil, fakeGate{}, nil, nil, nil, nil)
		if serr := svc.RunMessage(context.Background(), aitools.Invocation{OrgID: org, UserID: user}, sessID, "msg-1", "hello", "", "", noopEmit); serr != nil {
			t.Fatal(serr.Message)
		}
		if len(write) != 0 {
			t.Fatalf("paused write tool must not execute, got %d executions", len(write))
		}
		if len(read) != 1 {
			t.Fatalf("read tool should have executed once, got %d", len(read))
		}
		ri := read[0]
		if ri.AISurface != models.AISurfaceAgent || ri.AIDecision != models.AIDecisionAlwaysAllow {
			t.Fatalf("segment invocation not stamped: %+v", ri)
		}
		if ri.SessionID != sessID.String() || ri.MessageID != "msg-1" {
			t.Fatalf("session/message not stamped: %+v", ri)
		}
		var pending *models.PendingAgentTool
		for _, c := range repo.contexts {
			if c.Pending != nil {
				pending = c.Pending
			}
		}
		if pending == nil || pending.ToolName != "probe_write" {
			t.Fatalf("expected a persisted pending tool, got %+v", pending)
		}
	})

	t.Run("an always_allow write runs in the initial segment", func(t *testing.T) {
		var read, write []aitools.Invocation
		repo := &fakeAgentRepo{sess: newSess(), policies: map[string]string{"probe_write": "always_allow"}}
		script := func(ctx context.Context, req generation.AgentRequest) (*generation.AgentResult, error) {
			if err := requestTool(ctx, req, "probe_write"); err != nil {
				t.Errorf("policy-approved write must run: %v", err)
			}
			return &generation.AgentResult{StopReason: "stop"}, nil
		}
		svc := NewService(repo, probeRegistry(&read, &write), scriptedProvider{script: script}, nil, fakeGate{}, nil, nil, nil, nil)
		if serr := svc.RunMessage(context.Background(), aitools.Invocation{OrgID: org, UserID: user}, sessID, "msg-2", "hello", "", "", noopEmit); serr != nil {
			t.Fatal(serr.Message)
		}
		if len(write) != 1 {
			t.Fatalf("expected the write to run once, got %d", len(write))
		}
		if write[0].AIDecision != models.AIDecisionAlwaysAllow || write[0].AISurface != models.AISurfaceAgent {
			t.Fatalf("policy-approved write not attributed: %+v", write[0])
		}
	})

	t.Run("resumed execution after human approval is human_approved", func(t *testing.T) {
		var read, write []aitools.Invocation
		repo := &fakeAgentRepo{sess: newSess()}
		repo.sess.Context.Pending = &models.PendingAgentTool{
			MessageID: "msg-0", ToolCallID: "call-1", ToolName: "probe_write",
			Risk: string(generation.RiskWrite), Args: json.RawMessage(`{}`),
		}
		script := func(ctx context.Context, req generation.AgentRequest) (*generation.AgentResult, error) {
			return &generation.AgentResult{StopReason: "stop"}, nil
		}
		svc := NewService(repo, probeRegistry(&read, &write), scriptedProvider{script: script}, nil, fakeGate{}, nil, nil, nil, nil)
		if serr := svc.Resume(context.Background(), aitools.Invocation{OrgID: org, UserID: user}, sessID, "approve", noopEmit); serr != nil {
			t.Fatal(serr.Message)
		}
		if len(write) != 1 {
			t.Fatalf("expected the approved tool to run once, got %d", len(write))
		}
		wi := write[0]
		if wi.AIDecision != models.AIDecisionHumanApproved {
			t.Fatalf("resumed execution must be human_approved, got %q", wi.AIDecision)
		}
		if wi.AISurface != models.AISurfaceAgent || wi.SessionID != sessID.String() || wi.MessageID != "msg-0" {
			t.Fatalf("resumed execution provenance wrong: %+v", wi)
		}
		if len(repo.policyCalls) != 0 {
			t.Fatalf("a plain approve must not write a policy: %v", repo.policyCalls)
		}
		if len(repo.contexts) == 0 {
			t.Fatal("expected the resumed run to persist the cleared context")
		}
		if last := repo.contexts[len(repo.contexts)-1]; last.Pending != nil {
			t.Fatalf("pending must be cleared after approval, got %+v", last.Pending)
		}
	})
}
