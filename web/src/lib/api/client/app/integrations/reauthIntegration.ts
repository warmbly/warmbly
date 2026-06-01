import type { IntegrationOAuthStartResponse } from "@/lib/api/models/app/integrations/Integration";
import Request from "../../Request";

// Starts a fresh OAuth handshake for an existing connection whose token
// expired or was revoked. Returns a new authorization URL.
export default async function reauthIntegration(id: string): Promise<IntegrationOAuthStartResponse> {
    return await Request<IntegrationOAuthStartResponse>({
        method: "POST",
        url: `/integrations/oauth/reauth/${id}`,
        data: {},
        authorization: true,
    });
}
