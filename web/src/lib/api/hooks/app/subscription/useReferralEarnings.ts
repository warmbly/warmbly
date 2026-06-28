import { useInfiniteQuery, type InfiniteData } from "@tanstack/react-query";
import type { ReferralEarningsPage } from "@/lib/api/models/app/subscription/Referral";
import listReferralEarnings from "@/lib/api/client/app/referral/listReferralEarnings";

// Infinite scroll over the reward/clawback ledger, paginated by the same opaque
// next_cursor every other list uses. Pages flatten into one list.
export default function useReferralEarnings(limit = 20, enabled = true) {
    const queryResult = useInfiniteQuery<
        ReferralEarningsPage,
        Error,
        InfiniteData<ReferralEarningsPage, string | undefined>,
        [string, string, string, number],
        string | undefined
    >({
        queryKey: ["subscription", "referral", "earnings", limit],
        queryFn: async ({ pageParam }) => listReferralEarnings(pageParam, limit),
        initialPageParam: undefined,
        getNextPageParam: (lastPage) =>
            lastPage.pagination.has_more ? (lastPage.pagination.next_cursor ?? undefined) : undefined,
        staleTime: 30_000,
        enabled,
    });

    const earnings = queryResult.data?.pages.flatMap((p) => p.data ?? []) ?? [];

    return { ...queryResult, earnings };
}
