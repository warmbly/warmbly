import type {
    LeadSyncSource,
    UpdateLeadSyncSource,
} from "@/lib/api/models/app/leadsync/LeadSync";
import Request from "../../Request";

// Edits a saved source; omitted fields are left unchanged. clear_campaign:true
// detaches the target campaign.
export default async function updateSource(
    id: string,
    input: UpdateLeadSyncSource,
): Promise<LeadSyncSource> {
    return await Request<LeadSyncSource>({
        method: "PATCH",
        url: `/lead-sync/sources/${id}`,
        data: input,
        authorization: true,
    });
}
