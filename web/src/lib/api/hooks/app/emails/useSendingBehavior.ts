import { useQuery } from "@tanstack/react-query";
import getSendingBehavior from "@/lib/api/client/app/emails/getSendingBehavior";

// A mailbox's sending-behaviour profile. The backend substitutes defaults for a
// mailbox that has never been configured, so this always resolves to a complete
// object and the editor never has to render a half-empty form.
export default function useSendingBehavior(id: string, enabled = true) {
    return useQuery({
        queryKey: ["emails", id, "behavior"],
        queryFn: () => getSendingBehavior(id),
        enabled: !!id && enabled,
        staleTime: 30_000,
    });
}
