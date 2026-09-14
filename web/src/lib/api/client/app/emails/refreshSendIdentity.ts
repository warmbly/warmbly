import type SendIdentity from "@/lib/api/models/app/emails/SendIdentity";
import Request from "../../Request";

// Re-reads the send-as addresses from the provider and stores them. Importing
// the provider's signature is opt-in because it overwrites the stored one.
export default async function refreshSendIdentity(id: string, importSignature = false): Promise<SendIdentity> {
    return await Request<SendIdentity>({
        method: "POST",
        url: `/emails/${id}/identity/refresh`,
        data: { import_signature: importSignature },
        authorization: true,
    });
}
