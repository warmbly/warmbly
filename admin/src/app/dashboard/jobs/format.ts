// Time formatting shared by the operations pages (Sync, Sends, Jobs): every
// timestamp there reads as an age or a countdown, never as a raw date.

import { formatDistanceToNowStrict } from "date-fns";

export function relative(ts: string | null | undefined, empty = "never"): string {
    if (!ts) return empty;
    const d = new Date(ts);
    if (Number.isNaN(d.getTime())) return "—";
    return formatDistanceToNowStrict(d, { addSuffix: true });
}

export function absolute(ts: string | null | undefined): string {
    if (!ts) return "";
    const d = new Date(ts);
    return Number.isNaN(d.getTime()) ? "" : d.toLocaleString();
}

export function humanSeconds(seconds: number): string {
    const total = Math.max(0, Math.round(seconds));
    if (total < 60) return `${total}s`;
    const m = Math.floor(total / 60);
    const s = total % 60;
    if (m < 60) return s ? `${m}m ${s}s` : `${m}m`;
    const h = Math.floor(m / 60);
    const rm = m % 60;
    if (h < 24) return rm ? `${h}h ${rm}m` : `${h}h`;
    const d = Math.floor(h / 24);
    const rh = h % 24;
    return rh ? `${d}d ${rh}h` : `${d}d`;
}

export function humanDuration(ms: number): string {
    if (!Number.isFinite(ms) || ms < 0) return "—";
    if (ms < 1000) return `${Math.round(ms)}ms`;
    if (ms < 10_000) return `${(ms / 1000).toFixed(1)}s`;
    return humanSeconds(ms / 1000);
}

export function humanInterval(seconds: number): string {
    if (!seconds || seconds <= 0) return "on demand";
    return `every ${humanSeconds(seconds)}`;
}

export function shortId(id: string | null | undefined): string {
    return id ? id.slice(0, 8) : "";
}
