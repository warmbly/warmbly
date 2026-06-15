import Request from "../../Request";
import type { WebhookEndpointInput, WebhookEndpointWithSecret } from "@/lib/api/models/app/webhooks/Webhook";

export default async function createWebhook(data: WebhookEndpointInput): Promise<WebhookEndpointWithSecret> {
    return await Request<WebhookEndpointWithSecret>({
        method: "POST",
        url: `/webhooks`,
        data,
        authorization: true,
    });
}
