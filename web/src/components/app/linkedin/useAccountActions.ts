// Pause, resume, resync and remove for one LinkedIn account, shared by the row menu, the drawer and the selection bar.
import { useConfirm } from "@/hooks/context/confirm";
import { useLinkedInAccountAction, useRemoveLinkedInAccount } from "@/lib/api/hooks/app/linkedin/useLinkedIn";
import type { LinkedInAccount } from "@/lib/api/models/app/linkedin/LinkedIn";
import { linkedInErrorMessage } from "@/lib/linkedin/errors";
import toast from "react-hot-toast/headless";
import { fmtDay } from "./parts";

export interface AccountActions {
    pause: (a: LinkedInAccount) => void;
    resume: (a: LinkedInAccount) => void;
    resync: (a: LinkedInAccount) => void;
    remove: (a: LinkedInAccount, after?: () => void) => void;
    pending: boolean;
}

export const canPause = (a: LinkedInAccount) => a.status === "active" || a.status === "connecting";
export const canResume = (a: LinkedInAccount) => a.status === "paused";
export const needsReconnect = (a: LinkedInAccount) => a.status === "reconnect_required" || a.status === "disconnected" || a.status === "restricted";

export default function useAccountActions(): AccountActions {
    const confirm = useConfirm();
    const action = useLinkedInAccountAction();
    const remove = useRemoveLinkedInAccount();
    return {
        pause: (a) => action.mutate({ id: a.id, action: "pause" }),
        resume: (a) => action.mutate({ id: a.id, action: "resume" }),
        resync: (a) => action.mutate({ id: a.id, action: "resync" }),
        remove: (a, after) => {
            const store = a.billing?.mode === "store";
            const kept = store && a.billing.paid_through ? ` The seat stays paid through ${fmtDay(a.billing.paid_through)} and is not renewed.` : "";
            confirm.show(
                `Remove ${a.display_name || "this account"}? Warmbly stops every LinkedIn action it runs and disconnects it.${kept}`,
                async () => {
                    try {
                        await remove.mutateAsync(a.id);
                        toast.success("LinkedIn account removed.");
                        after?.();
                    } catch (e) {
                        toast.error(linkedInErrorMessage(e));
                    }
                },
                undefined,
                { title: "Remove LinkedIn account", confirmLabel: "Remove", tone: "danger" },
            );
        },
        pending: action.isPending || remove.isPending,
    };
}
