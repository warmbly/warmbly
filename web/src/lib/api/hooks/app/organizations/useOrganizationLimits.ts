import { useQuery } from "@tanstack/react-query";
import getOrganizationLimits from "@/lib/api/client/app/organizations/getOrganizationLimits";

export default function useOrganizationLimits() {
    return useQuery({
        queryKey: ["organizations", "limits"],
        queryFn: () => getOrganizationLimits(),
    })
}
