// Agency > Mailboxes: every client's mailboxes on one list, and the inbox
// vendor accounts the agency holds once for all of them.

import {
    infiniteQueryOptions,
    queryOptions,
    useInfiniteQuery,
    useMutation,
    useQuery,
    useQueryClient,
    type InfiniteData,
} from "@tanstack/react-query";
import {
    createAgencyVendor,
    deleteAgencyVendor,
    listAgencyMailboxes,
    listAgencyVendors,
    putAgencyVendorInClient,
    updateAgencyVendor,
} from "@/lib/api/client/app/agency/agencyMailboxes";
import { VENDORS_KEY } from "@/lib/api/hooks/app/emails/useMailboxVendors";
import type {
    AgencyMailboxFilters,
    AgencyMailboxPage,
    AgencyVendorAccountInput,
} from "@/lib/api/models/app/agency/AgencyMailboxes";

export const emptyMailboxFilters: AgencyMailboxFilters = { clients: [], attention: false, q: "" };

export const agencyMailboxesQuery = (agencyId: string, filters: AgencyMailboxFilters) =>
    infiniteQueryOptions<
        AgencyMailboxPage,
        Error,
        InfiniteData<AgencyMailboxPage, string | null>,
        ["agency", string, "mailboxes", AgencyMailboxFilters],
        string | null
    >({
        queryKey: ["agency", agencyId, "mailboxes", filters],
        queryFn: ({ pageParam, signal }) => listAgencyMailboxes(agencyId, filters, pageParam ?? undefined, signal),
        initialPageParam: null,
        getNextPageParam: (last) => (last.pagination.has_more ? last.pagination.next_cursor : undefined),
        staleTime: 15_000,
        refetchOnWindowFocus: true,
        // Other clients' events do not reach this socket; a slow poll covers them while the page is open.
        refetchInterval: 60_000,
        refetchIntervalInBackground: false,
    });

export const agencyVendorsQuery = (agencyId: string) =>
    queryOptions({
        queryKey: ["agency", agencyId, "vendors"],
        queryFn: ({ signal }) => listAgencyVendors(agencyId, signal),
        staleTime: 30_000,
    });

export function useAgencyMailboxes(agencyId: string | undefined, filters: AgencyMailboxFilters) {
    return useInfiniteQuery({
        ...agencyMailboxesQuery(agencyId ?? "", filters),
        enabled: !!agencyId,
    });
}

export function useAgencyVendors(agencyId?: string) {
    return useQuery({ ...agencyVendorsQuery(agencyId ?? ""), enabled: !!agencyId });
}

function useRefreshVendors(agencyId: string) {
    const qc = useQueryClient();
    return () => {
        void qc.invalidateQueries({ queryKey: ["agency", agencyId, "vendors"] });
        // A client workspace's own list shows the connection it was given.
        void qc.invalidateQueries({ queryKey: VENDORS_KEY });
    };
}

export function useSaveAgencyVendor(agencyId: string) {
    const refresh = useRefreshVendors(agencyId);
    return useMutation({
        mutationFn: ({ id, input, idempotencyKey }: { id?: string; input: AgencyVendorAccountInput; idempotencyKey?: string }) =>
            id ? updateAgencyVendor(agencyId, id, input) : createAgencyVendor(agencyId, input, idempotencyKey),
        onSettled: refresh,
    });
}

export function useDeleteAgencyVendor(agencyId: string) {
    const refresh = useRefreshVendors(agencyId);
    return useMutation({ mutationFn: (id: string) => deleteAgencyVendor(agencyId, id), onSettled: refresh });
}

/** Gives a client workspace its connection to one of the agency's accounts. */
export function useAgencyVendorForClient(agencyId: string) {
    const refresh = useRefreshVendors(agencyId);
    return useMutation({
        mutationFn: ({ id, orgId }: { id: string; orgId: string }) => putAgencyVendorInClient(agencyId, id, orgId),
        onSettled: refresh,
    });
}
