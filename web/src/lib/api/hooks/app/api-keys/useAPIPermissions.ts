import { useQuery } from "@tanstack/react-query";
import listAPIPermissions from "@/lib/api/client/app/api-keys/listAPIPermissions";

export default function useAPIPermissions() {
    return useQuery({
        queryKey: ["api-keys", "permissions"],
        queryFn: () => listAPIPermissions(),
    })
}
