// Small formatting helpers shared by the fleet, transfers and admins pages.

export function shortId(id: string | null | undefined): string {
    return id ? id.slice(0, 8) : "—";
}

export function fmtDateTime(iso: string | null | undefined): string {
    return iso ? new Date(iso).toLocaleString() : "—";
}

export function fmtDate(iso: string | null | undefined): string {
    return iso ? new Date(iso).toLocaleDateString() : "—";
}

// "3m ago" style relative time for feeds; falls back to the date past a week.
export function fmtAgo(iso: string | null | undefined): string {
    if (!iso) return "—";
    const diff = Date.now() - new Date(iso).getTime();
    const s = Math.round(diff / 1000);
    if (s < 60) return `${s}s ago`;
    const m = Math.round(s / 60);
    if (m < 60) return `${m}m ago`;
    const h = Math.round(m / 60);
    if (h < 48) return `${h}h ago`;
    const d = Math.round(h / 24);
    if (d < 8) return `${d}d ago`;
    return new Date(iso).toLocaleDateString();
}

export function userName(u: { first_name?: string; last_name?: string; email: string }): string {
    return `${u.first_name ?? ""} ${u.last_name ?? ""}`.trim() || u.email;
}
