import type AuthCheck from "@/lib/api/models/app/emails/AuthCheck";
import Request from "../../Request";

// Runs a live SPF/DKIM/DMARC validation of the mailbox's sending domain.
// Drives the auth-check panel in the mailbox detail drawer.
export default async function getAuthCheck(id: string): Promise<AuthCheck> {
    return await Request<AuthCheck>({
        method: "GET",
        url: `/emails/${id}/auth-check`,
        authorization: true,
    });
}
