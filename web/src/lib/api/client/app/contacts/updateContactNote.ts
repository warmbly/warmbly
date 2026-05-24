import type ContactNote from "@/lib/api/models/app/crm/ContactNote";
import Request from "../../Request";

export default async function updateContactNote(contactId: string, noteId: string, data: Partial<ContactNote>): Promise<ContactNote> {
    return await Request<ContactNote>({
        method: "PATCH",
        url: `/contacts/${contactId}/notes/${noteId}`,
        data,
        authorization: true,
    })
}
