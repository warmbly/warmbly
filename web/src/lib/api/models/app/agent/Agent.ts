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
    code?: string;
    message?: string;
    entity_type?: string;
    entity_id?: string;
    open_url?: string;
}
