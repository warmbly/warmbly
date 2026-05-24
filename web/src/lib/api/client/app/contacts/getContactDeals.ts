import type Deal from "@/lib/api/models/app/crm/Deal";
import Request from "../../Request";

export default async function getContactDeals(contactId: string): Promise<Deal[]> {
    return await Request<Deal[]>({
        method: "GET",
        url: `/contacts/${contactId}/deals`,
        authorization: true,
    })
}
