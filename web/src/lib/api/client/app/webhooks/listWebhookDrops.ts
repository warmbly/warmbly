import Request from "../../Request";
import type { WebhookDropsResult } from "@/lib/api/models/app/webhooks/Webhook";

export default async function listWebhookDrops(): Promise<WebhookDropsResult> {
    return await Request<WebhookDropsResult>({
        method: "GET",
        url: `/webhooks/throttle-drops`,
        authorization: true,
    });
}
