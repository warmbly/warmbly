import type DailyStats from "@/lib/api/models/app/analytics/DailyStats";
import Request from "../../Request";

// GET /analytics/campaigns/:id/daily?from&to — the backend REQUIRES both date
// params (YYYY-MM-DD) and returns 400 without them. Returns a { data: [...] }
// envelope, which we unwrap to a real array.
export default async function getCampaignDailyStats(id: string, from: string, to: string): Promise<DailyStats[]> {
    const params = new URLSearchParams({ from, to });
    const res = await Request<{ data: DailyStats[] | null } | DailyStats[]>({
        method: "GET",
        url: `/analytics/campaigns/${id}/daily?${params.toString()}`,
        authorization: true,
    });
    if (Array.isArray(res)) return res;
    return res?.data ?? [];
}
