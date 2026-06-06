import type {
    CreateLeadSyncSource,
    LeadSyncSource,
} from "@/lib/api/models/app/leadsync/LeadSync";
import Request from "../../Request";

// Saves a new on-demand source. The backend validates the connection is the
// org's google_sheets connection, dedup is valid, and column_mapping maps a
// column to 'email'.
export default async function createSource(
    input: CreateLeadSyncSource,
): Promise<LeadSyncSource> {
    return await Request<LeadSyncSource>({
        method: "POST",
        url: "/lead-sync/sources",
        data: input,
        authorization: true,
    });
}
