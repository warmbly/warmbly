// One path for every Store operator action: confirm, call, toast the server's
// own message on refusal, and refresh every store view.

import { useState } from "react";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { toast } from "sonner";
import { useConfirm, type ConfirmOptions } from "@/components/ConfirmDialog";
import { ReauthCancelledError } from "@/lib/api/client";
import { useAdminPerm } from "@/hooks/useAdminPerm";
import { AdminPerm } from "@/lib/auth/permissions";

export interface StoreActionJob<T> {
    id: string;
    confirm: ConfirmOptions;
    success: string;
    run: () => Promise<T>;
}

export function storeErrorText(e: unknown): string {
    return e instanceof Error && e.message ? e.message : "The action failed";
}

export function useStoreAction() {
    const confirm = useConfirm();
    const qc = useQueryClient();
    const canManage = useAdminPerm(AdminPerm.ManageOrganizations);
    const [busy, setBusy] = useState<string | null>(null);
    const mutation = useMutation({ mutationFn: (run: () => Promise<unknown>) => run() });

    async function act<T>(job: StoreActionJob<T>): Promise<T | undefined> {
        if (!(await confirm(job.confirm))) return undefined;
        setBusy(job.id);
        try {
            const out = (await mutation.mutateAsync(job.run)) as T;
            toast.success(job.success);
            await qc.invalidateQueries({ queryKey: ["admin", "store"] });
            return out;
        } catch (e) {
            if (!(e instanceof ReauthCancelledError)) toast.error(storeErrorText(e));
            return undefined;
        } finally {
            setBusy(null);
        }
    }

    return { act, busy, canManage };
}
