// The store row behind a connected mailbox, and the cancel-at-period-end prompt the drawer's cards share.
import toast from "react-hot-toast/headless";
import { useConfirm } from "@/hooks/context/confirm";
import { storeDisconnected, storeListable, useCancelStoreMailbox, useStoreMailboxes, useStoreState } from "@/lib/api/hooks/app/store/useStore";
import type { StoreMailbox, StoreStatus } from "@/lib/api/models/app/store/Store";
import { storeErrorMessage } from "@/lib/buy/errors";
import { shortDate as day } from "@/lib/buy/purchased";

/** The store row behind a connected mailbox, once the store list has it; listed too while GraphKit is disconnected. */
export function usePurchasedMailbox(emailAccountId: string): StoreMailbox | undefined {
    const list = useStoreMailboxes(storeListable(useStoreState()));
    return list.data?.data.find((x) => x.email_account_id === emailAccountId);
}

/** The store status while GraphKit is disconnected and the workspace still holds live purchases, else null; nothing about them can be changed then. */
export function useStorePausedStatus(): StoreStatus | null {
    const store = useStoreState();
    return store.kind === "locked" && storeDisconnected(store) ? store.status : null;
}

/** Asks before cancelling at the end of the paid month; the drawer's Subscription card and Danger zone share it. */
export function useCancelAtPeriodEnd() {
    const confirm = useConfirm();
    const cancel = useCancelStoreMailbox();
    const ask = (m: StoreMailbox) => {
        const paid = day(m.paid_through);
        confirm.show(
            `Cancel ${m.email}? It keeps sending and warming until ${paid ?? "the end of its paid month"}, then the mailbox is deleted with its mail, stops being billed and is switched off here. Its history stays in Warmbly until you disconnect it. You can undo this until then.`,
            async () => {
                try {
                    const next = await cancel.mutateAsync(m.id);
                    toast.success(`${m.email} ends ${day(next.cancels_at) ?? "at the end of its month"}`);
                } catch (e) {
                    toast.error(storeErrorMessage(e));
                }
            },
            undefined,
            { title: "Cancel mailbox", confirmLabel: "Cancel at period end" },
        );
    };
    return { ask, pending: cancel.isPending };
}
