import { useQuery } from "@tanstack/react-query";
import getWarmupBanStatus from "@/lib/api/client/app/emails/getWarmupBanStatus";

// Live warmup-ban status for a mailbox. Keyed under the
// ["analytics", "accounts", id] prefix so the existing WARMUP/ACCOUNT realtime
// branch in useRealtimeEvents (which invalidates ["analytics", "accounts"])
// also refreshes this without any extra wiring.
export default function useWarmupBanStatus(id: string, enabled = true) {
    return useQuery({
        queryKey: ["analytics", "accounts", id, "warmup-ban"],
        queryFn: () => getWarmupBanStatus(id),
        enabled: !!id && enabled,
    });
}
