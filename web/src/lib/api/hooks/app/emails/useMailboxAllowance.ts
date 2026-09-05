import { useQuery } from "@tanstack/react-query";
import getMailboxAllowance from "@/lib/api/client/app/emails/getMailboxAllowance";

/** Under the ["emails"] prefix so every mailbox event refreshes it live. */
export const MAILBOX_ALLOWANCE_KEY = ["emails", "allowance"] as const;

export default function useMailboxAllowance(enabled = true) {
    return useQuery({
        queryKey: MAILBOX_ALLOWANCE_KEY,
        queryFn: getMailboxAllowance,
        enabled,
        staleTime: 15_000,
    });
}
