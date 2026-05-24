import type AddContact from "@/lib/api/models/app/contacts/AddContact";
import type Contact from "@/lib/api/models/app/contacts/Contact";
import Request from "../../Request";

export default async function addContacts(contacts: AddContact[]): Promise<Contact[]> {
    return await Request<Contact[]>({
        method: "POST",
        url: "/contacts",
        data: contacts,
        authorization: true,
    })
}
