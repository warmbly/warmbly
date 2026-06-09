import type { IntegrationFieldMapping } from "@/lib/api/models/app/integrations/Integration";
import Request from "../../Request";

export interface FieldMappingInput {
    warmbly_field: string;
    external_field: string;
    transform?: string;
    static_value?: string;
}

export interface ReplaceFieldMappingsInput {
    connectionId: string;
    object: string;
    mappings: FieldMappingInput[];
}

// Full-replace of a connection's default field map for one object. Idempotent.
export default async function replaceFieldMappings(
    input: ReplaceFieldMappingsInput,
): Promise<{ mappings: IntegrationFieldMapping[] }> {
    const { connectionId, ...body } = input;
    return await Request<{ mappings: IntegrationFieldMapping[] }>({
        method: "PUT",
        url: `/integrations/connections/${connectionId}/field-mappings`,
        data: body,
        authorization: true,
    });
}
