import Request from "@/lib/api/client/Request";
import type { MailboxSignin } from "@/lib/api/models/app/emails/MailboxSources";

/** How the mailbox signs in and the switches that can succeed for it. */
export default async function getEmailSignin(id: string): Promise<MailboxSignin> {
    return await Request<MailboxSignin>({ method: "GET", url: `/emails/${id}/signin`, authorization: true });
}
