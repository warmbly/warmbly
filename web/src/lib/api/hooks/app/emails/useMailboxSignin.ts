// A mailbox's sign-in and its switches, under its own root: the email_account and
// mailbox_vendor audit spine refresh it, so a vendor reconnect moves on screen live.
import { useQuery } from "@tanstack/react-query";
import getEmailSignin from "@/lib/api/client/app/emails/getEmailSignin";

export const MAILBOX_SIGNIN_KEY = ["mailbox-signin"] as const;

export default function useMailboxSignin(id: string | null, enabled = true) {
    return useQuery({
        queryKey: [...MAILBOX_SIGNIN_KEY, id],
        queryFn: () => getEmailSignin(id!),
        enabled: !!id && enabled,
        staleTime: 30_000,
        retry: false,
    });
}
