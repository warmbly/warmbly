import type AuthCheck from "@/lib/api/models/app/emails/AuthCheck";
import Request from "../../Request";

// Re-runs the SPF/DKIM/DMARC lookup and RECORDS the verdict against every
// active mailbox on the sending domain. Separate from getAuthCheck because
// recording it is what lifts the cold-send and warmup gate, so it is a write.
export default async function refreshAuthCheck(id: string): Promise<AuthCheck> {
    return await Request<AuthCheck>({
        method: "POST",
        url: `/emails/${id}/auth-check`,
        authorization: true,
    });
}
