import { useQuery } from "@tanstack/react-query";
import getOrganizations from "@/lib/api/client/app/organizations/getOrganizations";

// Hot in the bootstrap chain (OrgGate, OrgSwitcher). The whole-list
// payload is small but the request still adds ~100ms of latency on
// every navigation under the global default. Keep it fresh for a
// minute and don't refetch on mount — switching workspaces and
// inviting members already invalidate explicitly.
export default function useOrganizations() {
    return useQuery({
        queryKey: ["organizations", "list"],
        queryFn: () => getOrganizations(),
        staleTime: 60_000,
        refetchOnMount: false,
    });
}
