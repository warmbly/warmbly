// The order a checkout was about to place when it left for card setup. On a successful
// return the same quote is placed at the same total, so nobody has to press Pay twice;
// a total that moved meanwhile is shown and waits, as at any other checkout.
import type { CreateOrderRequest } from "@/lib/api/models/app/store/Store";

const KEY = "warmbly.store.resume";
/** A pending resume older than this is a stale tab, not a card setup in progress. */
const MAX_AGE_MS = 60 * 60 * 1000;

interface Pending {
    body: CreateOrderRequest;
    at: number;
    ready: boolean;
}

function read(storage: Storage | undefined): Pending | null {
    try {
        const raw = storage?.getItem(KEY);
        if (!raw) return null;
        const p = JSON.parse(raw) as Pending;
        if (!p?.body?.quote_id || typeof p.at !== "number") return null;
        return p;
    } catch {
        return null;
    }
}

function write(storage: Storage | undefined, p: Pending | null) {
    try {
        if (p) storage?.setItem(KEY, JSON.stringify(p));
        else storage?.removeItem(KEY);
    } catch {
        // Private windows and blocked storage: the buyer presses Pay again.
    }
}

const session = (): Storage | undefined => (typeof window === "undefined" ? undefined : window.sessionStorage);

/** Remembers the order before leaving for card setup. */
export function saveResume(body: CreateOrderRequest, now = Date.now(), storage = session()) {
    write(storage, { body, at: now, ready: false });
}

/** Back from card setup: a saved card readies the order, a closed setup drops it. */
export function settleResume(saved: boolean, storage = session()) {
    const p = read(storage);
    write(storage, p && saved ? { ...p, ready: true } : null);
}

/** The order to place now, once; null when there is none or it went stale. */
export function takeResume(now = Date.now(), storage = session()): CreateOrderRequest | null {
    const p = read(storage);
    write(storage, null);
    if (!p || !p.ready || now - p.at > MAX_AGE_MS) return null;
    return p.body;
}

/** The order's Idempotency-Key: one per quote, so a reload during the call replays it. */
export function resumeKey(body: CreateOrderRequest): string {
    return `store-resume-${body.quote_id}`;
}
