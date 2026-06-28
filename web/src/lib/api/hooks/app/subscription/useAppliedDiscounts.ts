import { useQuery } from "@tanstack/react-query";
import getAppliedDiscounts from "@/lib/api/client/app/subscription/getAppliedDiscounts";

// The org's promo redemption history. Changes only when a code is redeemed, so
// we mark it lightly stale and let the realtime subscription spine refresh it.
export default function useAppliedDiscounts(limit = 20) {
    return useQuery({
        queryKey: ["subscription", "discounts", limit],
        queryFn: () => getAppliedDiscounts(limit),
        staleTime: 30_000,
    });
}
