import Request from "../../Request";

export default async function rotateWebhookSecret(id: string): Promise<{ secret: string }> {
    return await Request<{ secret: string }>({
        method: "POST",
        url: `/webhooks/${id}/rotate-secret`,
        authorization: true,
    });
}
