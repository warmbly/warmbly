import { useInfiniteQuery } from "@tanstack/react-query";
import listAgentSessions from "@/lib/api/client/app/agent/listAgentSessions";

// The member's AI assistant sessions, paged by opaque cursor. Refreshed by the
// ai_session spine entry on create.
export default function useAgentSessions(limit = 20) {
    return useInfiniteQuery({
        queryKey: ["ai", "sessions", limit],
        queryFn: ({ pageParam }) =>
            listAgentSessions(limit, pageParam as string | undefined),
        initialPageParam: undefined as string | undefined,
        getNextPageParam: (last) => last.pagination.next_cursor ?? undefined,
        staleTime: 30_000,
    });
}
