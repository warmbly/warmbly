import { useQuery } from "@tanstack/react-query";
import getSubscriptionLimits from "@/lib/api/client/app/subscription/getSubscriptionLimits";

export default function useSubscriptionLimits() {
    return useQuery({
        queryKey: ["subscription", "limits"],
        queryFn: () => getSubscriptionLimits(),
    })
}
