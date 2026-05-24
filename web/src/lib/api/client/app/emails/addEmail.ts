import Request from "../../Request";
import type AddEmail from "@/lib/api/models/app/emails/AddEmail";
import type Inbox from "@/lib/api/models/app/emails/Inbox";

export default async function addEmail(email: AddEmail): Promise<Inbox> {
    return await Request<Inbox>({
        method: "POST",
        url: `/emails/other`,
        data: email,
        authorization: true,
    })
}
