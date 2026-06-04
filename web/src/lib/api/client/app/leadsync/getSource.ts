import type { LeadSyncSource } from "@/lib/api/models/app/leadsync/LeadSync";
import Request from "../../Request";

// Returns one saved source (org-scoped).
export default async function getSource(id: string): Promise<LeadSyncSource> {
    return await Request<LeadSyncSource>({
        method: "GET",
        url: `/lead-sync/sources/${id}`,
        authorization: true,
    });
}
