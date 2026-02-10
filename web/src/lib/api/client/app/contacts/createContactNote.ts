import type ContactNote from "@/lib/api/models/app/crm/ContactNote";
import Request from "../../Request";

export default async function createContactNote(contactId: string, data: { content: string }): Promise<ContactNote> {
    return await Request<ContactNote>({
        method: "POST",
        url: `/contacts/${contactId}/notes`,
        data,
        authorization: true,
    })
}
