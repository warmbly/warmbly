// Package aiagent orchestrates the dashboard AI agent: it runs the M2 tool
// registry through the M1 provider loop, streams step events to the client over
// SSE, pauses write/send tools for human approval, charges one credit per loop
// iteration (bounded by a per-run budget), and persists a resumable transcript.
package aiagent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/warmbly/warmbly/internal/app/aitools"
	"github.com/warmbly/warmbly/internal/app/credits"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/pkg/generation"
	"github.com/warmbly/warmbly/internal/repository"
)

// DefaultRunBudget bounds one run's loop iterations (and therefore its credit
// cost). Surfaced to the client so the meter shows the ceiling.
const DefaultRunBudget = 20

// FeatureGate is the slice of the feature service used to route the model tier.
type FeatureGate interface {
	IsPaidOrganization(ctx context.Context, orgID uuid.UUID) (bool, *errx.Error)
}

// AuditLogger fires the ai_session audit so the create shows in the spine.
type AuditLogger interface {
	LogAction(ctx context.Context, orgID, actorID uuid.UUID, action models.AuditAction, entityType models.AuditEntityType, entityID *uuid.UUID, ip, userAgent string, changes, metadata map[string]string)
}

// SkillPreamble renders the org's enabled playbooks for the system prompt.
type SkillPreamble interface {
	EnabledPreamble(ctx context.Context, orgID uuid.UUID) string
}

// VoicePreamble renders the org's writing-style/voice rules (the shared M4
// humanizer + the org's product/ICP/house-voice grounding) for the agent system
// prompt, so anything the agent writes for the user sounds human.
type VoicePreamble interface {
	VoiceInstructions(ctx context.Context, orgID uuid.UUID) string
}

// OrgVoiceGetter is the slice of the organization service the voice preamble
// needs: the org's voice-grounding columns.
type OrgVoiceGetter interface {
	Get(ctx context.Context, orgID uuid.UUID) (*models.Organization, *errx.Error)
}

type orgVoicePreamble struct{ orgs OrgVoiceGetter }

// NewVoicePreamble builds the agent voice preamble from the organization
// service. A nil getter (or a load failure) yields the built-in humanizer rules
// with no org grounding, so the agent still sounds human.
func NewVoicePreamble(orgs OrgVoiceGetter) VoicePreamble {
	return &orgVoicePreamble{orgs: orgs}
}

func (v *orgVoicePreamble) VoiceInstructions(ctx context.Context, orgID uuid.UUID) string {
	vc := generation.VoiceContext{}
	if v != nil && v.orgs != nil {
		if org, err := v.orgs.Get(ctx, orgID); err == nil && org != nil {
			vc.ProductDescription = org.ProductDescription
			vc.ICPNotes = org.ICPNotes
			vc.VoiceProfile = org.VoiceProfile
		}
	}
	return generation.BuildAgentVoiceRules(vc)
}

// StreamEvent is one SSE step emitted to the client.
type StreamEvent struct {
	Type             string `json:"type"`
	Text             string `json:"text,omitempty"`
	Tool             string `json:"tool,omitempty"`
	Risk             string `json:"risk,omitempty"`
	ArgsSummary      string `json:"args_summary,omitempty"`
	ToolCallID       string `json:"tool_call_id,omitempty"`
	Result           string `json:"result,omitempty"`
	Iteration        int    `json:"iteration,omitempty"`
	CreditsRemaining int    `json:"credits_remaining,omitempty"`
	Budget           int    `json:"budget,omitempty"`
	// FreeModel is true when the run is on an explicitly free/local backend
	// (AI_FREE): the client warns the user and no credits are charged.
	FreeModel bool   `json:"free_model,omitempty"`
	Code      string `json:"code,omitempty"`
	Message   string `json:"message,omitempty"`
	// Draft artifact (create_campaign_draft / create_automation_draft): the
	// client renders a card that deep-links into the real editor.
	EntityType string `json:"entity_type,omitempty"`
	EntityID   string `json:"entity_id,omitempty"`
	OpenURL    string `json:"open_url,omitempty"`
}

// toolResultEvent builds the tool_result SSE step, extracting a draft artifact
// (id + open url) so the client can render a deep-link card.
func toolResultEvent(tool, result string) StreamEvent {
	ev := StreamEvent{Type: evToolDone, Tool: tool, Result: summarize(tool, result)}
	var m map[string]any
	if err := json.Unmarshal([]byte(result), &m); err == nil {
		if id, ok := m["campaign_id"].(string); ok {
			ev.EntityType, ev.EntityID = "campaign", id
		} else if id, ok := m["automation_id"].(string); ok {
			ev.EntityType, ev.EntityID = "automation", id
		}
		if url, ok := m["open_url"].(string); ok {
			ev.OpenURL = url
		}
	}
	return ev
}

const (
	evText     = "text"
	evTool     = "tool_start"
	evToolDone = "tool_result"
	evApproval = "approval_required"
	evError    = "error"
	evDone     = "done"
)

// Service is the dashboard-agent application API.
type Service interface {
	CreateSession(ctx context.Context, orgID, userID uuid.UUID, page, resource string) (*models.AgentSession, *errx.Error)
	ListSessions(ctx context.Context, orgID, userID uuid.UUID, limit int, beforeCreatedAt time.Time, beforeID uuid.UUID) ([]models.AgentSession, error)
	GetSession(ctx context.Context, orgID, userID, sessionID uuid.UUID) (*models.AgentSession, error)

	// Transcript returns the session's persisted history hydrated into the same
	// turn/block shape the live stream renders, so a reopened tab looks identical
	// to a fresh run.
	Transcript(ctx context.Context, orgID, userID, sessionID uuid.UUID) ([]HydratedTurn, *errx.Error)

	// RunMessage streams a new user message's run. inv carries the caller's
	// identity + org permission bits. emit is called for each SSE step.
	RunMessage(ctx context.Context, inv aitools.Invocation, sessionID uuid.UUID, messageID, text, page, resource string, emit func(StreamEvent)) *errx.Error

	// Resume continues a paused run after the user's decision
	// (approve | deny | always_allow).
	Resume(ctx context.Context, inv aitools.Invocation, sessionID uuid.UUID, decision string, emit func(StreamEvent)) *errx.Error
}

type service struct {
	repo     repository.AgentRepository
	registry *aitools.Registry
	provider generation.Provider
	credits  credits.CreditService
	feature  FeatureGate
	audit    AuditLogger
	skills   SkillPreamble
	voice    VoicePreamble
}

func NewService(repo repository.AgentRepository, registry *aitools.Registry, provider generation.Provider, creditSvc credits.CreditService, feature FeatureGate, audit AuditLogger, skills SkillPreamble, voice VoicePreamble) Service {
	return &service{repo: repo, registry: registry, provider: provider, credits: creditSvc, feature: feature, audit: audit, skills: skills, voice: voice}
}

func (s *service) CreateSession(ctx context.Context, orgID, userID uuid.UUID, page, resource string) (*models.AgentSession, *errx.Error) {
	sess, err := s.repo.CreateSession(ctx, orgID, userID, "", models.AgentSessionContext{Page: page, Resource: resource})
	if err != nil {
		return nil, errx.New(errx.Internal, "failed to create session")
	}
	if s.audit != nil {
		s.audit.LogAction(ctx, orgID, userID, models.AuditActionCreate, models.AuditEntityAISession, &sess.ID, "", "", nil, nil)
	}
	return sess, nil
}

func (s *service) ListSessions(ctx context.Context, orgID, userID uuid.UUID, limit int, beforeCreatedAt time.Time, beforeID uuid.UUID) ([]models.AgentSession, error) {
	return s.repo.ListSessions(ctx, orgID, userID, limit, beforeCreatedAt, beforeID)
}

func (s *service) GetSession(ctx context.Context, orgID, userID, sessionID uuid.UUID) (*models.AgentSession, error) {
	return s.repo.GetSession(ctx, orgID, userID, sessionID)
}

// HydratedBlock is one rendered piece of a persisted turn, mirroring the
// client's live block model (text or tool step) so a reopened session renders
// identically to a live one.
type HydratedBlock struct {
	Kind        string `json:"kind"` // "text" | "tool"
	Text        string `json:"text,omitempty"`
	Tool        string `json:"tool,omitempty"`
	ArgsSummary string `json:"args_summary,omitempty"`
	Result      string `json:"result,omitempty"`
	EntityType  string `json:"entity_type,omitempty"`
	EntityID    string `json:"entity_id,omitempty"`
	OpenURL     string `json:"open_url,omitempty"`
	Done        bool   `json:"done"`
}

// HydratedTurn is one user or assistant turn rebuilt from the transcript.
type HydratedTurn struct {
	Role   string          `json:"role"` // "user" | "assistant"
	Blocks []HydratedBlock `json:"blocks"`
}

func (s *service) Transcript(ctx context.Context, orgID, userID, sessionID uuid.UUID) ([]HydratedTurn, *errx.Error) {
	msgs, xerr := s.loadTranscript(ctx, orgID, userID, sessionID)
	if xerr != nil {
		return nil, xerr
	}
	return hydrateTranscript(msgs), nil
}

// hydrateTranscript folds the stored provider-agnostic messages into the
// client's turn/block model. Consecutive assistant + tool messages collapse into
// one assistant turn (interleaved text and tool steps), matching how the live
// SSE loop appends events to the current assistant turn until the next user
// message.
func hydrateTranscript(msgs []generation.AgentMessage) []HydratedTurn {
	turns := make([]HydratedTurn, 0, len(msgs))
	// tool_call_id -> (turnIndex, blockIndex) so a tool result completes its step.
	loc := map[string][2]int{}
	ensureAssistant := func() int {
		if len(turns) == 0 || turns[len(turns)-1].Role != "assistant" {
			turns = append(turns, HydratedTurn{Role: "assistant"})
		}
		return len(turns) - 1
	}
	for _, m := range msgs {
		switch m.Role {
		case "user":
			turns = append(turns, HydratedTurn{Role: "user", Blocks: []HydratedBlock{{Kind: "text", Text: m.Content, Done: true}}})
		case "assistant":
			ti := ensureAssistant()
			if strings.TrimSpace(m.Content) != "" {
				turns[ti].Blocks = append(turns[ti].Blocks, HydratedBlock{Kind: "text", Text: m.Content, Done: true})
			}
			for _, tc := range m.ToolCalls {
				turns[ti].Blocks = append(turns[ti].Blocks, HydratedBlock{Kind: "tool", Tool: tc.Name, ArgsSummary: summarizeArgs(tc.Args)})
				loc[tc.ID] = [2]int{ti, len(turns[ti].Blocks) - 1}
			}
		case "tool":
			at, ok := loc[m.ToolCallID]
			if !ok {
				continue
			}
			ev := toolResultEvent(turns[at[0]].Blocks[at[1]].Tool, m.Content)
			b := &turns[at[0]].Blocks[at[1]]
			b.Done = true
			b.Result = ev.Result
			b.EntityType = ev.EntityType
			b.EntityID = ev.EntityID
			b.OpenURL = ev.OpenURL
		}
	}
	return turns
}

// errOutOfCredits / errCapExceeded are recorded by the credit PreIteration hook
// so the run can stop cleanly and the reason surfaced to the client.
var (
	errOutOfCredits = errors.New("insufficient credits")
	errCapExceeded  = errors.New("usage cap exceeded")
	errStopped      = errors.New("stopped")
)

func (s *service) RunMessage(ctx context.Context, inv aitools.Invocation, sessionID uuid.UUID, messageID, text, page, resource string, emit func(StreamEvent)) *errx.Error {
	if s.provider == nil {
		return errx.New(errx.ServiceUnavailable, "the AI assistant is not configured")
	}
	sess, err := s.repo.GetSession(ctx, inv.OrgID, inv.UserID, sessionID)
	if err != nil || sess == nil {
		return errx.New(errx.NotFound, "session not found")
	}
	text = strings.TrimSpace(text)
	if text == "" {
		return errx.New(errx.BadRequest, "message is required")
	}

	// Load prior transcript, append the user message, persist it.
	genMsgs, xerr := s.loadTranscript(ctx, inv.OrgID, inv.UserID, sessionID)
	if xerr != nil {
		return xerr
	}
	userMsg := generation.AgentMessage{Role: "user", Content: text}
	if perr := s.persist(ctx, inv.OrgID, inv.UserID, sessionID, []generation.AgentMessage{userMsg}, 0); perr != nil {
		return errx.New(errx.Internal, "failed to save message")
	}
	genMsgs = append(genMsgs, userMsg)

	// Update session context (page/resource) and set a title from the first
	// message.
	sess.Context.Page = page
	sess.Context.Resource = resource
	sess.Context.Pending = nil
	_ = s.repo.UpdateSessionContext(ctx, inv.OrgID, inv.UserID, sessionID, sess.Context)
	_ = s.repo.UpdateSessionTitle(ctx, inv.OrgID, inv.UserID, sessionID, deriveTitle(text))

	return s.runLoop(ctx, inv, sess, genMsgs, len(genMsgs), messageID, emit)
}

func (s *service) Resume(ctx context.Context, inv aitools.Invocation, sessionID uuid.UUID, decision string, emit func(StreamEvent)) *errx.Error {
	sess, err := s.repo.GetSession(ctx, inv.OrgID, inv.UserID, sessionID)
	if err != nil || sess == nil {
		return errx.New(errx.NotFound, "session not found")
	}
	pending := sess.Context.Pending
	if pending == nil {
		return errx.New(errx.BadRequest, "no tool awaiting approval")
	}

	genMsgs, xerr := s.loadTranscript(ctx, inv.OrgID, inv.UserID, sessionID)
	if xerr != nil {
		return xerr
	}
	baseline := len(genMsgs)

	// Always-allow persists an org policy for write tools (never send).
	if decision == "always_allow" && pending.Risk == string(generation.RiskWrite) {
		_ = s.repo.SetToolPolicy(ctx, inv.OrgID, pending.ToolName, "always_allow", inv.UserID)
	}

	assistant := generation.AgentMessage{Role: "assistant", ToolCalls: []generation.ToolCall{{
		ID: pending.ToolCallID, Name: pending.ToolName, Args: pending.Args,
	}}}

	var toolResult string
	if decision == "deny" {
		toolResult = `{"status":"denied","note":"The user declined to run this action."}`
	} else {
		emit(StreamEvent{Type: evTool, Tool: pending.ToolName, Risk: pending.Risk, ArgsSummary: pending.ArgsSummary, ToolCallID: pending.ToolCallID})
		// Resolve the pending tool from the invocation's full tool set (static
		// registry tools PLUS dynamic per-org tools like connected MCP servers),
		// which registry.Call does not cover.
		out, cerr := s.executeTool(ctx, inv, pending.ToolName, pending.Args)
		if cerr != nil {
			b, _ := json.Marshal(map[string]string{"error": cerr.Error()})
			out = string(b)
		}
		toolResult = out
		emit(toolResultEvent(pending.ToolName, out))
	}

	genMsgs = append(genMsgs,
		assistant,
		generation.AgentMessage{Role: "tool", ToolCallID: pending.ToolCallID, Content: toolResult},
	)

	// Clear the pending marker before continuing.
	sess.Context.Pending = nil
	_ = s.repo.UpdateSessionContext(ctx, inv.OrgID, inv.UserID, sessionID, sess.Context)

	// Give the resumed segment its own credit-idempotency namespace. Reusing the
	// original message id would collide with the pre-pause iterations (which
	// already consumed messageID:1..N), so the resumed loop's iterations 1..N
	// would replay for free. Pending is cleared above, so a resume is single-
	// shot and a fresh id is safe.
	return s.runLoop(ctx, inv, sess, genMsgs, baseline, "resume:"+uuid.NewString(), emit)
}

// runLoop drives one RunAgent segment: credit-charged per iteration, approval-
// gated, streamed, and persisted. baseline is the count of genMsgs already
// persisted (new tail beyond it is written on completion).
func (s *service) runLoop(ctx context.Context, inv aitools.Invocation, sess *models.AgentSession, genMsgs []generation.AgentMessage, baseline int, messageID string, emit func(StreamEvent)) *errx.Error {
	paid, _ := s.feature.IsPaidOrganization(ctx, inv.OrgID)
	model := s.provider.ModelForTier(paid)
	sess.Context.Model = model
	// Free/local backends (AI_FREE) run un-metered and warn the user.
	freeModel := s.provider.IsLocal()
	chargeCredits := !freeModel
	sess.Context.FreeModel = freeModel

	policies, _ := s.repo.GetToolPolicies(ctx, inv.OrgID)

	var (
		stopReason    string
		lastRemaining int
	)

	skillsBlock := ""
	if s.skills != nil {
		skillsBlock = s.skills.EnabledPreamble(ctx, inv.OrgID)
	}
	voiceBlock := ""
	if s.voice != nil {
		voiceBlock = s.voice.VoiceInstructions(ctx, inv.OrgID)
	}
	req := generation.AgentRequest{
		System:        s.systemPrompt(sess, voiceBlock, skillsBlock),
		Messages:      genMsgs,
		Tools:         s.registry.ToolDefs(ctx, inv),
		Model:         model,
		MaxIterations: DefaultRunBudget,
		OnEvent: func(ev generation.AgentEvent) {
			switch ev.Type {
			case generation.EventText:
				emit(StreamEvent{Type: evText, Text: ev.Text})
			case generation.EventToolStart:
				emit(StreamEvent{Type: evTool, Tool: ev.ToolName, ArgsSummary: summarizeArgs(ev.ToolArgs)})
			case generation.EventToolResult:
				emit(toolResultEvent(ev.ToolName, ev.ToolResult))
			}
		},
		PreIteration: func(ctx context.Context, iter int) error {
			// Free/local backend: no debit, no cap. Omit CreditsRemaining so the
			// client shows the free-model notice instead of a misleading balance.
			if !chargeCredits {
				emit(StreamEvent{Type: "iteration", Iteration: iter, Budget: DefaultRunBudget, FreeModel: true})
				return nil
			}
			key := messageID + ":" + strconv.Itoa(iter)
			remaining, cerr := s.credits.Consume(ctx, inv.OrgID, credits.CostAgentIteration, "agent_iteration", model, 0, key)
			if cerr != nil {
				switch {
				case errors.Is(cerr, credits.ErrInsufficientCredits):
					stopReason = "out_of_credits"
					return errOutOfCredits
				case errors.Is(cerr, credits.ErrCapExceeded):
					stopReason = "usage_cap"
					return errCapExceeded
				default:
					stopReason = "error"
					return errStopped
				}
			}
			lastRemaining = remaining
			emit(StreamEvent{Type: "iteration", Iteration: iter, CreditsRemaining: remaining, Budget: DefaultRunBudget})
			return nil
		},
		Approve: func(ctx context.Context, tool generation.ToolDef, call generation.ToolCall) error {
			// Send-class is always per-action (never auto-allowed). External MCP
			// tools are also never auto-allowed. Write-class auto-runs only when
			// an org policy says always_allow.
			if tool.Risk == generation.RiskWrite && policies[tool.Name] == "always_allow" && !strings.HasPrefix(tool.Name, "mcp_") {
				return nil
			}
			sess.Context.Pending = &models.PendingAgentTool{
				MessageID:   messageID,
				ToolCallID:  call.ID,
				ToolName:    call.Name,
				Risk:        string(tool.Risk),
				Args:        call.Args,
				ArgsSummary: summarizeArgs(call.Args),
			}
			emit(StreamEvent{
				Type: evApproval, Tool: call.Name, Risk: string(tool.Risk),
				ArgsSummary: summarizeArgs(call.Args), ToolCallID: call.ID,
			})
			return generation.ErrApprovalRequired
		},
	}

	result, rerr := s.provider.RunAgent(ctx, req)
	if rerr != nil {
		// The client only ever sees the generic message; the real cause goes to
		// the server log so a failing provider is debuggable.
		log.Printf("aiagent: run failed (org=%s session=%s provider=%s model=%s): %v", inv.OrgID, sess.ID, s.provider.Name(), model, rerr)
		// The credit for the failed iteration was charged before the model call
		// (PreIteration); refund it so the user is not billed for output they
		// never received. Nothing to refund on the free/local path.
		if chargeCredits {
			if bal, gerr := s.credits.Grant(ctx, inv.OrgID, credits.CostAgentIteration, "agent_iteration_refund"); gerr == nil {
				lastRemaining = bal
			}
		}
		emit(StreamEvent{Type: evError, Code: "provider_error", Message: "The assistant hit an error. Please try again.", CreditsRemaining: lastRemaining})
		return nil
	}

	// Persist the new transcript tail.
	if len(result.Messages) > baseline {
		if perr := s.persist(ctx, sess.OrgID, sess.UserID, sess.ID, result.Messages[baseline:], result.TokensUsed); perr != nil {
			log.Printf("aiagent: transcript persist failed (org=%s session=%s): %v", inv.OrgID, sess.ID, perr)
		}
	}

	switch result.StopReason {
	case "approval_required":
		_ = s.repo.UpdateSessionContext(ctx, sess.OrgID, sess.UserID, sess.ID, sess.Context)
		emit(StreamEvent{Type: evDone, CreditsRemaining: lastRemaining, Message: "awaiting_approval"})
	case "stopped":
		s.emitStop(emit, stopReason, lastRemaining)
	default: // "stop" or "max_iterations"
		sess.Context.Pending = nil
		_ = s.repo.UpdateSessionContext(ctx, sess.OrgID, sess.UserID, sess.ID, sess.Context)
		emit(StreamEvent{Type: evDone, CreditsRemaining: lastRemaining})
	}
	return nil
}

func (s *service) emitStop(emit func(StreamEvent), reason string, remaining int) {
	switch reason {
	case "out_of_credits":
		emit(StreamEvent{Type: evError, Code: "insufficient_credits", Message: "You're out of AI credits. Add more to keep using the assistant.", CreditsRemaining: remaining})
	case "usage_cap":
		emit(StreamEvent{Type: evError, Code: "usage_cap_exceeded", Message: "AI usage limit reached, please try again later.", CreditsRemaining: remaining})
	default:
		emit(StreamEvent{Type: evError, Code: "stopped", Message: "The run was stopped.", CreditsRemaining: remaining})
	}
}

// executeTool runs an approved tool by name, resolving it from the invocation's
// full tool set (static + dynamic per-org tools). This is how a resumed run
// executes a paused write/MCP tool, since registry.Call only knows static tools.
func (s *service) executeTool(ctx context.Context, inv aitools.Invocation, name string, args json.RawMessage) (string, error) {
	for _, d := range s.registry.ToolDefs(ctx, inv) {
		if d.Name == name {
			return d.Handler(ctx, args)
		}
	}
	return "", aitools.ErrToolNotFound
}

// --- helpers ---

func (s *service) loadTranscript(ctx context.Context, orgID, userID, sessionID uuid.UUID) ([]generation.AgentMessage, *errx.Error) {
	rows, err := s.repo.LoadTranscript(ctx, orgID, userID, sessionID)
	if err != nil {
		return nil, errx.New(errx.Internal, "failed to load conversation")
	}
	out := make([]generation.AgentMessage, 0, len(rows))
	for _, r := range rows {
		var m generation.AgentMessage
		if uerr := json.Unmarshal(r.Content, &m); uerr != nil {
			continue
		}
		out = append(out, m)
	}
	return out, nil
}

func (s *service) persist(ctx context.Context, orgID, userID, sessionID uuid.UUID, msgs []generation.AgentMessage, tokens int) error {
	rows := make([]models.AgentMessageRow, 0, len(msgs))
	for i, m := range msgs {
		b, err := json.Marshal(m)
		if err != nil {
			return err
		}
		t := 0
		if i == len(msgs)-1 {
			t = tokens // attribute the run's tokens to its last message
		}
		rows = append(rows, models.AgentMessageRow{Role: m.Role, Content: b, Tokens: t})
	}
	return s.repo.AppendMessages(ctx, orgID, userID, sessionID, rows)
}

func (s *service) systemPrompt(sess *models.AgentSession, voiceBlock, skillsBlock string) string {
	var b strings.Builder
	b.WriteString(`You are Warmbly's in-product AI assistant. You help the user manage their cold email outreach: contacts, campaigns, the unified inbox, CRM, and automations. Use the available tools to look things up and take actions. Be concise and specific.

Rules:
- Read tools run automatically. Write actions (creating or changing data) require the user's approval, which the product handles for you; just call the tool and it will be gated.
- Never claim you sent an email. You can draft replies, but the user always sends.
- When you create a draft campaign or automation, tell the user it is a draft and give them the link to open it.
- If a tool returns an error, explain it plainly and suggest a next step.
- Format answers in simple Markdown: short paragraphs, "-" lists, **bold** for key names and numbers, and fenced code blocks only for actual code or raw data. No tables and no headings.`)
	if strings.TrimSpace(voiceBlock) != "" {
		b.WriteString("\n\n")
		b.WriteString(voiceBlock)
	}
	if sess.Context.Page != "" || sess.Context.Resource != "" {
		fmt.Fprintf(&b, "\n\nThe user is currently on page %q", sess.Context.Page)
		if sess.Context.Resource != "" {
			fmt.Fprintf(&b, " looking at %q", sess.Context.Resource)
		}
		b.WriteString(". Use this as context for what they mean by \"this\" or \"here\".")
	}
	if strings.TrimSpace(skillsBlock) != "" {
		b.WriteString("\n\n")
		b.WriteString(skillsBlock)
	}
	return b.String()
}

// deriveTitle makes a short session title from the first user message.
func deriveTitle(text string) string {
	text = strings.TrimSpace(strings.ReplaceAll(text, "\n", " "))
	if len(text) > 60 {
		return truncateRunesTitle(text, 60)
	}
	return text
}

func truncateRunesTitle(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return strings.TrimSpace(string(r[:n])) + "…"
}

// summarizeArgs renders a short one-line summary of tool arguments for the
// approval card / step row.
func summarizeArgs(args json.RawMessage) string {
	if len(args) == 0 {
		return ""
	}
	var m map[string]any
	if err := json.Unmarshal(args, &m); err != nil {
		return ""
	}
	parts := make([]string, 0, len(m))
	for k, v := range m {
		parts = append(parts, fmt.Sprintf("%s=%v", k, v))
		if len(parts) >= 4 {
			break
		}
	}
	return truncateRunesTitle(strings.Join(parts, ", "), 160)
}

// summarize renders a short human line for a tool result step row.
func summarize(tool, result string) string {
	var m map[string]any
	if err := json.Unmarshal([]byte(result), &m); err == nil {
		if c, ok := m["count"]; ok {
			return fmt.Sprintf("%v result(s)", c)
		}
		if e, ok := m["error"]; ok {
			return fmt.Sprintf("error: %v", e)
		}
		if id, ok := m["campaign_id"]; ok {
			return fmt.Sprintf("created campaign %v", id)
		}
		if id, ok := m["automation_id"]; ok {
			return fmt.Sprintf("created automation %v", id)
		}
	}
	return truncateRunesTitle(result, 120)
}
