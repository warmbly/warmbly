import type { Tone } from "@/lib/tones";
import type { AdminAgency } from "@/lib/api/client/admin/agencies";
import type { AdminPlan } from "@/lib/api/models/admin";

export const AGENCY_STATUS_TONE: Record<string, Tone> = {
    active: "success",
    trialing: "info",
    past_due: "warning",
    canceled: "neutral",
    unpaid: "danger",
    incomplete: "neutral",
    incomplete_expired: "neutral",
    paused: "warning",
};

export function poolLabel(a: Pick<AdminAgency, "pool_used" | "pool_total" | "unlimited">): string {
    if (a.unlimited) return `${a.pool_used.toLocaleString()} / no cap`;
    if (a.pool_total <= 0) return "No plan";
    const pct = Math.round((a.pool_used / a.pool_total) * 100);
    return `${a.pool_used.toLocaleString()} / ${a.pool_total.toLocaleString()} (${pct}%)`;
}

export const usd = (n: number) => n.toLocaleString("en-US", { style: "currency", currency: "USD" });

// An agency plan is one with a shared send pool; zero is a pool with no cap.
export function isAgencyPlan(p: Pick<AdminPlan, "monthly_send_pool">): boolean {
    return p.monthly_send_pool != null;
}

// A Stripe subscription that still entitles the agency, the same statuses the backend honours.
export function hasLiveSubscription(a: Pick<AdminAgency, "plan_id" | "status">): boolean {
    return !!a.plan_id && (a.status === "active" || a.status === "trialing" || a.status === "past_due");
}

// Reads an amount field: null when left empty, NaN when it is not a number.
export function parseAmount(raw: string): number | null {
    const text = raw.replace(/[,\s$]/g, "");
    if (text === "") return null;
    return /^\d+(\.\d+)?$/.test(text) ? Number(text) : NaN;
}

// randomUUID only exists on a secure origin; a plain-http panel still needs a key.
export function newIdempotencyKey(): string {
    if (typeof crypto.randomUUID === "function") return crypto.randomUUID();
    return Array.from(crypto.getRandomValues(new Uint8Array(16)), (b) => b.toString(16).padStart(2, "0")).join("");
}
