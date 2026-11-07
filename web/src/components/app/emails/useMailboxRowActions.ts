import { useMemo } from "react";
import { useMutationState, useQueryClient } from "@tanstack/react-query";
import toast from "react-hot-toast/headless";
import { useConfirm } from "@/hooks/context/confirm";
import { switchOffPrompt } from "@/components/app/emails/useMailboxSwitch";
import runMutation from "@/lib/api/hooks/runMutation";
import { warmupLifecycleOptions } from "@/lib/api/hooks/app/emails/useWarmupLifecycle";
import { removeEmailOptions } from "@/lib/api/hooks/app/emails/useRemoveEmail";
import { updateEmailOptions } from "@/lib/api/hooks/app/emails/useUpdateEmail";
import { useEnrollCloudLinkMailbox, useUnenrollCloudLinkMailbox, useCloudLinkMailboxLifecycle } from "@/lib/api/hooks/app/cloudlink/useCloudLink";
import useCurrentOrganization from "@/lib/api/hooks/app/organizations/useCurrentOrganization";
import type { WarmupAction } from "@/lib/api/client/app/emails/warmupLifecycle";
import type { AppError } from "@/lib/api/client/normalizeError";
import buildError from "@/lib/helper/buildError";

// The mailbox list's row actions, held once by the page instead of a set of
// mutation hooks per row. Each write runs under the same per-mailbox key the
// single-mailbox hooks use, so overlapping writes still settle in order.
export default function useMailboxRowActions() {
    const queryClient = useQueryClient();
    const confirm = useConfirm();
    const workspaceZone = useCurrentOrganization().data?.timezone;
    const { mutateAsync: enroll } = useEnrollCloudLinkMailbox();
    const { mutateAsync: unenroll } = useUnenrollCloudLinkMailbox();
    const { mutateAsync: setCloudLifecycle } = useCloudLinkMailboxLifecycle();

    // Mailboxes with a warmup change in flight, for the row's menu trigger.
    const warmupKeys = useMutationState({
        filters: { status: "pending", predicate: (m) => m.options.mutationKey?.[0] === "emails" && m.options.mutationKey?.[2] === "warmup" },
        select: (m) => String(m.options.mutationKey?.[1]),
    });
    const warmupPending = useMemo(() => new Set(warmupKeys), [warmupKeys]);

    return useMemo(() => {
        const setPower = async (id: string, email: string, on: boolean) => {
            try {
                await runMutation(queryClient, updateEmailOptions(queryClient, id), { status: on ? "active" : "inactive" });
                void queryClient.invalidateQueries({ queryKey: ["analytics", "accounts"] });
                toast.success(on ? `${email} is back on` : `${email} switched off`);
            } catch (e) {
                toast.error(buildError(e as AppError));
            }
        };
        return {
            confirm,
            workspaceZone,
            warmupPending,
            warmup: (id: string, action: WarmupAction) => runMutation(queryClient, warmupLifecycleOptions(queryClient, id), action),
            remove: (id: string) => runMutation(queryClient, removeEmailOptions(queryClient, id), undefined),
            switchOn: (id: string, email: string) => void setPower(id, email, true),
            switchOff: (id: string, email: string) => confirm.show(switchOffPrompt(email), () => setPower(id, email, false)),
            cloudEnroll: (id: string) => enroll(id),
            cloudUnenroll: (id: string) => unenroll(id),
            cloudLifecycle: (id: string, action: "pause" | "resume") => setCloudLifecycle({ id, action }),
        };
    }, [queryClient, confirm, workspaceZone, warmupPending, enroll, unenroll, setCloudLifecycle]);
}

export type MailboxRowActions = ReturnType<typeof useMailboxRowActions>;
