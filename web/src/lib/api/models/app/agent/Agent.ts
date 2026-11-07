// Dashboard AI agent types, mirroring internal/app/aiagent and models/agent.go.

export interface AgentSession {
    id: string;
    org_id: string;
    user_id: string;
    // Owner display name; present only in workspaces with shared assistant
    // history, where the listing spans every member's conversations.
    user_name?: string;
    title: string;
    context: {
        page?: string;
        resource?: string;
        model?: string;
        pending?: PendingAgentTool | null;
    };
    created_at: Date;
    updated_at: Date;
}

export interface PendingAgentTool {
    tool_call_id: string;
    tool_name: string;
    risk: string;
    args_summary?: string;
    // Every argument as indented JSON with sorted keys, capped server-side.
    arguments?: string;
    arguments_truncated?: boolean;
    preview?: AgentSendPreview | null;
    // Only a member who manages settings is offered "Always allow", never for tools that always ask.
    always_allow_offered?: boolean;
}

// The resolved shape of a send awaiting approval.
export interface AgentSendPreview {
    from: string;
    to: string[];
    subject: string;
    body: string;
    body_html?: string;
}

// A workspace "always allow" policy for one assistant tool.
export interface AIToolPolicy {
    tool_name: string;
    decision: string;
    created_by?: string;
    created_by_name?: string;
    created_at: string;
}

export interface AgentSessionsPage {
    data: AgentSession[];
    pagination: { next_cursor: string | null; has_more: boolean };
}

// A persisted transcript block, hydrated server-side into the same shape the
// live stream renders (see internal/app/aiagent HydratedBlock).
export interface AgentHydratedBlock {
    kind: "text" | "tool";
    text?: string;
    tool?: string;
    args_summary?: string;
    result?: string;
    entity_type?: string;
    entity_id?: string;
    open_url?: string;
    done: boolean;
}

export interface AgentHydratedTurn {
    role: "user" | "assistant";
    blocks: AgentHydratedBlock[];
}

// GET /ai/sessions/:id/messages — a reopened session's history + any pending
// approval, so a tab rehydrates identically to a live run.
export interface AgentTranscript {
    title: string;
    turns: AgentHydratedTurn[];
    pending: PendingAgentTool | null;
    free_model?: boolean;
}

// AgentStreamEvent is one SSE step from a message/approval run.
export interface AgentStreamEvent {
    type:
        | "text"
        | "text_delta"
        | "tool_start"
        | "tool_result"
        | "approval_required"
        | "tool_secret"
        | "iteration"
        | "error"
        | "done";
    text?: string;
    tool?: string;
    risk?: string;
    args_summary?: string;
    tool_call_id?: string;
    result?: string;
    iteration?: number;
    /** The workspace balance in dollars after the step. */
    balance_usd?: number;
    budget?: number;
    // True when the run is on a free/local model (AI_LOCAL_MODEL): the client
    // warns the user and nothing is charged.
    free_model?: boolean;
    code?: string;
    message?: string;
    entity_type?: string;
    entity_id?: string;
    open_url?: string;
    arguments?: string;
    arguments_truncated?: boolean;
    preview?: AgentSendPreview | null;
    always_allow_offered?: boolean;
    // tool_secret: values shown once to the member, never stored.
    secrets?: Record<string, string>;
}
