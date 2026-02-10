import { useQuery } from "@tanstack/react-query";
import getOrganizations from "@/lib/api/client/app/organizations/getOrganizations";

export default function useOrganizations() {
    return useQuery({
        queryKey: ["organizations", "list"],
        queryFn: () => getOrganizations(),
    })
}
