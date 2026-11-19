// Money, labels and tones shared by the Store section.

import type { Tone } from "@/lib/tones";
import type { AdminStoreOrder } from "@/lib/api/client/admin/store";

export const MICROS_PER_USD = 1_000_000;

// Micro-USD as dollars; sub-cent wholesale prices keep their precision.
export function usd(micros: number | null | undefined): string {
    if (micros == null) return "—";
    const cents = micros % 10_000 === 0;
    return (micros / MICROS_PER_USD).toLocaleString("en-US", {
        style: "currency",
        currency: "USD",
        minimumFractionDigits: 2,
        maximumFractionDigits: cents ? 2 : 4,
    });
}

// A typed dollar amount as micros: null when empty, NaN when it is not a positive amount with at most cents.
export function dollarsToMicros(raw: string): number | null {
    const text = raw.replace(/[,\s$]/g, "");
    if (text === "") return null;
    if (!/^\d+(\.\d{1,2})?$/.test(text)) return NaN;
    return Math.round(Number(text) * 100) * 10_000;
}

export function refundableMicros(o: Pick<AdminStoreOrder, "charged_micros" | "refunded_micros">): number {
    return Math.max(0, o.charged_micros - o.refunded_micros);
}

// randomUUID only exists on a secure origin; a plain-http panel still needs a key.
export function newIdempotencyKey(): string {
    if (typeof crypto.randomUUID === "function") return crypto.randomUUID();
    return Array.from(crypto.getRandomValues(new Uint8Array(16)), (b) => b.toString(16).padStart(2, "0")).join("");
}

export const label = (s: string) => (s ? (s.charAt(0).toUpperCase() + s.slice(1)).replace(/_/g, " ") : s);

export const PROVIDER_LABEL: Record<string, string> = {
    google: "Google",
    gmail: "Google",
    microsoft: "Microsoft",
    outlook: "Microsoft",
};
export const providerLabel = (p: string) => PROVIDER_LABEL[p] ?? (p ? label(p) : "—");

export const ORDER_STATUSES = ["open", "charging", "processing", "completed", "partially_failed", "failed"] as const;
export const DOMAIN_STATUSES = [
    "registering",
    "connecting",
    "active",
    "expired",
    "redemption",
    "transferring_out",
    "transferred_out",
    "removed",
    "failed",
] as const;
export const MAILBOX_STATUSES = ["provisioning", "active", "suspended", "cancelling", "cancelled", "failed"] as const;
export const CONNECT_STATES = ["waiting", "connecting", "connected", "failed"] as const;

const STATUS_TONE: Record<string, Tone> = {
    charging: "info",
    processing: "info",
    completed: "success",
    partially_failed: "warning",
    failed: "danger",
    registering: "info",
    connecting: "info",
    provisioning: "info",
    active: "success",
    connected: "success",
    expired: "warning",
    redemption: "danger",
    transferring_out: "info",
    transferred_out: "neutral",
    removed: "neutral",
    suspended: "warning",
    cancelling: "warning",
    cancelled: "neutral",
    waiting: "neutral",
    done: "success",
    running: "info",
    pending: "neutral",
    skipped: "neutral",
};
export const statusTone = (s: string): Tone => STATUS_TONE[s] ?? "neutral";

export const MODE_TONE: Record<string, Tone> = {
    live: "success",
    sandbox: "info",
    simulator: "warning",
    off: "neutral",
};

// The store view a filtered link from Health lands on.
export type StoreTab = "health" | "orders" | "domains" | "mailboxes" | "economics";
export function storeLink(tab: StoreTab, filters: Record<string, string> = {}): string {
    const p = new URLSearchParams({ tab, ...filters });
    return `/store?${p.toString()}`;
}
