import type Deal from "@/lib/api/models/app/crm/Deal";
import Request from "../../../Request";

export default async function listDeals(): Promise<Deal[]> {
    return await Request<Deal[]>({
        method: "GET",
        url: `/crm/deals`,
        authorization: true,
    })
}
