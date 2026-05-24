import type Contact from "@/lib/api/models/app/contacts/Contact";
import Request from "../../Request";
import type ContactUpdate from "@/lib/api/models/app/contacts/ContactUpdate";

export default async function updateContact(id: string, contact: Partial<ContactUpdate>): Promise<Contact> {
    return await Request<Contact>({
        method: "PATCH",
        url: `/contacts/${id}`,
        data: contact,
        authorization: true,
    })
}
