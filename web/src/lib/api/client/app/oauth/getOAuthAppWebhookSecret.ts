import Request from "../../Request";

// Reveals the app's webhook signing secret (generated when a URL is first set).
export default async function getOAuthAppWebhookSecret(id: string): Promise<{ webhook_secret: string }> {
    return await Request<{ webhook_secret: string }>({
        method: "GET",
        url: `/oauth/applications/${id}/webhook-secret`,
        authorization: true,
    });
}
