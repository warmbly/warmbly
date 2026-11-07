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


export interface RefineChip {
    key: string;
    label: string;
    clear: (r: Refine) => Refine;
}

const REACH_LABEL: Record<ReachFilter, string> = {
    email: "Has email",
    people: "Has people",
    website: "Has website",
    phone: "Has phone",
    socials: "Has socials",
    no_website: "No website",
};

const STATUS_LABEL: Record<LeadStatus, string> = { new: "New", saved: "Saved", contacted: "Contacted", dismissed: "Dismissed" };

// refineChips lists each active filter as something the filter bar can show and clear.
export function refineChips(r: Refine): RefineChip[] {
    const out: RefineChip[] = r.reach.map((id) => ({
        key: `reach:${id}`,
        label: REACH_LABEL[id],
        clear: (x) => ({ ...x, reach: x.reach.filter((v) => v !== id) }),
    }));
    if (r.minRating != null) out.push({ key: "rating", label: `${r.minRating}+ stars`, clear: (x) => ({ ...x, minRating: null }) });
    if (r.minReviews > 0) out.push({ key: "minReviews", label: `${r.minReviews}+ reviews`, clear: (x) => ({ ...x, minReviews: 0 }) });
    if (r.maxReviews > 0) out.push({ key: "maxReviews", label: `Up to ${r.maxReviews} reviews`, clear: (x) => ({ ...x, maxReviews: 0 }) });
    if (r.minScore > 0) out.push({ key: "minScore", label: `Score ${r.minScore}+`, clear: (x) => ({ ...x, minScore: 0 }) });
    for (const s of r.statuses) {
        out.push({ key: `status:${s}`, label: STATUS_LABEL[s], clear: (x) => ({ ...x, statuses: x.statuses.filter((v) => v !== s) }) });
    }
    if (r.category) out.push({ key: "category", label: r.category, clear: (x) => ({ ...x, category: "" }) });
    if (r.tag) out.push({ key: "tag", label: `#${r.tag}`, clear: (x) => ({ ...x, tag: "" }) });
    if (r.inView) out.push({ key: "inView", label: "In map view", clear: (x) => ({ ...x, inView: false }) });
    if (r.hideContacts) out.push({ key: "hideContacts", label: "Not in contacts", clear: (x) => ({ ...x, hideContacts: false }) });
    if (r.excludeChains) out.push({ key: "excludeChains", label: "No chains", clear: (x) => ({ ...x, excludeChains: false }) });
    if (!r.hideDismissed && r.statuses.length === 0) out.push({ key: "dismissed", label: "Dismissed shown", clear: (x) => ({ ...x, hideDismissed: true }) });
    return out;
}
