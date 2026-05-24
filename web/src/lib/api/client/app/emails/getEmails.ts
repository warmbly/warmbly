import type GetEmails from "@/lib/api/models/app/emails/GetEmails";
import Request from "../../Request";
import { DEFAULT_PAGINATION_LIMIT } from "@/lib/information";

export default async function getEmails(query: string, cursor: string | null, tag: string | null, limit: number = DEFAULT_PAGINATION_LIMIT): Promise<GetEmails> {
    const params = new URLSearchParams();

    if (query) params.append("q", query);
    if (cursor) params.append("cursor", cursor);
    if (tag) params.append("tag", tag);
    if (limit !== DEFAULT_PAGINATION_LIMIT) params.append("limit", String(limit));

    const queryString = params.toString();
    const url = `/emails${queryString ? `?${queryString}` : ""}`;

    return await Request<GetEmails>({
        method: "GET",
        url: url,
        authorization: true,
    })
}
