import type Deal from "@/lib/api/models/app/crm/Deal";
import Request from "../../../Request";

export default async function getDeal(id: string): Promise<Deal> {
    return await Request<Deal>({
        method: "GET",
        url: `/crm/deals/${id}`,
        authorization: true,
    })
}
