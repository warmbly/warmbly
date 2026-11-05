import type { LeadFilter, LeadStatus } from "@/lib/api/models/app/leads/Lead";

export type ReachFilter = "email" | "people" | "website" | "phone" | "socials" | "no_website";

export interface Refine {
    reach: ReachFilter[];
    statuses: LeadStatus[];
    hideContacts: boolean;
    hideDismissed: boolean;
    excludeChains: boolean;
    minRating: number | null;
    minReviews: number;
    maxReviews: number;
    minScore: number;
    category: string;
    tag: string;
    inView: boolean;
}

export const EMPTY_REFINE: Refine = {
    reach: [],
    statuses: [],
    hideContacts: false,
    hideDismissed: true,
    excludeChains: false,
    minRating: null,
    minReviews: 0,
    maxReviews: 0,
    minScore: 0,
    category: "",
    tag: "",
    inView: false,
};

// refineFilter turns the panel's state into the API filter.
export function refineFilter(r: Refine): LeadFilter {
    const f: LeadFilter = {};
    if (r.reach.includes("email")) f.has_email = true;
    if (r.reach.includes("people")) f.has_people = true;
    if (r.reach.includes("website")) f.has_website = true;
    if (r.reach.includes("no_website")) f.has_website = false;
    if (r.reach.includes("phone")) f.has_phone = true;
    if (r.reach.includes("socials")) f.has_socials = true;
    if (r.hideContacts) f.in_contacts = false;
    if (r.statuses.length) f.statuses = r.statuses;
    else if (r.hideDismissed) f.statuses = ["new", "saved", "contacted"];
    if (r.excludeChains) f.exclude_chains = true;
    if (r.minRating != null) f.min_rating = r.minRating;
    if (r.minReviews > 0) f.min_reviews = r.minReviews;
    if (r.maxReviews > 0) f.max_reviews = r.maxReviews;
    if (r.minScore > 0) f.min_score = r.minScore;
    if (r.category) f.category = r.category;
    if (r.tag) f.tag = r.tag;
    return f;
}

export function refineCount(r: Refine): number {
    const d = EMPTY_REFINE;
    return (
        r.reach.length +
        r.statuses.length +
        Number(r.hideContacts) +
        Number(r.excludeChains) +
        Number(r.hideDismissed !== d.hideDismissed) +
        Number(r.minRating != null) +
        Number(r.minReviews > 0) +
        Number(r.maxReviews > 0) +
        Number(r.minScore > 0) +
        Number(!!r.category) +
        Number(!!r.tag) +
        Number(r.inView)
    );
}

