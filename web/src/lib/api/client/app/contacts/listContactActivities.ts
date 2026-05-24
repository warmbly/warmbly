import type ContactActivity from "@/lib/api/models/app/crm/ContactActivity";
import Request from "../../Request";

export default async function listContactActivities(contactId: string): Promise<ContactActivity[]> {
    return await Request<ContactActivity[]>({
        method: "GET",
        url: `/contacts/${contactId}/activities`,
        authorization: true,
    })
}
