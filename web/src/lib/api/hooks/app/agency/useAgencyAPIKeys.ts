import { type InfiniteData, infiniteQueryOptions, queryOptions, useInfiniteQuery, useMutation, useQuery, useQueryClient } from "@tanstack/react-query";

import {
    createAgencyAPIKey,
    listAgencyAPIKeyPermissions,
    listAgencyAPIKeys,
    revokeAgencyAPIKey,
    updateAgencyAPIKey,
} from "@/lib/api/client/app/agency/agencyApiKeys";
import type { AgencyAPIKey, AgencyAPIKeysPage, CreateAgencyAPIKey, UpdateAgencyAPIKey } from "@/lib/api/models/app/agency/AgencyAPIKey";

// Under ["agency", id], so the audit spine refreshes every staff member's list.
export const agencyAPIKeysQuery = (agencyId: string) =>
    infiniteQueryOptions<AgencyAPIKeysPage, Error, InfiniteData<AgencyAPIKeysPage, string | null>, ["agency", string, "api-keys"], string | null>({
        queryKey: ["agency", agencyId, "api-keys"],
        queryFn: ({ pageParam }) => listAgencyAPIKeys(agencyId, pageParam ?? undefined),
        initialPageParam: null,
        getNextPageParam: (last) => (last.pagination.has_more ? last.pagination.next_cursor : undefined),
        staleTime: 15_000,
    });

export const agencyAPIKeyPermissionsQuery = (agencyId: string) =>
    queryOptions({
        queryKey: ["agency", agencyId, "api-key-permissions"],
        queryFn: () => listAgencyAPIKeyPermissions(agencyId),
        staleTime: 10 * 60_000,
    });

export function useAgencyAPIKeys(agencyId: string | undefined, enabled = true) {
    return useInfiniteQuery({ ...agencyAPIKeysQuery(agencyId ?? ""), enabled: !!agencyId && enabled });
}

export function useAgencyAPIKeyPermissions(agencyId: string | undefined, enabled = true) {
    return useQuery({ ...agencyAPIKeyPermissionsQuery(agencyId ?? ""), enabled: !!agencyId && enabled });
}

type Pages = InfiniteData<AgencyAPIKeysPage, string | null>;

function patchKey(old: Pages | undefined, keyId: string, patch: (k: AgencyAPIKey) => AgencyAPIKey): Pages | undefined {
    if (!old) return old;
    return { ...old, pages: old.pages.map((p) => ({ ...p, data: p.data.map((k) => (k.id === keyId ? patch(k) : k)) })) };
}

export function useCreateAgencyAPIKey(agencyId: string) {
    const qc = useQueryClient();
    return useMutation({
        mutationFn: (data: CreateAgencyAPIKey) => createAgencyAPIKey(agencyId, data),
        onSettled: () => void qc.invalidateQueries({ queryKey: agencyAPIKeysQuery(agencyId).queryKey }),
    });
}

export function useUpdateAgencyAPIKey(agencyId: string) {
    const qc = useQueryClient();
    const key = agencyAPIKeysQuery(agencyId).queryKey;
    return useMutation({
        mutationFn: ({ keyId, data }: { keyId: string; data: UpdateAgencyAPIKey }) => updateAgencyAPIKey(agencyId, keyId, data),
        onMutate: async ({ keyId, data }) => {
            await qc.cancelQueries({ queryKey: key });
            const prev = qc.getQueryData<Pages>(key);
            qc.setQueryData<Pages>(key, (old) =>
                patchKey(old, keyId, (k) => ({ ...k, name: data.name ?? k.name, allowed_ips: data.allowed_ips ?? k.allowed_ips })),
            );
            return { prev };
        },
        onError: (_e, _v, ctx) => ctx?.prev && qc.setQueryData(key, ctx.prev),
        onSettled: () => void qc.invalidateQueries({ queryKey: key }),
    });
}

export function useRevokeAgencyAPIKey(agencyId: string) {
    const qc = useQueryClient();
    const key = agencyAPIKeysQuery(agencyId).queryKey;
    return useMutation({
        mutationFn: (keyId: string) => revokeAgencyAPIKey(agencyId, keyId),
        onMutate: async (keyId) => {
            await qc.cancelQueries({ queryKey: key });
            const prev = qc.getQueryData<Pages>(key);
            qc.setQueryData<Pages>(key, (old) => patchKey(old, keyId, (k) => ({ ...k, status: "revoked" })));
            return { prev };
        },
        onError: (_e, _v, ctx) => ctx?.prev && qc.setQueryData(key, ctx.prev),
        onSettled: () => void qc.invalidateQueries({ queryKey: key }),
    });
}
