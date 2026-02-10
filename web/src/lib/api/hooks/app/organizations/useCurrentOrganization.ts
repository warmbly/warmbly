import { useQuery } from "@tanstack/react-query";
import getCurrentOrganization from "@/lib/api/client/app/organizations/getCurrentOrganization";

export default function useCurrentOrganization() {
    return useQuery({
        queryKey: ["organizations", "current"],
        queryFn: () => getCurrentOrganization(),
    })
}
