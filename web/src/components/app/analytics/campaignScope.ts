import type { DashboardCampaignFilter, DashboardScope } from "@/lib/api/models/app/analytics/DashboardOverview";

export const ALL_CAMPAIGNS: DashboardCampaignFilter = { campaigns: [], folders: [] };

export function isScoped(filter: DashboardCampaignFilter): boolean {
    return filter.campaigns.length > 0 || filter.folders.length > 0;
}

function plural(n: number, word: string): string {
    return `${n.toLocaleString()} ${word}${n === 1 ? "" : "s"}`;
}

// scopeLabel names a filter: "All campaigns", "Client A · 6 campaigns",
// one campaign's name, or a count. Names come from the server's echo first.
export function scopeLabel(
    filter: DashboardCampaignFilter,
    scope: DashboardScope | undefined,
    folderName: (id: string) => string | undefined,
    campaignName: (id: string) => string | undefined,
): string {
    if (!isScoped(filter)) return "All campaigns";
    const total = scope ? ` · ${plural(scope.campaign_count, "campaign")}` : "";
    const { campaigns, folders } = filter;
    if (folders.length === 1 && campaigns.length === 0) {
        const id = folders[0];
        const name = scope?.folders.find((f) => f.id === id)?.name ?? folderName(id);
        return `${name ?? "1 folder"}${total}`;
    }
    if (campaigns.length === 1 && folders.length === 0) {
        const id = campaigns[0];
        return scope?.campaigns.find((c) => c.id === id)?.name ?? campaignName(id) ?? "1 campaign";
    }
    if (folders.length === 0) return plural(scope?.campaign_count ?? campaigns.length, "campaign");
    const parts = [plural(folders.length, "folder")];
    if (campaigns.length) parts.push(plural(campaigns.length, "campaign"));
    return `${parts.join(" + ")}${total}`;
}
