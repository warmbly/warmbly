import type { IntegrationEventSubscription } from "@/lib/api/models/app/integrations/Integration";
import Request from "../../Request";

export interface CreateConnectionEventInput {
    connectionId: string;
    event_type: string;
    action: string;
    config?: Record<string, unknown>;
    enabled?: boolean;
}

// Wires a Warmbly event (e.g. campaign.reply_received) to a provider action
// (e.g. slack.notify) on a connection.
export default async function createConnectionEvent(
    input: CreateConnectionEventInput,
): Promise<IntegrationEventSubscription> {
    const { connectionId, ...body } = input;
    return await Request<IntegrationEventSubscription>({
        method: "POST",
        url: `/integrations/connections/${connectionId}/events`,
        data: body,
        authorization: true,
    });
}
