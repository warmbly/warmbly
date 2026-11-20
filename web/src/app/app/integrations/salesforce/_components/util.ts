import { errorMessage } from "@/lib/errors/message";

export function errMsg(err: unknown, fallback: string): string {
    return errorMessage(err, fallback);
}

/** Whether two id lists hold the same ids, in any order. */
export function sameIds(a: readonly string[] | null | undefined, b: readonly string[] | null | undefined): boolean {
    const x = [...(a ?? [])].sort();
    const y = [...(b ?? [])].sort();
    return x.length === y.length && x.every((v, i) => v === y[i]);
}

export function isForbidden(err: unknown): boolean {
    return (err as { status?: number } | null)?.status === 403;
}

// "3 min ago" style relative time for sync lines.
export function ago(d: Date | string | null | undefined): string {
    if (!d) return "never";
    const t = typeof d === "string" ? new Date(d).getTime() : d.getTime();
    if (Number.isNaN(t)) return "never";
    const sec = Math.max(0, Math.round((Date.now() - t) / 1000));
    if (sec < 45) return "just now";
    const min = Math.round(sec / 60);
    if (min < 60) return `${min} min ago`;
    const hr = Math.round(min / 60);
    if (hr < 24) return `${hr} h ago`;
    const day = Math.round(hr / 24);
    if (day < 30) return `${day} d ago`;
    return new Date(t).toLocaleDateString();
}

export function absolute(d: Date | string | null | undefined): string {
    if (!d) return "";
    const dt = typeof d === "string" ? new Date(d) : d;
    if (Number.isNaN(dt.getTime())) return "";
    return dt.toLocaleString(undefined, { month: "short", day: "numeric", hour: "numeric", minute: "2-digit" });
}
