import type { LeadHold } from "@/lib/api/models/app/contacts/Contact";
import Request from "../../Request";

// One lead's hold inside one campaign: an out-of-office auto-reply parking the
// contact until they are back, or a member pausing them by hand.
export interface LeadHoldResult {
    campaign_id: string;
    contact_id: string;
    // Absent when the lead is not held.
    hold?: LeadHold | null;
}

// `until` omitted (or null) holds the lead with no end: only a resume lifts it.
export async function pauseCampaignLead(
    campaignId: string,
    contactId: string,
    body: { until?: string | null; reason?: string },
): Promise<LeadHoldResult> {
    return await Request<LeadHoldResult>({
        method: "POST",
        url: `/campaigns/${campaignId}/leads/${contactId}/pause`,
        data: body,
        authorization: true,
    });
}

export async function resumeCampaignLead(
    campaignId: string,
    contactId: string,
): Promise<LeadHoldResult> {
    return await Request<LeadHoldResult>({
        method: "POST",
        url: `/campaigns/${campaignId}/leads/${contactId}/resume`,
        authorization: true,
    });
}
