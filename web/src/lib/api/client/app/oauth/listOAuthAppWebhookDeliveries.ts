import Request from "../../Request";
import type { WebhookDeliveriesQuery, WebhookDeliveriesResult } from "@/lib/api/models/app/webhooks/Webhook";

// Lists the app's cross-org webhook delivery log. Supports status/event_type/limit/cursor.
export default async function listOAuthAppWebhookDeliveries(
    id: string,
    query: Omit<WebhookDeliveriesQuery, "endpointId"> = {},
): Promise<WebhookDeliveriesResult> {
    const qs = new URLSearchParams();
    if (query.status) qs.set("status", query.status);
    if (query.eventType) qs.set("event_type", query.eventType);
    if (query.cursor) qs.set("cursor", query.cursor);
    if (query.limit) qs.set("limit", String(query.limit));
    const suffix = qs.toString() ? `?${qs.toString()}` : "";

    const result = await Request<WebhookDeliveriesResult>({
        method: "GET",
        url: `/oauth/applications/${id}/webhook-deliveries${suffix}`,
        authorization: true,
    });

    return {
        data: result.data ?? [],
        pagination: result.pagination ?? { has_more: false, next_cursor: null },
    };
}
