import { useInfiniteQuery } from "@tanstack/react-query";
import getCreditTransactions from "@/lib/api/client/app/subscription/getCreditTransactions";

// The credit transaction log, paged by opaque cursor. Infinite so "Load more"
// appends pages and a realtime spine invalidation (['subscription','credits'])
// refetches every loaded page cleanly instead of hand-accumulating rows.
export default function useCreditTransactions(limit = 25) {
    return useInfiniteQuery({
        queryKey: ["subscription", "credits", "transactions", limit],
        queryFn: ({ pageParam }) =>
            getCreditTransactions(limit, pageParam as string | undefined),
        initialPageParam: undefined as string | undefined,
        getNextPageParam: (last) => last.pagination.next_cursor ?? undefined,
        staleTime: 30_000,
    });
}
