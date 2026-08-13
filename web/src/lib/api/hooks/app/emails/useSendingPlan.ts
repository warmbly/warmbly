import { useQuery } from "@tanstack/react-query";
import getSendingPlan from "@/lib/api/client/app/emails/getSendingPlan";

// The workday this mailbox rolled for today, plus how much of it is spent.
// Refetched on an interval because "remaining today" moves as sends land, and
// there is no per-send realtime event to hang it off.
export default function useSendingPlan(id: string, enabled = true) {
    return useQuery({
        queryKey: ["emails", id, "behavior", "plan"],
        queryFn: () => getSendingPlan(id),
        enabled: !!id && enabled,
        staleTime: 30_000,
        refetchInterval: enabled ? 60_000 : false,
    });
}
