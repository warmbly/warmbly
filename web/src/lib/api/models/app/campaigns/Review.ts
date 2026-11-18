import type { LeadHold } from "@/lib/api/models/app/contacts/Contact";

// A lead's review state: waiting for approval, ready to start, paused for
// another reason, or already emailed.
export type ReviewStatus = "waiting" | "ready" | "held" | "started";

// The fields the readiness check counts blanks of.
export type MissingField = "FirstName" | "LastName" | "Company" | "Phone";

export const MISSING_FIELD_LABEL: Record<MissingField, string> = {
    FirstName: "first name",
    LastName: "last name",
    Company: "company",
    Phone: "phone",
};

export interface ReviewCounts {
    total: number;
    waiting: number;
    ready: number;
    held: number;
    started: number;
    personalized: number;
}

export interface CampaignReviewSummary {
    campaign_id: string;
    review_leads: boolean;
    counts: ReviewCounts;
    missing: { field: MissingField; leads: number }[];
    undeliverable: number;
    unsubscribed: number;
    ai: {
        step_id?: string;
        blocks: number;
        written: number;
        pending: number;
        estimated_cost_usd: number;
        // The campaign is running, so pending leads are being written on the server.
        auto?: boolean;
    };
    template_errors?: { step_id: string; position: number; message: string }[];
}

export interface ReviewLead {
    contact_id: string;
    email: string;
    first_name: string;
    last_name: string;
    company: string;
    status: ReviewStatus;
    hold?: LeadHold | null;
    personalized: number;
    missing?: MissingField[];
    added_at?: Date | null;
}

export interface ReviewLeadsPage {
    data: ReviewLead[];
    pagination: { next_cursor: string | null; has_more: boolean };
}

export interface ReviewLeadsFilter {
    status: "all" | ReviewStatus;
    missing?: MissingField;
    q?: string;
}

export interface LeadEmailStep {
    step_id: string;
    position: number;
    name: string;
    subject: string;
    body_html: string;
    body_plain: string;
    // The template this lead's copy renders from: their own copy, their A/B arm, or the step's.
    source: { subject: string; body_html: string; body_plain: string };
    threaded: boolean;
    personalized: boolean;
    edited_at?: Date | null;
    step_changed: boolean;
    variant_id?: string | null;
    ai_blocks?: { id: string; name: string; written: boolean }[];
    missing?: MissingField[];
    errors?: string[];
    sent: boolean;
    sent_at?: Date | null;
}

export interface LeadEmails {
    campaign_id: string;
    lead: ReviewLead;
    from?: { name: string; email: string } | null;
    steps: LeadEmailStep[];
}

export interface ReviewChange {
    campaign_id: string;
    review_leads: boolean;
    held: number;
    released: number;
}

export interface AIWritePass {
    written: number;
    failed: number;
    remaining: number;
    stopped?: "insufficient_credits" | "usage_cap_exceeded" | string;
    // Sent back as after on the next pass.
    next?: string;
}

export function leadName(l: Pick<ReviewLead, "first_name" | "last_name" | "email">): string {
    return `${l.first_name ?? ""} ${l.last_name ?? ""}`.trim() || l.email;
}
