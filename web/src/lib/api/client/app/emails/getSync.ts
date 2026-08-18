import type EmailSync from "@/lib/api/models/app/emails/SyncState";
import Request from "../../Request";

// Backfill progress and fair-use status for one mailbox.
export default async function getSync(id: string): Promise<EmailSync> {
    return await Request<EmailSync>({
        method: "GET",
        url: `/emails/${id}/sync`,
        authorization: true,
    });
}
