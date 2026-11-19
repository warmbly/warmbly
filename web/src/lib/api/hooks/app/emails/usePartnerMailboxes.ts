import { useMemo } from "react";
import { useQuery } from "@tanstack/react-query";
import getPartnerMailboxes from "@/lib/api/client/app/emails/getPartnerMailboxes";
import type PartnerMailbox from "@/lib/api/models/app/emails/PartnerMailbox";
import usePartnerManaged from "@/hooks/usePartnerManaged";

/** Under the ["emails"] prefix so a partner's change, audited on the mailbox, refreshes it live. */
export const PARTNER_MAILBOXES_KEY = ["emails", "partner"] as const;

/** Each mailbox's enablement in a workspace a partner manages, by mailbox id; empty in any other workspace. */
export default function usePartnerMailboxes(): Map<string, PartnerMailbox> {
    const managed = usePartnerManaged() !== null;
    const { data } = useQuery({
        queryKey: PARTNER_MAILBOXES_KEY,
        queryFn: getPartnerMailboxes,
        enabled: managed,
        staleTime: 15_000,
    });
    return useMemo(() => new Map((data?.data ?? []).map((m) => [m.email_account_id, m])), [data]);
}
