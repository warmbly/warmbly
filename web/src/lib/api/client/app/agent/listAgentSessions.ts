import type { AgentSessionsPage } from "@/lib/api/models/app/agent/Agent";
import Request from "../../Request";

export default async function listAgentSessions(
    limit = 20,
    cursor?: string,
): Promise<AgentSessionsPage> {
    const params = new URLSearchParams({ limit: String(limit) });
    if (cursor) params.set("cursor", cursor);
    return await Request<AgentSessionsPage>({
        method: "GET",
        url: `/ai/sessions?${params.toString()}`,
        authorization: true,
    });
}
