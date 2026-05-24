import type Inbox from "@/lib/api/models/app/emails/Inbox";
import Request from "../../Request";

export default async function getEmail(id: string): Promise<Inbox> {
    return await Request<Inbox>({
        method: "GET",
        url: `/emails/${id}`,
        authorization: true,
    })
}
