import { useQuery } from "@tanstack/react-query";
import compareCampaigns from "@/lib/api/client/app/analytics/compareCampaigns";

function isoDay(d: Date): string {
    return d.toISOString().slice(0, 10);
}

export default function useCompareCampaigns(campaignIds: string[], days: number = 30) {
    const to = new Date();
    const from = new Date();
    from.setUTCDate(from.getUTCDate() - (days - 1));
    const fromStr = isoDay(from);
    const toStr = isoDay(to);
    return useQuery({
        queryKey: ["analytics", "campaigns", "compare", campaignIds, fromStr, toStr],
        queryFn: () => compareCampaigns(campaignIds, fromStr, toStr),
        enabled: campaignIds.length > 0,
    });
}
