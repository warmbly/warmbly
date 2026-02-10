import type UniboxEmail from "@/lib/api/models/app/unibox/UniboxEmail";
import Request from "../../Request";

export default async function getIncoming(accountId?: string, cursor?: string): Promise<UniboxEmail[]> {
    const params = new URLSearchParams();
    if (accountId) params.append("account_id", accountId);
    if (cursor) params.append("cursor", cursor);
    const queryString = params.toString();
    const url = `/unibox${queryString ? `?${queryString}` : ""}`;

    return await Request<UniboxEmail[]>({
        method: "GET",
        url,
        authorization: true,
    })
}
