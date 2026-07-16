package models

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

// AgentSession is one dashboard-agent conversation. Sessions are per-user
// (private to the member who started them); the org scope is for tenancy only.
type AgentSession struct {
	ID        uuid.UUID           `json:"id"`
	OrgID     uuid.UUID           `json:"org_id"`
	UserID    uuid.UUID           `json:"user_id"`
	Title     string              `json:"title"`
	Context   AgentSessionContext `json:"context"`
	CreatedAt time.Time           `json:"created_at"`
	UpdatedAt time.Time           `json:"updated_at"`
}

// AgentSessionContext is the read-then-execute jsonb blob on a session: the
// client's page/resource awareness, the model chosen for the run, and any tool
// call paused awaiting approval. Validated at the app boundary (this struct),
// never filtered in SQL.
type AgentSessionContext struct {
	// Page / Resource mirror the presence shape the client already pushes
	// ({page, resource}); injected into the system prompt as context.
	Page     string `json:"page,omitempty"`
	Resource string `json:"resource,omitempty"`
	// Model is the provider model id resolved for this session's tier.
	Model string `json:"model,omitempty"`
	// FreeModel is true when the session ran on a free/local backend
	// (AI_FREE): persisted so a reopened tab still shows the warning.
	FreeModel bool `json:"free_model,omitempty"`
	// Pending is the tool call awaiting the user's approve/deny when a run is
	// paused; nil when the session is idle or running.
	Pending *PendingAgentTool `json:"pending,omitempty"`
}

// PendingAgentTool is a write/send tool call paused for human approval.
type PendingAgentTool struct {
	MessageID   string          `json:"message_id"`
	ToolCallID  string          `json:"tool_call_id"`
	ToolName    string          `json:"tool_name"`
	Risk        string          `json:"risk"`
	Args        json.RawMessage `json:"args"`
	ArgsSummary string          `json:"args_summary,omitempty"`
}

// AgentMessageRow is one persisted transcript turn. Content is the serialized
// provider-agnostic message (role, text, tool_calls, tool result) so a run
// resumes losslessly after an approval pause.
type AgentMessageRow struct {
	ID        uuid.UUID       `json:"id"`
	SessionID uuid.UUID       `json:"session_id"`
	Role      string          `json:"role"`
	Content   json.RawMessage `json:"content"`
	Tokens    int             `json:"tokens"`
	CreatedAt time.Time       `json:"created_at"`
}

// AIToolPolicy is a per-org "always allow this tool" decision. Only write-class
// tools can have a policy; send-class tools are never auto-allowed.
type AIToolPolicy struct {
	OrgID     uuid.UUID  `json:"org_id"`
	ToolName  string     `json:"tool_name"`
	Decision  string     `json:"decision"`
	CreatedBy *uuid.UUID `json:"created_by,omitempty"`
	CreatedAt time.Time  `json:"created_at"`
}
