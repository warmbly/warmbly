import { useQuery } from "@tanstack/react-query";
import getCredits from "@/lib/api/client/app/subscription/getCredits";

// The org's AI credit ledger. Changes on consumption, purchase, and the
// monthly reset; the realtime spine (credit_purchase / credit_grant / the
// per-generation consume) invalidates ['subscription','credits'], so this is
// lightly stale rather than polled.
export default function useCredits() {
    return useQuery({
        queryKey: ["subscription", "credits"],
        queryFn: () => getCredits(),
        staleTime: 30_000,
    });
}
