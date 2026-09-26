// Pure helpers for the contact filter bar, kept out of the component file so
// fast refresh keeps working.

import type SearchContacts from "@/lib/api/models/app/contacts/SearchContacts";
import type SearchContactsFilter from "@/lib/api/models/app/contacts/SearchContactsFilter";

export function isCompleteCustomFilter(f: SearchContactsFilter): boolean {
    return f.name.trim() !== "" && f.value.trim() !== "";
}

export function countActiveFilters(f: SearchContacts, campaignContext: boolean): number {
    let n = 0;
    n += f.custom_field_filters.filter(isCompleteCustomFilter).length;
    if (f.category_ids?.length) n++;
    if (f.segment_ids?.length) n++;
    if (!campaignContext && f.campaign_ids.length > 0) n++;
    if (f.subscribed !== undefined) n++;
    if (f.verification_status) n++;
    if (f.mail_hosts?.length) n++;
    if (f.min_campaigns !== undefined || f.max_campaigns !== undefined) n++;
    if (f.created_after || f.created_before) n++;
    if (f.updated_after || f.updated_before) n++;
    if (f.lead_status) n++;
    if (f.engagement) n++;
    return n;
}

// Whether anything narrows the list beyond its scope. `base` is the scope's
// own request (a campaign's or a segment's ids), which does not count as a
// filter; without it every id list does.
export function hasNarrowingFilters(f: SearchContacts, base?: SearchContacts): boolean {
    const same = (a?: string[], b?: string[]) => (a ?? []).join(",") === (b ?? []).join(",");
    return (
        !!f.query ||
        f.custom_field_filters.some(isCompleteCustomFilter) ||
        !same(f.campaign_ids, base?.campaign_ids) ||
        !same(f.segment_ids, base?.segment_ids) ||
        (f.category_ids?.length ?? 0) > 0 ||
        f.subscribed !== undefined ||
        !!f.verification_status ||
        (f.mail_hosts?.length ?? 0) > 0 ||
        !!f.lead_status ||
        !!f.engagement ||
        f.min_campaigns !== undefined ||
        f.max_campaigns !== undefined ||
        !!f.created_after ||
        !!f.created_before ||
        !!f.updated_after ||
        !!f.updated_before
    );
}

// The request for a list's own scope with nothing narrowing it.
export function scopeSearch(scope: { campaignId?: string; segmentId?: string }): SearchContacts {
    return {
        query: "",
        custom_field_filters: [],
        campaign_ids: scope.campaignId ? [scope.campaignId] : [],
        segment_ids: scope.segmentId ? [scope.segmentId] : undefined,
        sort_by: "created_at",
        reverse: false,
    };
}
