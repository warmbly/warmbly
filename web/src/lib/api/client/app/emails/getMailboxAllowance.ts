import Request from "../../Request";
import type MailboxAllowance from "@/lib/api/models/app/emails/MailboxAllowance";

export default async function getMailboxAllowance(): Promise<MailboxAllowance> {
    return await Request<MailboxAllowance>({
        method: "GET",
        url: `/emails/allowance`,
        authorization: true,
    });
}
