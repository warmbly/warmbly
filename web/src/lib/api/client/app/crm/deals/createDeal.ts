import type Deal from "@/lib/api/models/app/crm/Deal";
import Request from "../../../Request";

export default async function createDeal(data: Partial<Deal>): Promise<Deal> {
    return await Request<Deal>({
        method: "POST",
        url: `/crm/deals`,
        data,
        authorization: true,
    })
}
