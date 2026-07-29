import { keepPreviousData, useQuery } from "@tanstack/react-query";
import composeCandidates from "@/lib/api/client/app/unibox/composeCandidates";

// Candidate senders for the compose picker, keyed by the typed recipient.
// keepPreviousData holds the last result while the address changes so the
// picker doesn't flash empty between keystrokes.
export default function useComposeCandidates(address: string, enabled = true) {
    return useQuery({
        queryKey: ["unibox", "compose", "candidates", address],
        queryFn: () => composeCandidates(address),
        staleTime: 15_000,
        placeholderData: keepPreviousData,
        enabled,
    });
}
