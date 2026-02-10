import { useQuery } from "@tanstack/react-query";
import getSubscription from "@/lib/api/client/app/subscription/getSubscription";

export default function useSubscription() {
    return useQuery({
        queryKey: ["subscription"],
        queryFn: () => getSubscription(),
    })
}
