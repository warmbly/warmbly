import { useQuery } from "@tanstack/react-query";
import getOrganizationDangerZone from "@/lib/api/client/app/dangerzone/getOrganizationDangerZone";
import { useCurrentOrg } from "@/stores/useAppStore";

// Gated on having an org selected — otherwise the endpoint 400s and
// react-query would stamp the cache with a useless error that the
// global banner has to special-case around.
export default function useOrganizationDangerZone() {
    const org = useCurrentOrg();
    return useQuery({
        queryKey: ["dangerzone", "organization", org?.id ?? null],
        queryFn: () => getOrganizationDangerZone(),
        staleTime: 30_000,
        enabled: !!org,
    });
}
