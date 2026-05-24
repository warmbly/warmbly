import type SearchContacts from "@/lib/api/models/app/contacts/SearchContacts";
import type SearchContactsResult from "@/lib/api/models/app/contacts/SearchContactsResult";
import Request from "../../Request";
import { DEFAULT_PAGINATION_LIMIT } from "@/lib/information";

export default async function searchContacts(options: SearchContacts, cursor: string | null, limit: number = DEFAULT_PAGINATION_LIMIT): Promise<SearchContactsResult> {
    const params = new URLSearchParams();

    if (cursor) params.append("cursor", cursor);
    if (limit !== DEFAULT_PAGINATION_LIMIT) params.append("limit", String(limit))

    const queryString = params.toString();
    const url = `/contacts/search${queryString ? `?${queryString}` : ""}`;
    return await Request<SearchContactsResult>({
        method: "POST",
        url,
        data: options,
        authorization: true,
    })
}
