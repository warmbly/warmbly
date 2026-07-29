import type { AgentSession } from "@/lib/api/models/app/agent/Agent";
import Request from "../../Request";

export default async function createAgentSession(data: {
    page?: string;
    resource?: string;
}): Promise<AgentSession> {
    return await Request<AgentSession>({
        method: "POST",
        url: `/ai/sessions`,
        data,
        authorization: true,
    });
}
