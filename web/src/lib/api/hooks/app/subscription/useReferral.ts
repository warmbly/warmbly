import { useQuery } from "@tanstack/react-query";
import getReferral from "@/lib/api/client/app/referral/getReferral";

// The referral summary changes when a reward lands or the balance is spent.
// The realtime spine invalidates ["subscription", "referral"] on referral /
// referral_credit audit events, so we don't poll here.
export default function useReferral() {
    return useQuery({
        queryKey: ["subscription", "referral"],
        queryFn: () => getReferral(),
        staleTime: 30_000,
    });
}
