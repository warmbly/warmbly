import type { ImportPreview } from "@/lib/api/client/app/contacts/importContacts";
import Request from "../../Request";

// Reads the first ~21 rows of a tab and returns the exact contacts
// ImportPreview JSON shape, so the frontend reuses its contact-import column
// mapper verbatim.
export default async function previewSheet(input: {
    connection_id: string;
    sheet_id: string;
    tab_title: string;
}): Promise<ImportPreview> {
    return await Request<ImportPreview>({
        method: "POST",
        url: "/lead-sync/google/preview",
        data: input,
        authorization: true,
    });
}
