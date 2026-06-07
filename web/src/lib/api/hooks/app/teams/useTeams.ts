import { useQuery } from "@tanstack/react-query";
import listTeams from "@/lib/api/client/app/teams/listTeams";

export default function useTeams() {
    return useQuery({
        queryKey: ["teams"],
        queryFn: () => listTeams(),
        staleTime: 5 * 60 * 1000,
    });
}
