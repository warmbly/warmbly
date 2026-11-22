// The small pieces every social account surface shares: avatar, status pill, licence chips, usage meters, billing line.
import { useState } from "react";
import type { SocialAccount, SocialUsage } from "@/lib/api/models/app/social/Social";
import { fmtMonthly } from "@/lib/buy/money";
import { cn } from "@/lib/utils";

export function initials(name: string): string {
    const parts = name.trim().split(/\s+/).filter(Boolean);
    return ((parts[0]?.[0] ?? "") + (parts.length > 1 ? (parts[parts.length - 1]?.[0] ?? "") : "")).toUpperCase() || "?";
}

/** A profile photo, or the person's initials when there is none or it fails to load. */
export function Avatar({ src, name, size = 32, square = false, className }: { src?: string | null; name: string; size?: number; square?: boolean; className?: string }) {
    const [failed, setFailed] = useState(false);
    const show = !!src && !failed && src.startsWith("https://");
    return (
        <span
            className={cn(
                "shrink-0 inline-flex items-center justify-center overflow-hidden bg-slate-100 text-slate-500 font-medium ring-1 ring-slate-200",
                square ? "rounded-md" : "rounded-full",
                className,
            )}
            style={{ width: size, height: size, fontSize: Math.max(9, Math.round(size * 0.36)) }}
            aria-hidden
        >
            {show ? <img src={src} alt="" referrerPolicy="no-referrer" onError={() => setFailed(true)} className="size-full object-cover" /> : initials(name)}
        </span>
    );
}

const STATUS: Record<string, { label: string; tone: string; dot: string }> = {
    active: { label: "Active", tone: "bg-emerald-50 text-emerald-700 ring-emerald-200", dot: "bg-emerald-500" },
    connecting: { label: "Connecting", tone: "bg-sky-50 text-sky-700 ring-sky-200", dot: "bg-sky-500" },
    reconnect_required: { label: "Reconnect needed", tone: "bg-amber-50 text-amber-800 ring-amber-200", dot: "bg-amber-500" },
    restricted: { label: "Restricted", tone: "bg-red-50 text-red-700 ring-red-200", dot: "bg-red-500" },
    paused: { label: "Paused", tone: "bg-slate-100 text-slate-600 ring-slate-200", dot: "bg-slate-400" },
    disconnected: { label: "Disconnected", tone: "bg-slate-100 text-slate-600 ring-slate-200", dot: "bg-slate-400" },
    removed: { label: "Removed", tone: "bg-slate-100 text-slate-500 ring-slate-200", dot: "bg-slate-300" },
};

export function statusLabel(status: string): string {
    return STATUS[status]?.label ?? status.replace(/_/g, " ");
}

export function StatusPill({ status, className }: { status: string; className?: string }) {
    const s = STATUS[status] ?? STATUS.disconnected;
    return (
        <span className={cn("inline-flex items-center gap-1.5 h-5 px-1.5 rounded-full ring-1 text-[11px] font-medium whitespace-nowrap", s.tone, className)}>
            <span className={cn("size-1.5 rounded-full", s.dot, status === "connecting" && "animate-pulse")} />
            {statusLabel(status)}
        </span>
    );
}

const LICENCE: Record<string, string> = {
    classic: "LinkedIn",
    premium: "Premium",
    sales_navigator: "Sales Navigator",
    recruiter: "Recruiter",
    recruiter_lite: "Recruiter Lite",
};

export const licenceLabel = (l: string) => LICENCE[l] ?? l.replace(/_/g, " ").replace(/^./, (c) => c.toUpperCase());

export function LicenceChips({ licences, className }: { licences: string[]; className?: string }) {
    if (!licences?.length) return null;
    return (
        <span className={cn("inline-flex flex-wrap gap-1", className)}>
            {licences.map((l) => (
                <span key={l} className="h-[18px] px-1.5 rounded bg-sky-50 text-sky-700 text-[10.5px] font-medium inline-flex items-center whitespace-nowrap">
                    {licenceLabel(l)}
                </span>
            ))}
        </span>
    );
}

/** Short plural labels for usage meters and limit fields. */
export const ACTION_LABEL: Record<string, string> = {
    invite: "Invites",
    message: "Messages",
    inmail: "InMail",
    voice_note: "Voice notes",
    profile_visit: "Visits",
    follow: "Follows",
    like_post: "Likes",
    comment_post: "Comments",
    endorse: "Endorsements",
    withdraw_invite: "Withdrawals",
    search: "Searches",
};

export const actionPlural = (a: string) => ACTION_LABEL[a] ?? a.replace(/_/g, " ").replace(/^./, (c) => c.toUpperCase());

/** The actions an account has limits for, in a stable order. */
export function limitKeys(account: Pick<SocialAccount, "limits" | "limit_ceilings">): string[] {
    const keys = new Set([...Object.keys(account.limits ?? {}), ...Object.keys(account.limit_ceilings ?? {})]);
    const order = Object.keys(ACTION_LABEL);
    return [...keys].sort((a, b) => (order.indexOf(a) === -1 ? 99 : order.indexOf(a)) - (order.indexOf(b) === -1 ? 99 : order.indexOf(b)));
}

export function usageFor(account: Pick<SocialAccount, "usage" | "limits">, action: string): SocialUsage {
    const u = account.usage?.find((x) => x.action === action);
    if (u) return u;
    const limit = (account.limits as Record<string, number | undefined>)?.[action] ?? 0;
    return { action, today: 0, daily_limit: limit };
}

export function Meter({ usage, compact = false }: { usage: SocialUsage; compact?: boolean }) {
    const limit = Math.max(0, usage.daily_limit);
    const pct = limit > 0 ? Math.min(100, Math.round((usage.today / limit) * 100)) : 0;
    const full = limit > 0 && usage.today >= limit;
    return (
        <div className={cn("min-w-0", compact ? "w-[116px]" : "w-full")} title={`${actionPlural(usage.action)}: ${usage.today} of ${limit} today`}>
            <div className="flex items-baseline justify-between gap-1">
                <span className="text-[10.5px] text-slate-500 truncate">{actionPlural(usage.action)}</span>
                <span className={cn("text-[10.5px] tabular-nums", full ? "text-amber-700 font-medium" : "text-slate-700")}>
                    {usage.today}
                    <span className="text-slate-400">/{limit}</span>
                </span>
            </div>
            <div className="mt-0.5 h-1 rounded-full bg-slate-100 overflow-hidden">
                <div className={cn("h-full rounded-full transition-[width] duration-500", full ? "bg-amber-500" : "bg-sky-500")} style={{ width: `${pct}%` }} />
            </div>
        </div>
    );
}

export function fmtDay(d: Date | string | null | undefined): string {
    if (!d) return "";
    const t = d instanceof Date ? d : new Date(d);
    if (Number.isNaN(t.getTime())) return "";
    return t.toLocaleDateString(undefined, { month: "short", day: "numeric", year: t.getFullYear() === new Date().getFullYear() ? undefined : "numeric" });
}

/** "Paid through Mar 3 · $49/mo", "Billed by GraphKit", "Not charged", or the overdue line. */
export function billingLine(a: Pick<SocialAccount, "billing">): { text: string; tone: "muted" | "warn" } {
    const b = a.billing;
    if (!b) return { text: "", tone: "muted" };
    if (b.mode === "linked") return { text: "Billed by GraphKit", tone: "muted" };
    if (b.unpaid_since) return { text: `Payment overdue since ${fmtDay(b.unpaid_since)}`, tone: "warn" };
    if (!b.monthly_micros) return { text: "Not charged", tone: "muted" };
    const price = b.monthly_micros ? ` · ${fmtMonthly(b.monthly_micros)}` : "";
    return b.paid_through ? { text: `Paid through ${fmtDay(b.paid_through)}${price}`, tone: "muted" } : { text: price.replace(/^ · /, ""), tone: "muted" };
}

export function BillingLine({ account, className }: { account: Pick<SocialAccount, "billing">; className?: string }) {
    const l = billingLine(account);
    if (!l.text) return null;
    return <span className={cn("text-[11px] whitespace-nowrap", l.tone === "warn" ? "text-red-600 font-medium" : "text-slate-500", className)}>{l.text}</span>;
}

export const TIER_LABEL: Record<string, string> = { standard: "Standard", agency: "Agency", agency_pro: "Agency Pro" };
