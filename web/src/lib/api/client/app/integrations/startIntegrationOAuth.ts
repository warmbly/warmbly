import type { IntegrationOAuthStartResponse } from "@/lib/api/models/app/integrations/Integration";
import Request from "../../Request";

// Starts an OAuth handshake. Returns the provider authorization URL the SPA
// opens in a popup; the backend mints + stores the CSRF state / PKCE verifier.
export default async function startIntegrationOAuth(input: {
    provider: string;
    label?: string;
}): Promise<IntegrationOAuthStartResponse> {
    return await Request<IntegrationOAuthStartResponse>({
        method: "POST",
        url: "/integrations/oauth/start",
        data: input,
        authorization: true,
    });
}
