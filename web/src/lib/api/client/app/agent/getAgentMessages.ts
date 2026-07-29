import type { AgentTranscript } from "@/lib/api/models/app/agent/Agent";
import Request from "../../Request";

// Loads a session's hydrated transcript so a reopened workspace tab rehydrates
// its conversation (and any pending approval) from the server.
export default async function getAgentMessages(
    sessionId: string,
): Promise<AgentTranscript> {
    return await Request<AgentTranscript>({
        method: "GET",
        url: `/ai/sessions/${sessionId}/messages`,
        authorization: true,
    });
}
