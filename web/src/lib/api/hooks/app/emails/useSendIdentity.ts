import { useQuery } from "@tanstack/react-query";
import getSendIdentity from "@/lib/api/client/app/emails/getSendIdentity";

// Sending identity for the mailbox drawer. Stored state, so it is cheap and
// stays fresh through the refresh mutation rather than polling.
export default function useSendIdentity(id: string, enabled = true) {
    return useQuery({
        queryKey: ["emails", id, "identity"],
        queryFn: () => getSendIdentity(id),
        enabled: !!id && enabled,
        staleTime: 60_000,
    });
}
