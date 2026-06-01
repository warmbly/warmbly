import type { IntegrationConnection } from "@/lib/api/models/app/integrations/Integration";
import Request from "../../Request";

// Completes the OAuth handshake: the backend validates state, exchanges the
// code, resolves the connected account, and stores encrypted tokens.
export default async function finishIntegrationOAuth(input: {
    code: string;
    state: string;
}): Promise<IntegrationConnection> {
    return await Request<IntegrationConnection>({
        method: "POST",
        url: "/integrations/oauth/finish",
        data: input,
        authorization: true,
    });
}
