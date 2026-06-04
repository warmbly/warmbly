import type { SheetMeta } from "@/lib/api/models/app/leadsync/LeadSync";
import Request from "../../Request";

// Returns a spreadsheet's title + tabs (read via the connection's refreshed
// Google token) so the UI can render a tab picker.
export default async function getSpreadsheet(input: {
    connection_id: string;
    sheet_id: string;
}): Promise<SheetMeta> {
    return await Request<SheetMeta>({
        method: "POST",
        url: "/lead-sync/google/spreadsheet",
        data: input,
        authorization: true,
    });
}
