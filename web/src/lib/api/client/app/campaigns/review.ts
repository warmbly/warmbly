import type {
    AIWritePass,
    CampaignReviewSummary,
    LeadEmails,
    ReviewChange,
    ReviewLeadsFilter,
    ReviewLeadsPage,
} from "@/lib/api/models/app/campaigns/Review";
import Request from "../../Request";

export async function getCampaignReview(campaignId: string): Promise<CampaignReviewSummary> {
    return await Request<CampaignReviewSummary>({ method: "GET", url: `/campaigns/${campaignId}/review`, authorization: true });
}

export async function setCampaignReview(campaignId: string, enabled: boolean): Promise<ReviewChange> {
    return await Request<ReviewChange>({ method: "PUT", url: `/campaigns/${campaignId}/review`, data: { enabled }, authorization: true });
}

export async function getCampaignReviewLeads(
    campaignId: string,
    filter: ReviewLeadsFilter,
    cursor: string | null,
    limit = 50,
): Promise<ReviewLeadsPage> {
    const params = new URLSearchParams({ status: filter.status, limit: String(limit) });
    if (filter.missing) params.set("missing", filter.missing);
    if (filter.q?.trim()) params.set("q", filter.q.trim());
    if (cursor) params.set("cursor", cursor);
    return await Request<ReviewLeadsPage>({
        method: "GET",
        url: `/campaigns/${campaignId}/review/leads?${params.toString()}`,
        authorization: true,
    });
}

// Either contact_ids or all.
export async function approveCampaignLeads(campaignId: string, body: { contact_ids?: string[]; all?: boolean }): Promise<ReviewChange> {
    return await Request<ReviewChange>({ method: "POST", url: `/campaigns/${campaignId}/review/approve`, data: body, authorization: true });
}

export async function reopenCampaignLeads(campaignId: string, contactIds: string[]): Promise<ReviewChange> {
    return await Request<ReviewChange>({
        method: "POST",
        url: `/campaigns/${campaignId}/review/reopen`,
        data: { contact_ids: contactIds },
        authorization: true,
    });
}

// One pass of writing AI copy ahead; repeat with the returned next as after while remaining > 0 and stopped is unset.
export async function writeCampaignReviewAI(
    campaignId: string,
    body: { step_id?: string; contact_ids?: string[]; waiting_only?: boolean; after?: string },
): Promise<AIWritePass> {
    return await Request<AIWritePass>({ method: "POST", url: `/campaigns/${campaignId}/review/write`, data: body, authorization: true });
}

export async function getLeadEmails(campaignId: string, contactId: string): Promise<LeadEmails> {
    return await Request<LeadEmails>({ method: "GET", url: `/campaigns/${campaignId}/leads/${contactId}/emails`, authorization: true });
}

export async function putLeadEmail(
    campaignId: string,
    contactId: string,
    stepId: string,
    body: { subject: string; body_html: string; body_plain: string },
): Promise<unknown> {
    return await Request<unknown>({
        method: "PUT",
        url: `/campaigns/${campaignId}/leads/${contactId}/emails/${stepId}`,
        data: body,
        authorization: true,
    });
}

export async function deleteLeadEmail(campaignId: string, contactId: string, stepId: string): Promise<void> {
    await Request<unknown>({ method: "DELETE", url: `/campaigns/${campaignId}/leads/${contactId}/emails/${stepId}`, authorization: true });
}

// rewrite replaces copy already written, and is charged again.
export async function writeLeadEmailAI(campaignId: string, contactId: string, stepId: string, rewrite = false): Promise<LeadEmails> {
    return await Request<LeadEmails>({
        method: "POST",
        url: `/campaigns/${campaignId}/leads/${contactId}/emails/${stepId}/write`,
        data: rewrite ? { rewrite: true } : undefined,
        authorization: true,
    });
}
