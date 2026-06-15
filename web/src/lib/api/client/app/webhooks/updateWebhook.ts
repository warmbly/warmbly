import Request from "../../Request";
import type { WebhookEndpoint, WebhookEndpointInput } from "@/lib/api/models/app/webhooks/Webhook";

export default async function updateWebhook(id: string, data: WebhookEndpointInput): Promise<WebhookEndpoint> {
    return await Request<WebhookEndpoint>({
        method: "PATCH",
        url: `/webhooks/${id}`,
        data,
        authorization: true,
    });
}
