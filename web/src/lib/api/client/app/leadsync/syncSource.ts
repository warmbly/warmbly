import type { LeadSyncResult } from "@/lib/api/models/app/leadsync/LeadSync";
import Request from "../../Request";

// "Sync now": reads the full sheet tab, encodes rows as CSV in memory, and runs
// the existing contact ImportCommit (upsert by email, attach categories +
// target campaign). Naturally idempotent (email upsert), so no Idempotency-Key.
export default async function syncSource(id: string): Promise<LeadSyncResult> {
    return await Request<LeadSyncResult>({
        method: "POST",
        url: `/lead-sync/sources/${id}/sync`,
        authorization: true,
    });
}
