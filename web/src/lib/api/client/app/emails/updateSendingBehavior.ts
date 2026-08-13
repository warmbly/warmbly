import type SendingBehavior from "@/lib/api/models/app/emails/SendingBehavior";
import type { SendingBehaviorPatch } from "@/lib/api/models/app/emails/SendingBehavior";
import Request from "../../Request";

export default async function updateSendingBehavior(id: string, patch: SendingBehaviorPatch): Promise<SendingBehavior> {
    return await Request<SendingBehavior>({
        method: "PUT",
        url: `/emails/${id}/behavior`,
        data: patch,
        authorization: true,
    })
}
