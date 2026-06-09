import type { IntegrationConnection, SyncDirection } from "@/lib/api/models/app/integrations/Integration";
import Request from "../../Request";

export interface UpdateConnectionConfigInput {
    connectionId: string;
    config_capabilities?: Record<string, unknown>;
    sync_direction?: SyncDirection;
}

// Saves a connection's onboarding/capability snapshot + sync direction (the
// "what is this integration for" settings, incl. a meeting provider's
// scheduling_url).
export default async function updateConnectionConfig(
    input: UpdateConnectionConfigInput,
): Promise<{ connection: IntegrationConnection }> {
    const { connectionId, ...body } = input;
    return await Request<{ connection: IntegrationConnection }>({
        method: "PATCH",
        url: `/integrations/connections/${connectionId}/config`,
        data: body,
        authorization: true,
    });
}
