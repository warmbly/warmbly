import Request from "../../Request";
import type AddEmail from "@/lib/api/models/app/emails/AddEmail";
import type Inbox from "@/lib/api/models/app/emails/Inbox";

export default async function addEmailBulk(emails: AddEmail[]): Promise<Inbox[]> {
    return await Request<Inbox[]>({
        method: "POST",
        url: `/emails/other/bulk`,
        data: emails,
        authorization: true,
    })
}
