// One operator write on an agency: every endpoint answers with the agency as it now stands.

import { useMutation, useQueryClient } from "@tanstack/react-query";
import { toast } from "sonner";
import { ReauthCancelledError } from "@/lib/api/client";
import type { AdminAgencyDetail } from "@/lib/api/client/admin/agencies";

export function useAgencyWrite<V = void>(
    agencyId: string,
    write: (vars: V) => Promise<AdminAgencyDetail>,
    // quiet: the caller shows the failure itself, next to the form that caused it.
    text: { done: string; failed: string; quiet?: boolean },
    onDone?: (detail: AdminAgencyDetail, vars: V) => void,
) {
    const qc = useQueryClient();
    return useMutation({
        mutationFn: write,
        onSuccess: (detail, vars) => {
            qc.setQueryData(["admin", "agencies", agencyId], detail);
            // The lists show the plan and pool too; the detail was just set.
            void qc.invalidateQueries({ queryKey: ["admin", "agencies"], predicate: (q) => q.queryKey[2] !== agencyId });
            toast.success(text.done);
            onDone?.(detail, vars);
        },
        onError: (e: Error) => {
            // Closing the confirm-it-is-you prompt is a choice, not a failure.
            if (text.quiet || e instanceof ReauthCancelledError) return;
            toast.error(e.message || text.failed);
        },
    });
}
