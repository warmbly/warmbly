import type { LeadSyncConnection } from "@/lib/api/models/app/leadsync/LeadSync";
import Request from "../../Request";

// Reports whether the org has a connected (hidden) google_sheets OAuth
// connection usable for lead-sync. Drives "Connect Google" vs the sheet picker.
export default async function getGoogleConnection(): Promise<LeadSyncConnection> {
    return await Request<LeadSyncConnection>({
        method: "GET",
        url: "/lead-sync/google/connection",
        authorization: true,
    });
}
