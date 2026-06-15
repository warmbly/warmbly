import Request from "../../Request";

export default async function redeliverWebhookDelivery(deliveryId: string): Promise<{ status: string }> {
    return await Request<{ status: string }>({
        method: "POST",
        url: `/webhooks/deliveries/${deliveryId}/redeliver`,
        authorization: true,
    });
}
