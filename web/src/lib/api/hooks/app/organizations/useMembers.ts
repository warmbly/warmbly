import { useQuery } from "@tanstack/react-query";
import getMembers from "@/lib/api/client/app/organizations/getMembers";

export default function useMembers() {
    return useQuery({
        queryKey: ["organizations", "members"],
        queryFn: () => getMembers(),
    })
}
