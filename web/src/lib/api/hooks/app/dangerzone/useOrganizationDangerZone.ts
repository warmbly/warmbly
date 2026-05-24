import { useQuery } from "@tanstack/react-query";
import getOrganizationDangerZone from "@/lib/api/client/app/dangerzone/getOrganizationDangerZone";

export default function useOrganizationDangerZone() {
    return useQuery({
        queryKey: ["dangerzone", "organization"],
        queryFn: () => getOrganizationDangerZone(),
        staleTime: 30_000,
    });
}
