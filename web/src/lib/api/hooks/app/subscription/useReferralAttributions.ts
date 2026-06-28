import { useInfiniteQuery, type InfiniteData } from "@tanstack/react-query";
import type { ReferralAttributionsPage } from "@/lib/api/models/app/subscription/Referral";
import listReferralAttributions from "@/lib/api/client/app/referral/listReferralAttributions";

// Infinite scroll over the org's referrals, paginated by the same opaque
// next_cursor every other list uses. Pages flatten into one list.
export default function useReferralAttributions(limit = 20, enabled = true) {
    const queryResult = useInfiniteQuery<
        ReferralAttributionsPage,
        Error,
        InfiniteData<ReferralAttributionsPage, string | undefined>,
        [string, string, string, number],
        string | undefined
    >({
        queryKey: ["subscription", "referral", "attributions", limit],
        queryFn: async ({ pageParam }) => listReferralAttributions(pageParam, limit),
        initialPageParam: undefined,
        getNextPageParam: (lastPage) =>
            lastPage.pagination.has_more ? (lastPage.pagination.next_cursor ?? undefined) : undefined,
        staleTime: 30_000,
        enabled,
    });

    const attributions = queryResult.data?.pages.flatMap((p) => p.data ?? []) ?? [];

    return { ...queryResult, attributions };
}
