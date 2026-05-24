import type Inbox from "@/lib/api/models/app/emails/Inbox";
import Request from "../../Request";

export default async function updateEmail(id: string, email: Partial<Inbox>): Promise<Inbox> {
    return await Request<Inbox>({
        method: "PATCH",
        url: `/emails/${id}`,
        data: email,
        authorization: true,
    })
}
