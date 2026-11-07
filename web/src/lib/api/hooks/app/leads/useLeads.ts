import { infiniteQueryOptions, queryOptions, useInfiniteQuery, useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import {
    addLeadPerson,
    addLeadsToContacts,
    deleteLeadPerson,
    deleteLeadSearch,
    deleteLeads,
    enrichLeads,
    findLeadPersonEmail,
    getLead,
    getLeadFacets,
    getLeadFinderConfig,
    getLeadPoints,
    listLeadEnrichments,
    listLeadSearches,
    queryLeads,
    searchLeads,
    updateLead,
    updateLeadPerson,
    updateLeadSearch,
    updateLeads,
} from "@/lib/api/client/app/leads/leads";
import type { Lead, LeadEnrichmentRun, LeadFilter, LeadPatch, LeadPerson, LeadPersonInput, LeadPoint, LeadSearch, LeadSort } from "@/lib/api/models/app/leads/Lead";

// Everything under ["leads"] is refreshed by the realtime spine on entity_type "lead".
export const LEADS_KEY = ["leads"] as const;

export const leadFinderConfigQuery = queryOptions({
    queryKey: [...LEADS_KEY, "config"],
    queryFn: getLeadFinderConfig,
    staleTime: 60_000,
});

export const leadSearchesQuery = queryOptions({
    queryKey: [...LEADS_KEY, "searches"],
    queryFn: listLeadSearches,
    staleTime: 15_000,
});

export const leadsListQuery = (filter: LeadFilter, sort: LeadSort) =>
    infiniteQueryOptions({
        queryKey: [...LEADS_KEY, "list", filter, sort],
        queryFn: ({ pageParam }) => queryLeads({ filter, sort, limit: 50, cursor: pageParam || undefined }),
        initialPageParam: "",
        getNextPageParam: (last) => last.pagination.next_cursor ?? undefined,
        staleTime: 10_000,
    });

export const leadPointsQuery = (filter: LeadFilter) =>
    queryOptions({
        queryKey: [...LEADS_KEY, "points", filter],
        queryFn: () => getLeadPoints(filter),
        staleTime: 10_000,
        placeholderData: (prev) => prev,
    });

export const leadFacetsQuery = (filter: LeadFilter) =>
    queryOptions({
        queryKey: [...LEADS_KEY, "facets", filter],
        queryFn: () => getLeadFacets(filter),
        staleTime: 30_000,
        placeholderData: (prev) => prev,
    });

export const leadQuery = (id: string) =>
    queryOptions({ queryKey: [...LEADS_KEY, "lead", id], queryFn: () => getLead(id), staleTime: 10_000 });

export function useLeadFinderConfig() {
    return useQuery(leadFinderConfigQuery);
}

export function useLeadSearches() {
    return useQuery(leadSearchesQuery);
}

// live refetches while a website run is going, so each row turns over as its lead is read.
export function useLeadsList(filter: LeadFilter, sort: LeadSort, live = false) {
    return useInfiniteQuery({ ...leadsListQuery(filter, sort), refetchInterval: live ? 3_000 : false });
}

export function useLeadPoints(filter: LeadFilter) {
    return useQuery(leadPointsQuery(filter));
}

export function useLeadFacets(filter: LeadFilter) {
    return useQuery(leadFacetsQuery(filter));
}

export function useLead(id: string | null) {
    return useQuery({ ...leadQuery(id ?? ""), enabled: !!id });
}

export const runActive = (r: LeadEnrichmentRun) => r.status === "queued" || r.status === "running";

// Website reads report progress only by polling: there is no per-lead event.
export function useLeadEnrichments() {
    return useQuery({
        queryKey: [...LEADS_KEY, "enrichments"],
        queryFn: listLeadEnrichments,
        staleTime: 2_000,
        refetchInterval: (q) => ((q.state.data ?? []).some(runActive) ? 2_000 : false),
    });
}

function useInvalidateLeads() {
    const qc = useQueryClient();
    return () => qc.invalidateQueries({ queryKey: LEADS_KEY });
}

export function useSearchLeads() {
    const invalidate = useInvalidateLeads();
    return useMutation({ mutationFn: searchLeads, onSuccess: () => invalidate() });
}

// Patching a lead is predictable, so the drawer and the pins change at once.
export function useUpdateLead() {
    const qc = useQueryClient();
    return useMutation({
        mutationFn: ({ id, patch }: { id: string; patch: LeadPatch }) => updateLead(id, patch),
        onMutate: async ({ id, patch }) => {
            const key = [...LEADS_KEY, "lead", id];
            await qc.cancelQueries({ queryKey: key });
            const prev = qc.getQueryData<Lead>(key);
            if (prev) qc.setQueryData<Lead>(key, { ...prev, ...patch } as Lead);
            const points = qc.getQueriesData<{ data: LeadPoint[]; truncated: boolean }>({ queryKey: [...LEADS_KEY, "points"] });
            if (patch.status) {
                for (const [k, v] of points) {
                    if (!v) continue;
                    qc.setQueryData(k, { ...v, data: v.data.map((p) => (p.id === id ? { ...p, status: patch.status! } : p)) });
                }
            }
            return { prev, points, key };
        },
        onError: (_e, _v, ctx) => {
            if (!ctx) return;
            if (ctx.prev) qc.setQueryData(ctx.key, ctx.prev);
            for (const [k, v] of ctx.points) qc.setQueryData(k, v);
        },
        onSettled: () => qc.invalidateQueries({ queryKey: LEADS_KEY }),
    });
}

export function useUpdateLeads() {
    const invalidate = useInvalidateLeads();
    return useMutation({ mutationFn: updateLeads, onSettled: () => invalidate() });
}

// Removed leads leave the map before the server answers.
export function useDeleteLeads() {
    const qc = useQueryClient();
    return useMutation({
        mutationFn: deleteLeads,
        onMutate: async (sel) => {
            if (!sel.lead_ids?.length) return { points: [] as [readonly unknown[], unknown][] };
            const gone = new Set(sel.lead_ids);
            await qc.cancelQueries({ queryKey: [...LEADS_KEY, "points"] });
            const points = qc.getQueriesData<{ data: LeadPoint[]; truncated: boolean }>({ queryKey: [...LEADS_KEY, "points"] });
            for (const [k, v] of points) {
                if (v) qc.setQueryData(k, { ...v, data: v.data.filter((p) => !gone.has(p.id)) });
            }
            return { points };
        },
        onError: (_e, _v, ctx) => {
            for (const [k, v] of ctx?.points ?? []) qc.setQueryData(k, v);
        },
        onSettled: () => qc.invalidateQueries({ queryKey: LEADS_KEY }),
    });
}

export function useEnrichLeads() {
    const invalidate = useInvalidateLeads();
    return useMutation({ mutationFn: enrichLeads, onSuccess: () => invalidate() });
}

export function useAddLeadsToContacts() {
    const qc = useQueryClient();
    return useMutation({
        mutationFn: addLeadsToContacts,
        onSuccess: () => {
            qc.invalidateQueries({ queryKey: LEADS_KEY });
            qc.invalidateQueries({ queryKey: ["contacts"] });
        },
    });
}

export function useUpdateLeadSearch() {
    const qc = useQueryClient();
    return useMutation({
        mutationFn: ({ id, patch }: { id: string; patch: { name?: string; saved?: boolean } }) => updateLeadSearch(id, patch),
        onMutate: async ({ id, patch }) => {
            const key = leadSearchesQuery.queryKey;
            await qc.cancelQueries({ queryKey: key });
            const prev = qc.getQueryData<LeadSearch[]>(key);
            if (prev) qc.setQueryData<LeadSearch[]>(key, prev.map((s) => (s.id === id ? { ...s, ...patch } : s)));
            return { prev };
        },
        onError: (_e, _v, ctx) => {
            if (ctx?.prev) qc.setQueryData(leadSearchesQuery.queryKey, ctx.prev);
        },
        onSettled: () => qc.invalidateQueries({ queryKey: leadSearchesQuery.queryKey }),
    });
}

export function useDeleteLeadSearch() {
    const qc = useQueryClient();
    return useMutation({
        mutationFn: deleteLeadSearch,
        onMutate: async (id) => {
            const key = leadSearchesQuery.queryKey;
            await qc.cancelQueries({ queryKey: key });
            const prev = qc.getQueryData<LeadSearch[]>(key);
            if (prev) qc.setQueryData<LeadSearch[]>(key, prev.filter((s) => s.id !== id));
            return { prev };
        },
        onError: (_e, _v, ctx) => {
            if (ctx?.prev) qc.setQueryData(leadSearchesQuery.queryKey, ctx.prev);
        },
        onSettled: () => qc.invalidateQueries({ queryKey: leadSearchesQuery.queryKey }),
    });
}

// Finding a person's address is decided by the server, so these wait for it,
// then write the answer straight into the open lead.
function usePersonResult() {
    const qc = useQueryClient();
    return (person: LeadPerson) => {
        const key = [...LEADS_KEY, "lead", person.lead_id];
        const lead = qc.getQueryData<Lead>(key);
        if (lead) {
            const people = lead.people ?? [];
            const exists = people.some((p) => p.id === person.id);
            qc.setQueryData<Lead>(key, {
                ...lead,
                people: exists ? people.map((p) => (p.id === person.id ? person : p)) : [...people, person],
                people_count: exists ? lead.people_count : lead.people_count + 1,
            });
        }
        qc.invalidateQueries({ queryKey: LEADS_KEY });
    };
}

export function useAddLeadPerson() {
    const settle = usePersonResult();
    return useMutation({
        mutationFn: ({ leadId, input }: { leadId: string; input: LeadPersonInput }) => addLeadPerson(leadId, input),
        onSuccess: settle,
    });
}

export function useFindLeadPersonEmail() {
    const settle = usePersonResult();
    return useMutation({
        mutationFn: ({ leadId, personId }: { leadId: string; personId: string }) => findLeadPersonEmail(leadId, personId),
        onSuccess: settle,
    });
}

// Editing a name, title or link is predictable; a new address is checked first.
export function useUpdateLeadPerson() {
    const qc = useQueryClient();
    const settle = usePersonResult();
    return useMutation({
        mutationFn: ({ leadId, personId, patch }: { leadId: string; personId: string; patch: LeadPersonInput }) =>
            updateLeadPerson(leadId, personId, patch),
        onMutate: async ({ leadId, personId, patch }) => {
            const key = [...LEADS_KEY, "lead", leadId];
            await qc.cancelQueries({ queryKey: key });
            const prev = qc.getQueryData<Lead>(key);
            if (prev && patch.email === undefined) {
                qc.setQueryData<Lead>(key, { ...prev, people: (prev.people ?? []).map((p) => (p.id === personId ? { ...p, ...patch } : p)) });
            }
            return { prev, key };
        },
        onError: (_e, _v, ctx) => {
            if (ctx?.prev) qc.setQueryData(ctx.key, ctx.prev);
        },
        onSuccess: settle,
    });
}

export function useDeleteLeadPerson() {
    const qc = useQueryClient();
    return useMutation({
        mutationFn: ({ leadId, personId }: { leadId: string; personId: string }) => deleteLeadPerson(leadId, personId),
        onMutate: async ({ leadId, personId }) => {
            const key = [...LEADS_KEY, "lead", leadId];
            await qc.cancelQueries({ queryKey: key });
            const prev = qc.getQueryData<Lead>(key);
            if (prev) {
                qc.setQueryData<Lead>(key, {
                    ...prev,
                    people: (prev.people ?? []).filter((p) => p.id !== personId),
                    people_count: Math.max(0, prev.people_count - 1),
                });
            }
            return { prev, key };
        },
        onError: (_e, _v, ctx) => {
            if (ctx?.prev) qc.setQueryData(ctx.key, ctx.prev);
        },
        onSettled: () => qc.invalidateQueries({ queryKey: LEADS_KEY }),
    });
}
