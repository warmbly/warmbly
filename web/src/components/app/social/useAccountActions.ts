// Pause, resume, resync and remove for one social account, shared by the row menu, the drawer and the selection bar.
import { useConfirm } from "@/hooks/context/confirm";
import { useRemoveSocialAccount, useSocialAccountAction } from "@/lib/api/hooks/app/social/useSocial";
import type { SocialAccount } from "@/lib/api/models/app/social/Social";
import { channelLabel } from "@/lib/social/channels";
import { socialErrorMessage } from "@/lib/social/errors";
import toast from "react-hot-toast/headless";
import { fmtDay } from "./parts";

export interface AccountActions {
    pause: (a: SocialAccount) => void;
    resume: (a: SocialAccount) => void;
    resync: (a: SocialAccount) => void;
    remove: (a: SocialAccount, after?: () => void) => void;
    pending: boolean;
}

export const canPause = (a: SocialAccount) => a.status === "active" || a.status === "connecting";
export const canResume = (a: SocialAccount) => a.status === "paused";
export const needsReconnect = (a: SocialAccount) => a.status === "reconnect_required" || a.status === "disconnected" || a.status === "restricted";

export default function useAccountActions(): AccountActions {
    const confirm = useConfirm();
    const action = useSocialAccountAction();
    const remove = useRemoveSocialAccount();
    return {
        pause: (a) => action.mutate({ id: a.id, action: "pause" }),
        resume: (a) => action.mutate({ id: a.id, action: "resume" }),
        resync: (a) => action.mutate({ id: a.id, action: "resync" }),
        remove: (a, after) => {
            const store = a.billing?.mode === "store";
            const kept = store && a.billing.paid_through ? ` The seat stays paid through ${fmtDay(a.billing.paid_through)} and is not renewed.` : "";
            confirm.show(
                `Remove ${a.display_name || "this account"}? Warmbly stops everything it runs and disconnects it.${kept}`,
                async () => {
                    try {
                        await remove.mutateAsync(a.id);
                        toast.success(`${channelLabel(a.channel)} account removed.`);
                        after?.();
                    } catch (e) {
                        toast.error(socialErrorMessage(e));
                    }
                },
                undefined,
                { title: `Remove ${channelLabel(a.channel)} account`, confirmLabel: "Remove", tone: "danger" },
            );
        },
        pending: action.isPending || remove.isPending,
    };
}
