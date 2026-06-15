import Request from "../../Request";
import type { WebhookEventTypesResult } from "@/lib/api/models/app/webhooks/Webhook";

export default async function listWebhookEventTypes(): Promise<WebhookEventTypesResult> {
    return await Request<WebhookEventTypesResult>({
        method: "GET",
        url: `/webhooks/event-types`,
        authorization: true,
    });
}
