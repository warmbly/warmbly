import type { IntegrationConnection } from "@/lib/api/models/app/integrations/Integration";
import Request from "../../Request";

export default async function listIntegrationConnections(): Promise<{ connections: IntegrationConnection[] }> {
    return await Request<{ connections: IntegrationConnection[] }>({
        method: "GET",
        url: "/integrations/connections",
        authorization: true,
    });
}
