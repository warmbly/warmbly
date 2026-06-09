import Request from "../../Request";

export interface WebhookSecretInfo {
    signing_secret: string;
    signature_header: string;
    scheme: string;
}

export default async function getWebhookSecret(connectionId: string): Promise<WebhookSecretInfo> {
    return await Request<WebhookSecretInfo>({
        method: "GET",
        url: `/integrations/connections/${connectionId}/webhook-secret`,
        authorization: true,
    });
}
