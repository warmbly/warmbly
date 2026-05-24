import { useQuery } from "@tanstack/react-query";
import type Access from "@/lib/api/models/app/admin/Access";

const EMPTY_ACCESS: Access = { roles: [], permissions: [] };

export default function useRoles() {
    return useQuery({
        queryKey: ["admin", "roles"],
        queryFn: async () => EMPTY_ACCESS,
        staleTime: Infinity,
    });
}
