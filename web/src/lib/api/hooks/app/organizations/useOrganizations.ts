import { queryOptions, useQuery } from "@tanstack/react-query";
import getOrganizations from "@/lib/api/client/app/organizations/getOrganizations";
import { currentPortal, portalWorkspaces } from "@/lib/portal";

// Hot in the bootstrap chain (OrgGate, OrgSwitcher). The whole-list
// payload is small but the request still adds ~100ms of latency on
// every navigation under the global default. Keep it fresh for a
// minute and don't refetch on mount — switching workspaces and
// inviting members already invalidate explicitly.
export const organizationsQuery = queryOptions({
    queryKey: ["organizations", "list"],
    // On an agency's portal domain only that agency's clients are reachable.
    queryFn: async () => portalWorkspaces(await getOrganizations(), currentPortal()),
    staleTime: 60_000,
    refetchOnMount: false,
});

export default function useOrganizations() {
    return useQuery(organizationsQuery);
}
