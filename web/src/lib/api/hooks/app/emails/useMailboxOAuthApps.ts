import { queryOptions, useMutation, useQuery, useQueryClient } from "@tanstack/react-query";

import {
    deleteMailboxOAuthApp,
    listMailboxOAuthApps,
    saveMailboxOAuthApp,
} from "@/lib/api/client/app/emails/mailboxOAuthApps";
import type {
    MailboxOAuthAppInput,
    MailboxOAuthAppsResult,
    MailboxOAuthProvider,
} from "@/lib/api/models/app/emails/MailboxOAuthApp";

export const mailboxOAuthAppsQuery = queryOptions({
    queryKey: ["mailbox-oauth-apps"],
    queryFn: () => listMailboxOAuthApps(),
    staleTime: 10_000,
});

export function useMailboxOAuthApps(enabled = true) {
    return useQuery({ ...mailboxOAuthAppsQuery, enabled });
}

export function useSaveMailboxOAuthApp() {
    const qc = useQueryClient();
    return useMutation({
        mutationFn: ({ provider, data }: { provider: MailboxOAuthProvider; data: MailboxOAuthAppInput }) =>
            saveMailboxOAuthApp(provider, data),
        onSuccess: (app) => {
            qc.setQueryData<MailboxOAuthAppsResult>(mailboxOAuthAppsQuery.queryKey, (prev) => ({
                callbacks: prev?.callbacks ?? {},
                data: [...(prev?.data ?? []).filter((a) => a.provider !== app.provider), app],
            }));
        },
        onSettled: () => void qc.invalidateQueries({ queryKey: ["mailbox-oauth-apps"] }),
    });
}

// Removing an app is predictable: drop it at once, put it back if the server refuses.
export function useDeleteMailboxOAuthApp() {
    const qc = useQueryClient();
    return useMutation({
        mutationFn: (provider: MailboxOAuthProvider) => deleteMailboxOAuthApp(provider),
        onMutate: async (provider) => {
            await qc.cancelQueries({ queryKey: ["mailbox-oauth-apps"] });
            const prev = qc.getQueryData<MailboxOAuthAppsResult>(mailboxOAuthAppsQuery.queryKey);
            qc.setQueryData<MailboxOAuthAppsResult>(mailboxOAuthAppsQuery.queryKey, (cur) => ({
                callbacks: cur?.callbacks ?? {},
                data: (cur?.data ?? []).filter((a) => a.provider !== provider),
            }));
            return { prev };
        },
        onError: (_err, _provider, ctx) => {
            if (ctx?.prev) qc.setQueryData(mailboxOAuthAppsQuery.queryKey, ctx.prev);
        },
        onSettled: () => void qc.invalidateQueries({ queryKey: ["mailbox-oauth-apps"] }),
    });
}
