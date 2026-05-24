import { useQuery } from "@tanstack/react-query";
import listAPIKeys from "@/lib/api/client/app/api-keys/listAPIKeys";

export default function useAPIKeys() {
    return useQuery({
        queryKey: ["api-keys", "list"],
        queryFn: () => listAPIKeys(),
    })
}
