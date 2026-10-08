import { useCallback, useContext, useEffect, useRef, useState, type Dispatch, type SetStateAction } from "react";
import useBrowseState from "@/hooks/useBrowseState";
import { UserContext } from "@/hooks/context/user";
import { useAppStore } from "@/stores/useAppStore";
import { browseIds, contactFiltersSchema, type ContactBrowseFilters } from "@/lib/browse-contacts-campaigns";
import type SearchContacts from "@/lib/api/models/app/contacts/SearchContacts";

export function contactBrowseName(campaignId?: string, segmentId?: string) {
    return campaignId ? `campaigns:${campaignId}:leads` : segmentId ? `contacts:segments:${segmentId}:members` : "contacts:all";
}

export default function useContactBrowseState({ campaignId, segmentId, category, initialSort, clearCategoryIntent }: {
    campaignId?: string;
    segmentId?: string;
    category?: string;
    initialSort: () => Pick<SearchContacts, "sort_by" | "reverse">;
    clearCategoryIntent?: () => void;
}): [SearchContacts, Dispatch<SetStateAction<SearchContacts>>] {
    const name = contactBrowseName(campaignId, segmentId);
    const userId = useContext(UserContext)?.user.id;
    const orgId = useAppStore((s) => s.currentOrganization?.id);
    const owner = `${userId}:${orgId}:${campaignId ? "campaign_leads" : "contacts"}`;
    const [filters, setFilters] = useBrowseState<ContactBrowseFilters>(`${name}:filters`, {
        query: "", custom_field_filters: [], campaign_ids: [],
    }, contactFiltersSchema);
    const linkedCategory = !campaignId && !segmentId ? category : undefined;
    const [categories, setCategories] = useBrowseState<string[]>(`${name}:labels`, [], browseIds,
        linkedCategory ? { initialOverride: [linkedCategory] } : undefined);
    const [intent, setIntent] = useState(linkedCategory);
    if (intent !== linkedCategory) {
        setIntent(linkedCategory);
        if (linkedCategory) setCategories([linkedCategory]);
    }
    // Sort remains owned by the existing server/localStorage view preferences.
    const [sortMemory, setSortMemory] = useState(() => ({ owner, value: initialSort() }));
    let sort = sortMemory.value;
    if (sortMemory.owner !== owner) {
        sort = initialSort();
        setSortMemory({ owner, value: sort });
    }
    const search: SearchContacts = {
        ...filters, ...sort,
        category_ids: intent !== linkedCategory && linkedCategory ? [linkedCategory] : categories.length ? categories : undefined,
        campaign_ids: campaignId ? [campaignId] : filters.campaign_ids,
        segment_ids: segmentId ? [segmentId] : filters.segment_ids,
        lead_status: campaignId ? filters.lead_status : undefined,
        engagement: campaignId ? filters.engagement : undefined,
    };
    const latest = useRef(search);
    latest.current = search;
    useEffect(() => {
        if (linkedCategory && !search.category_ids?.includes(linkedCategory)) clearCategoryIntent?.();
    }, [linkedCategory, search.category_ids, clearCategoryIntent]);
    const setSearch = useCallback<Dispatch<SetStateAction<SearchContacts>>>((update) => {
        const next = typeof update === "function" ? update(latest.current) : update;
        latest.current = next;
        setSortMemory((previous) => previous.owner === owner ? { owner, value: { sort_by: next.sort_by, reverse: next.reverse } } : previous);
        setCategories(next.category_ids ?? []);
        const { category_ids: _labels, sort_by: _sortBy, reverse: _reverse, ...browseFilters } = next;
        setFilters({
            ...browseFilters, campaign_ids: campaignId ? [] : next.campaign_ids,
            segment_ids: segmentId ? undefined : next.segment_ids,
            lead_status: campaignId ? next.lead_status : undefined,
            engagement: campaignId ? next.engagement : undefined,
        });
    }, [owner, campaignId, segmentId, setFilters, setCategories]);
    return [search, setSearch];
}
