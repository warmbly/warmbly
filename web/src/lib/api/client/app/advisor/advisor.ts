import type {
    AdvisorAgentResult,
    AdvisorFinding,
    AdvisorFindingsQuery,
    AdvisorSettings,
    AdvisorSummary,
} from "@/lib/api/models/app/advisor/Advisor";
import Request from "../../Request";

function buildQuery(q: AdvisorFindingsQuery): string {
    const params = new URLSearchParams();
    if (q.surface) params.set("surface", q.surface);
    if (q.category) params.set("category", q.category);
    if (q.entityType) params.set("entity_type", q.entityType);
    if (q.entityId) params.set("entity_id", q.entityId);
    if (q.limit) params.set("limit", String(q.limit));
    for (const s of q.status ?? []) params.append("status", s);
    const qs = params.toString();
    return qs ? `?${qs}` : "";
}

export async function getAdvisorFindings(q: AdvisorFindingsQuery = {}): Promise<AdvisorFinding[]> {
    const res = await Request<{ data: AdvisorFinding[] }>({
        method: "GET",
        url: `/advisor/recommendations${buildQuery(q)}`,
        authorization: true,
    });
    return res.data ?? [];
}

export async function getAdvisorSummary(): Promise<AdvisorSummary> {
    return await Request<AdvisorSummary>({
        method: "GET",
        url: "/advisor/summary",
        authorization: true,
    });
}

// Applying twice is a no-op that returns the first outcome, so a retry after a
// dropped response is safe.
export async function applyAdvisorFinding(id: string): Promise<AdvisorFinding> {
    return await Request<AdvisorFinding>({
        method: "POST",
        url: `/advisor/recommendations/${id}/apply`,
        authorization: true,
    });
}

// The agent fix, for findings with no deterministic action. Slow by nature:
// the agent reads the current state before it changes anything.
export async function agentFixAdvisorFinding(id: string): Promise<AdvisorAgentResult> {
    return await Request<AdvisorAgentResult>({
        method: "POST",
        url: `/advisor/recommendations/${id}/agent-fix`,
        authorization: true,
    });
}

export async function undoAdvisorFinding(id: string): Promise<AdvisorFinding> {
    return await Request<AdvisorFinding>({
        method: "POST",
        url: `/advisor/recommendations/${id}/undo`,
        authorization: true,
    });
}

export async function snoozeAdvisorFinding(id: string, days: number): Promise<void> {
    await Request({
        method: "POST",
        url: `/advisor/recommendations/${id}/snooze`,
        data: { days },
        authorization: true,
    });
}

export async function dismissAdvisorFinding(id: string, reason: string): Promise<void> {
    await Request({
        method: "POST",
        url: `/advisor/recommendations/${id}/dismiss`,
        data: { reason },
        authorization: true,
    });
}

export async function submitAdvisorFeedback(id: string, helpful: boolean, reason = ""): Promise<void> {
    await Request({
        method: "POST",
        url: `/advisor/recommendations/${id}/feedback`,
        data: { helpful, reason },
        authorization: true,
    });
}

export async function refreshAdvisor(): Promise<AdvisorSummary> {
    return await Request<AdvisorSummary>({
        method: "POST",
        url: "/advisor/refresh",
        authorization: true,
    });
}

export async function getAdvisorSettings(): Promise<AdvisorSettings> {
    return await Request<AdvisorSettings>({
        method: "GET",
        url: "/advisor/settings",
        authorization: true,
    });
}

export async function updateAdvisorSettings(s: Partial<AdvisorSettings>): Promise<AdvisorSettings> {
    return await Request<AdvisorSettings>({
        method: "PATCH",
        url: "/advisor/settings",
        data: s,
        authorization: true,
    });
}
