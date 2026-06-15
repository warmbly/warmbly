import Request from "../../Request";

// Doubles as "send a test event": the endpoint verifies on the first 2xx.
export default async function verifyWebhook(id: string): Promise<{ status: string }> {
    return await Request<{ status: string }>({
        method: "POST",
        url: `/webhooks/${id}/verify`,
        authorization: true,
    });
}
