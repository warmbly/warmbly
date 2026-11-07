// The workspace balance is too low for what was just asked. Any surface that
// meets it calls openInsufficientBalance (the API client already does for every
// REST call), and InsufficientBalanceDialog offers to add money in place.

import type { AppError } from "@/lib/api/client/normalizeError";

const EVENT = "balance-insufficient";

/** A 402 that means the balance ran out, not a plan limit or a declined card. */
export function isInsufficientBalance(err: unknown): boolean {
    const e = err as AppError | null | undefined;
    if (e?.status !== 402) return false;
    return !e.code || e.code === "insufficient_credits" || e.code === "insufficient_balance";
}

export function openInsufficientBalance(): void {
    if (typeof window === "undefined") return;
    window.dispatchEvent(new CustomEvent(EVENT));
}

export function onInsufficientBalance(fn: () => void): () => void {
    window.addEventListener(EVENT, fn);
    return () => window.removeEventListener(EVENT, fn);
}
