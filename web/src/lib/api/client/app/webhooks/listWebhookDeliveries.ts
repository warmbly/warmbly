import Request from "../../Request";
import type { WebhookDeliveriesQuery, WebhookDeliveriesResult } from "@/lib/api/models/app/webhooks/Webhook";

// Lists delivery attempts. With an endpointId it hits the endpoint-scoped route;
// otherwise the org-wide log. Both support status/event_type/limit/cursor.
export default async function listWebhookDeliveries(
    query: WebhookDeliveriesQuery = {},
): Promise<WebhookDeliveriesResult> {
    const qs = new URLSearchParams();
    if (query.status) qs.set("status", query.status);
    if (query.eventType) qs.set("event_type", query.eventType);
    if (query.cursor) qs.set("cursor", query.cursor);
    if (query.limit) qs.set("limit", String(query.limit));
    const suffix = qs.toString() ? `?${qs.toString()}` : "";

    const url = query.endpointId
        ? `/webhooks/${query.endpointId}/deliveries${suffix}`
        : `/webhooks/deliveries${suffix}`;

    const result = await Request<WebhookDeliveriesResult>({
        method: "GET",
        url,
        authorization: true,
    });

    return {
        data: result.data ?? [],
        pagination: result.pagination ?? { has_more: false, next_cursor: null },
    };
}
