import type GetCampaigns from "@/lib/api/models/app/campaigns/GetCampaigns";
import Request from "../../Request";
import { DEFAULT_PAGINATION_LIMIT } from "@/lib/information";

export default async function getCampaigns(query: string, cursor: string | null, folder: string | null, limit: number): Promise<GetCampaigns> {
    const params = new URLSearchParams();

    if (query) params.append("q", query);
    if (cursor) params.append("cursor", cursor);
    if (folder) params.append("folder", folder);
    if (limit !== DEFAULT_PAGINATION_LIMIT) params.append("limit", String(limit))

    const queryString = params.toString();
    const url = `/campaigns${queryString ? `?${queryString}` : ""}`;

    return await Request<GetCampaigns>({
        method: "GET",
        url,
        authorization: true,
    })
}
