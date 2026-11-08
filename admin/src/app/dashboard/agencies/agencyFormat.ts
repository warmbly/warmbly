import type { Tone } from "@/lib/tones";
import type { AdminAgency } from "@/lib/api/client/admin/agencies";

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
