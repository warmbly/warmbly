// Dashboard AI agent types, mirroring internal/app/aiagent and models/agent.go.

export interface AgentSession {
    id: string;
    org_id: string;
    user_id: string;
    title: string;
    context: {
        page?: string;
        resource?: string;
        model?: string;
        pending?: PendingAgentTool | null;
    };
    created_at: string;
    updated_at: string;
}

export interface PendingAgentTool {
    tool_call_id: string;
    tool_name: string;
    risk: string;
    args_summary?: string;
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
        | "tool_start"
        | "tool_result"
        | "approval_required"
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
    credits_remaining?: number;
    budget?: number;
    // True when the run is on a free/local model (AI_LOCAL_MODEL): the client
    // warns the user and no credits are charged.
    free_model?: boolean;
    code?: string;
    message?: string;
    entity_type?: string;
    entity_id?: string;
    open_url?: string;
}
