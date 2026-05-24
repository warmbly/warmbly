import type BulkEditContacts from "@/lib/api/models/app/contacts/BulkEditContacts";
import type Contact from "@/lib/api/models/app/contacts/Contact";
import Request from "../../Request";

export default async function updateContactsBulk(options: BulkEditContacts): Promise<Contact[]> {
    return await Request<Contact[]>({
        method: "PATCH",
        url: "/contacts",
        data: options,
        authorization: true,
    })
}
