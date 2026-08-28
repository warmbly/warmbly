import { useQuery } from "@tanstack/react-query";
import getOrganizationRisk from "@/lib/api/client/app/organizations/getOrganizationRisk";

// Keyed under ["organizations"] so the audit spine's org_risk entry refreshes
// it for every teammate the moment the posture changes.
export default function useOrganizationRisk() {
    return useQuery({
        queryKey: ["organizations", "risk"],
        queryFn: getOrganizationRisk,
        staleTime: 60_000,
    });
}
