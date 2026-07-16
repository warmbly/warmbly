import type { ContactResearchRun } from "@/lib/api/models/app/contacts/Research";
import Request from "../../Request";

// Run research for one contact (sync). Returns the terminal run.
export async function researchContact(
    contactId: string,
    objective: string,
): Promise<ContactResearchRun> {
    return await Request<ContactResearchRun>({
        method: "POST",
        url: `/contacts/${contactId}/research`,
        data: { objective },
        authorization: true,
    });
}

// List a contact's research history.
export async function listContactResearch(
    contactId: string,
): Promise<{ data: ContactResearchRun[] }> {
    return await Request<{ data: ContactResearchRun[] }>({
        method: "GET",
        url: `/contacts/${contactId}/research`,
        authorization: true,
    });
}

// Queue research for many contacts. Returns how many were queued.
export async function batchResearch(
    contactIds: string[],
    objective: string,
): Promise<{ queued: number }> {
    return await Request<{ queued: number }>({
        method: "POST",
        url: `/contacts/research/batch`,
        data: { contact_ids: contactIds, objective },
        authorization: true,
    });
}
