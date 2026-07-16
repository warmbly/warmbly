import Request from "../../Request";

// AIThreadDraft mirrors the backend models.AIThreadDraft: a suggested reply the
// inbox agent drafted for an inbound human reply, awaiting human review.
export interface AIThreadDraft {
    id: string
    organization_id: string
    email_account_id: string
    owner_user_id: string
    thread_id: string
    source_message_id?: string | null
    contact_id?: string | null
    campaign_id?: string | null
    to_addr: string
    subject: string
    in_reply_to: string
    body: string
    intent_class: string
    confidence: number
    model: string
    status: "pending" | "approved" | "discarded"
    created_at: string
    updated_at: string
}

// listAgentDrafts returns the org's pending inbox-agent drafts, newest first.
export async function listAgentDrafts(): Promise<{ data: AIThreadDraft[] }> {
    return await Request<{ data: AIThreadDraft[] }>({
        method: "GET",
        url: "/unibox/agent-drafts",
        authorization: true,
    })
}

// approveAgentDraft sends the draft (optionally with an edited body) through the
// normal reply path and marks it approved. Never sends until called.
export async function approveAgentDraft(id: string, body?: string): Promise<{ task_id: string }> {
    return await Request<{ task_id: string }>({
        method: "POST",
        url: `/unibox/agent-drafts/${id}/approve`,
        data: body != null ? { body } : {},
        authorization: true,
    })
}

// discardAgentDraft dismisses a pending draft without sending.
export async function discardAgentDraft(id: string): Promise<{ status: string }> {
    return await Request<{ status: string }>({
        method: "POST",
        url: `/unibox/agent-drafts/${id}/discard`,
        authorization: true,
    })
}
