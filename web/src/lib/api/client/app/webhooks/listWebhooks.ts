import Request from "../../Request";
import type { WebhooksResult } from "@/lib/api/models/app/webhooks/Webhook";

export default async function listWebhooks(): Promise<WebhooksResult> {
    return await Request<WebhooksResult>({
        method: "GET",
        url: `/webhooks`,
        authorization: true,
    });
}
