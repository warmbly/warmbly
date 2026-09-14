import type Pagination from "../Pagination";
import type Contact from "./Contact";

export interface ContactCategoryCount {
    category_id: string;
    count: number;
}

// Org-wide contact facet totals, returned on the first page (no cursor) of a
// search. Independent of the request filters, so they drive stable browse
// stats regardless of what's currently filtered or how many rows are loaded.
export interface ContactsCounts {
    total: number;
    subscribed: number;
    unsubscribed: number;
    in_campaign: number;
    not_contacted: number;
    categories: ContactCategoryCount[];
    verification?: ContactVerificationCounts;
}

// Org contacts by verification verdict. pending is the subset of unknown
// nobody has checked yet.
export interface ContactVerificationCounts {
    valid: number;
    risky: number;
    invalid: number;
    unknown: number;
    pending: number;
}

// Per-status lead totals for one campaign's Leads view. Returned on the first
// page when the search targets exactly one campaign, and independent of the
// request's lead_status filter, so every chip shows the campaign's real total
// rather than a count over the rows that happen to be loaded.
export interface CampaignLeadCounts {
    total: number;
    queued: number;
    processing: number;
    completed: number;
    replied: number;
    bounced: number;
    failed: number;
    unsubscribed: number;
    // Leads whose flow is held (an out-of-office auto-reply, or a member
    // pausing them) and resumes where it stopped.
    paused: number;
    // Leads the campaign will never send to: address verification refused them.
    undeliverable: number;
    // Engagement totals matching the `engagement` filter: leads sent at least
    // one step, and of those the ones with a human open, click, or reply.
    contacted: number;
    opened: number;
    clicked: number;
    replied_any: number;
}

export default interface SearchContactsResult {
    data: Contact[];
    pagination: Pagination;
    counts?: ContactsCounts;
    lead_counts?: CampaignLeadCounts;
}
