import type { IntegrationConnection, IntegrationProvider } from "@/lib/api/models/app/integrations/Integration";
import Request from "../../Request";

export interface ConnectInput {
    provider: IntegrationProvider;
    label?: string;
    config: Record<string, unknown>;
}

export default async function connectIntegration(input: ConnectInput): Promise<IntegrationConnection> {
    return await Request<IntegrationConnection>({
        method: "POST",
        url: "/integrations/connections",
        data: input,
        authorization: true,
    });
}
