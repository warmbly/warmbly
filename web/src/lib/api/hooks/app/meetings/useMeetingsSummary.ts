import { useQuery } from "@tanstack/react-query";
import meetingsSummary from "@/lib/api/client/app/meetings/meetingsSummary";

// Powers the Meetings page header stats and the sidebar upcoming count. Kept
// fresh by realtime meeting events (see useRealtimeEvents) rather than polling.
export default function useMeetingsSummary(enabled = true) {
    return useQuery({
        queryKey: ["meetings", "summary"],
        queryFn: meetingsSummary,
        staleTime: 30_000,
        enabled,
    });
}
