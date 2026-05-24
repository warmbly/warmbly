import { useQuery } from "@tanstack/react-query";
import getSubscription from "@/lib/api/client/app/subscription/getSubscription";

// Subscription state changes only when the user upgrades, downgrades,
// cancels, or Stripe sends us a webhook. Refetching on navigation
// gives nothing back so we mark it as never-stale and invalidate
// explicitly from useChangePlan / useCancelSubscription / the realtime
// channel. This kills the "header reloads on every page change"
// flicker.
export default function useSubscription() {
    return useQuery({
        queryKey: ["subscription"],
        queryFn: () => getSubscription(),
        staleTime: Infinity,
        gcTime: Infinity,
        refetchOnWindowFocus: false,
        refetchOnMount: false,
        refetchOnReconnect: false,
    });
}
