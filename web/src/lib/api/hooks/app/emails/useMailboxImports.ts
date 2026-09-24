import { useQuery } from "@tanstack/react-query";
import listMailboxImports from "@/lib/api/client/app/emails/imports/listMailboxImports";

export const MAILBOX_IMPORTS_KEY = ["emails", "imports"] as const;

// Recent imports for the mailboxes page; MAILBOX_IMPORT_PROGRESS keeps it live,
// and a running import is also re-read every few seconds in case the socket is down.
export default function useMailboxImports(enabled = true, limit = 10) {
    return useQuery({
        queryKey: [...MAILBOX_IMPORTS_KEY, "list", limit],
        queryFn: () => listMailboxImports({ limit }),
        enabled,
        staleTime: 15_000,
        refetchInterval: (q) => (q.state.data?.data?.some((j) => j.status === "running") ? 5_000 : false),
    });
}
