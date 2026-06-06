import type HourlyStats from "@/lib/api/models/app/analytics/HourlyStats";
import Request from "../../Request";

// GET /analytics/campaigns/:id/hourly?date=YYYY-MM-DD — pass the day explicitly
// (the backend keys hourly buckets on a date). Returns a { data: [...] } envelope.
export default async function getCampaignHourlyStats(id: string, date: string): Promise<HourlyStats[]> {
    const params = new URLSearchParams({ date });
    const res = await Request<{ data: HourlyStats[] | null } | HourlyStats[]>({
        method: "GET",
        url: `/analytics/campaigns/${id}/hourly?${params.toString()}`,
        authorization: true,
    });
    if (Array.isArray(res)) return res;
    return res?.data ?? [];
}
