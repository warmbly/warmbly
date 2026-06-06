import type { LeadSyncSource } from "@/lib/api/models/app/leadsync/LeadSync";
import Request from "../../Request";

// Lists this org's saved sync sources, optionally filtered to one campaign
// (powers both the global Contacts > Sync sources area and the per-campaign
// "Connect a Google Sheet" list).
export default async function listSources(campaignId?: string): Promise<{ data: LeadSyncSource[] }> {
    return await Request<{ data: LeadSyncSource[] }>({
        method: "GET",
        url: "/lead-sync/sources",
        params: campaignId ? { campaign_id: campaignId } : undefined,
        authorization: true,
    });
}
