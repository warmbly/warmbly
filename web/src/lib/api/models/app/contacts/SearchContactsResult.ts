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
}

export default interface SearchContactsResult {
    data: Contact[];
    pagination: Pagination;
    counts?: ContactsCounts;
}
