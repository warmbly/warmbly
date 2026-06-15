import Request from "../../Request";
import type { WebhookEndpoint } from "@/lib/api/models/app/webhooks/Webhook";

// Lists the per-org endpoints this app materialized, with their health.
export default async function listOAuthAppWebhookEndpoints(id: string): Promise<{ endpoints: WebhookEndpoint[] }> {
    const result = await Request<{ endpoints: WebhookEndpoint[] }>({
        method: "GET",
        url: `/oauth/applications/${id}/webhook-endpoints`,
        authorization: true,
    });
    return { endpoints: result.endpoints ?? [] };
}
