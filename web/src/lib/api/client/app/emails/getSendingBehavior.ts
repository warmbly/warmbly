import type SendingBehavior from "@/lib/api/models/app/emails/SendingBehavior";
import Request from "../../Request";

export default async function getSendingBehavior(id: string): Promise<SendingBehavior> {
    return await Request<SendingBehavior>({
        method: "GET",
        url: `/emails/${id}/behavior`,
        authorization: true,
    })
}
