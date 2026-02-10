import type ContactNote from "@/lib/api/models/app/crm/ContactNote";
import Request from "../../Request";

export default async function listContactNotes(contactId: string): Promise<ContactNote[]> {
    return await Request<ContactNote[]>({
        method: "GET",
        url: `/contacts/${contactId}/notes`,
        authorization: true,
    })
}
