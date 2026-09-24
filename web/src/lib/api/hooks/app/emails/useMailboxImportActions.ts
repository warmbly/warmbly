import { useMutation, useQueryClient } from "@tanstack/react-query";
import previewMailboxImport from "@/lib/api/client/app/emails/imports/previewMailboxImport";
import createMailboxImport from "@/lib/api/client/app/emails/imports/createMailboxImport";
import fixMailboxImportRow from "@/lib/api/client/app/emails/imports/fixMailboxImportRow";
import retryMailboxImport from "@/lib/api/client/app/emails/imports/retryMailboxImport";
import cancelMailboxImport from "@/lib/api/client/app/emails/imports/cancelMailboxImport";
import dismissMailboxImport from "@/lib/api/client/app/emails/imports/dismissMailboxImport";
import type {
    MailboxImport,
    MailboxImportInput,
    RetryImportRequest,
    RowFix,
} from "@/lib/api/models/app/emails/MailboxImport";
import { sourceMutationKey } from "./mailboxSourceBusy";

export function usePreviewMailboxImport() {
    return useMutation({
        mutationFn: (input: MailboxImportInput) => previewMailboxImport(input),
    });
}

export function useCreateMailboxImport() {
    const qc = useQueryClient();
    return useMutation({
        mutationKey: sourceMutationKey("import-create"),
        mutationFn: (input: MailboxImportInput) => createMailboxImport(input),
        onSuccess: (job) => {
            qc.setQueryData<MailboxImport>(["emails", "imports", job.id], job);
            qc.invalidateQueries({ queryKey: ["emails", "imports"] });
        },
    });
}

// Writes the job back into its cache entry and refreshes its rows and the list.
function useJobMutation<V>(id: string | null, fn: (id: string, v: V) => Promise<MailboxImport>) {
    const qc = useQueryClient();
    return useMutation({
        mutationFn: (v: V) => fn(id!, v),
        onSuccess: (job) => {
            qc.setQueryData<MailboxImport>(["emails", "imports", job.id], job);
            qc.invalidateQueries({ queryKey: ["emails", "imports"] });
        },
    });
}

export function useRetryMailboxImport(id: string | null) {
    return useJobMutation<RetryImportRequest>(id, (i, body) => retryMailboxImport(i, body));
}

export function useCancelMailboxImport(id: string | null) {
    return useJobMutation<void>(id, (i) => cancelMailboxImport(i));
}

export function useDismissMailboxImport() {
    const qc = useQueryClient();
    return useMutation({
        mutationFn: (id: string) => dismissMailboxImport(id),
        onSuccess: () => qc.invalidateQueries({ queryKey: ["emails", "imports"] }),
    });
}

export function useFixMailboxImportRow(id: string | null) {
    const qc = useQueryClient();
    return useMutation({
        mutationFn: ({ line, fix }: { line: number; fix: RowFix }) => fixMailboxImportRow(id!, line, fix),
        onSuccess: () => {
            qc.invalidateQueries({ queryKey: ["emails", "imports", id] });
        },
    });
}
