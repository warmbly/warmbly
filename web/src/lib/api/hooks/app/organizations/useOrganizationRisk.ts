import { useQuery } from "@tanstack/react-query";
import getOrganizationRisk from "@/lib/api/client/app/organizations/getOrganizationRisk";
import { useAccessRestricted } from "@/hooks/usePermission";

// Keyed under ["organizations"] so the audit spine's org_risk entry refreshes
// it for every teammate the moment the posture changes.
export default function useOrganizationRisk() {
    // The workspace's posture is not part of a restricted member's scope.
    const restricted = useAccessRestricted();
    return useQuery({
        enabled: !restricted,
        queryKey: ["organizations", "risk"],
        queryFn: getOrganizationRisk,
        staleTime: 60_000,
    });
}
