// Purchased domains and mailboxes across an agency's clients. Under ["agency", id, "store"], which
// useAgencyLive refreshes on the current workspace's STORE_* events; other clients' events never
// reach this socket, so focus and a slow poll while visible cover them.
import { queryOptions, useQuery } from "@tanstack/react-query";
import { getAgencyStore } from "@/lib/api/client/app/agency/agencyStore";

const STORE_POLL_MS = 120_000;

export const agencyStoreQuery = (agencyId: string) =>
    queryOptions({
        queryKey: ["agency", agencyId, "store"],
        queryFn: ({ signal }) => getAgencyStore(agencyId, signal),
        staleTime: 30_000,
        refetchOnWindowFocus: true,
        refetchInterval: STORE_POLL_MS,
        refetchIntervalInBackground: false,
        retry: 1,
    });

export function useAgencyStore(agencyId?: string) {
    return useQuery({ ...agencyStoreQuery(agencyId ?? ""), enabled: !!agencyId });
}

/** The nav entry shows only while the instance sells domains and mailboxes. */
export function useAgencyStoreEnabled(agencyId?: string): boolean {
    const q = useQuery({ ...agencyStoreQuery(agencyId ?? ""), enabled: !!agencyId, refetchInterval: false, select: (d) => d.enabled === true });
    return q.data === true;
}
