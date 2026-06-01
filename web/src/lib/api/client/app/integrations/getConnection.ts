import type { IntegrationConnectionDetail } from "@/lib/api/models/app/integrations/Integration";
import Request from "../../Request";

// Returns a single connection plus its event subscriptions and recent sync
// runs — the connection-management drawer payload.
export default async function getConnection(id: string): Promise<IntegrationConnectionDetail> {
    return await Request<IntegrationConnectionDetail>({
        method: "GET",
        url: `/integrations/connections/${id}`,
        authorization: true,
    });
}
