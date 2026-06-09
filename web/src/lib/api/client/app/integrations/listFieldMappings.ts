import type { IntegrationFieldMapping } from "@/lib/api/models/app/integrations/Integration";
import Request from "../../Request";

export default async function listFieldMappings(
    connectionId: string,
): Promise<{ mappings: IntegrationFieldMapping[] }> {
    return await Request<{ mappings: IntegrationFieldMapping[] }>({
        method: "GET",
        url: `/integrations/connections/${connectionId}/field-mappings`,
        authorization: true,
    });
}
