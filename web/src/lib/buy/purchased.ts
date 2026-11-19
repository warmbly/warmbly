// Where a mailbox bought in the store stands, for the screens that remove or reconnect mailboxes.
import type Inbox from "@/lib/api/models/app/emails/Inbox";

/** Why a live purchased mailbox has no disconnect: removing it would not stop its billing. */
export const PURCHASED_LOCKED =
    "This mailbox was bought through Warmbly and stays billed while you have it. Cancel it under Subscription to stop it at the end of the paid month, or switch it off to pause it.";

export type PurchaseState = { kind: "none" } | { kind: "live" } | { kind: "cancelled"; on: string | null };

/** A mailbox not bought in the store, one still paid for, or one whose subscription ended. */
export function purchaseState(box: Pick<Inbox, "purchased" | "purchase_cancelled_at">): PurchaseState {
    if (!box.purchased) return { kind: "none" };
    if (!box.purchase_cancelled_at) return { kind: "live" };
    return { kind: "cancelled", on: shortDate(box.purchase_cancelled_at) };
}

/** True for a store mailbox the provider no longer keeps. */
export function storeMailboxEnded(status: string): boolean {
    return status === "cancelled" || status === "failed";
}

export function shortDate(v: Date | string | null | undefined): string | null {
    if (!v) return null;
    const d = v instanceof Date ? v : new Date(v);
    return Number.isNaN(d.getTime()) ? null : d.toLocaleDateString(undefined, { month: "short", day: "numeric", year: "numeric" });
}
