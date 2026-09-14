import type SendIdentity from "@/lib/api/models/app/emails/SendIdentity";
import Request from "../../Request";

// Stored sending identity for one mailbox. Reads nothing from the provider.
export default async function getSendIdentity(id: string): Promise<SendIdentity> {
    return await Request<SendIdentity>({
        method: "GET",
        url: `/emails/${id}/identity`,
        authorization: true,
    });
}
