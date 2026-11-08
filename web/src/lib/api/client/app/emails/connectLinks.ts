import Request from "../../Request";
import type { ConnectLink, ConnectLinkList, ConnectLinkUse, CreatedConnectLink, NewConnectLink } from "@/lib/api/models/app/emails/ConnectLink";

export async function listConnectLinks(params: { cursor?: string; limit?: number } = {}): Promise<ConnectLinkList> {
    return await Request<ConnectLinkList>({
        method: "GET",
        url: `/emails/client-links`,
        params: { cursor: params.cursor || undefined, limit: params.limit },
        authorization: true,
    });
}

// One Idempotency-Key per create, so a retried request replays the same link instead of minting a second.
export async function createConnectLink(body: NewConnectLink, idempotencyKey: string): Promise<CreatedConnectLink> {
    return await Request<CreatedConnectLink>({
        method: "POST",
        url: `/emails/client-links`,
        data: body,
        headers: { "Idempotency-Key": idempotencyKey },
        authorization: true,
    });
}

export async function revokeConnectLink(id: string): Promise<ConnectLink> {
    return await Request<ConnectLink>({
        method: "POST",
        url: `/emails/client-links/${encodeURIComponent(id)}/revoke`,
        authorization: true,
    });
}

export async function listConnectLinkUses(id: string): Promise<{ data: ConnectLinkUse[] }> {
    return await Request<{ data: ConnectLinkUse[] }>({
        method: "GET",
        url: `/emails/client-links/${encodeURIComponent(id)}/uses`,
        authorization: true,
    });
}
