import Request from "../../Request";

// Rotates the app's webhook signing secret; the new value is returned once.
export default async function rotateOAuthAppWebhookSecret(id: string): Promise<{ webhook_secret: string }> {
    return await Request<{ webhook_secret: string }>({
        method: "POST",
        url: `/oauth/applications/${id}/webhook-secret/rotate`,
        authorization: true,
    });
}
