// Mailbox detail — a themed right slide-over with four tabs:
//   Overview   read-only at-a-glance: health, today's usage, warmup status, identity
//   Analytics  warmup volume series + summary metrics
//   Warmup     editable warmup ramp config + live status
//   Settings   profile, signature, tags, sending caps, tracking domain
// Edits across Warmup + Settings share one form; a sticky save bar commits
// them via PATCH /emails/:id. Read data: /analytics/accounts/:id and
// /analytics/warmup?email_id=.

import React, { useMemo, useState } from "react";
import { AnimatePresence, motion } from "framer-motion";
import {
    XIcon,
    GaugeIcon,
    BarChart3Icon,
    FlameIcon,
    Settings2Icon,
    CheckCircle2Icon,
    AlertTriangleIcon,
    AlertCircleIcon,
    ClockIcon,
    CalendarIcon,
    ReplyIcon,
    SendIcon,
    CopyIcon,
    CheckIcon,
    type LucideIcon,
} from "lucide-react";
import toast from "react-hot-toast";

import type Inbox from "@/lib/api/models/app/emails/Inbox";
import type { AccountError } from "@/lib/api/models/app/analytics/AccountStatus";
import useAccountStatus from "@/lib/api/hooks/app/analytics/useAccountStatus";
import useWarmupAnalytics from "@/lib/api/hooks/app/analytics/useWarmupAnalytics";
import useUpdateEmail from "@/lib/api/hooks/app/emails/useUpdateEmail";
import useUpdateEmailTrackingDomain from "@/lib/api/hooks/app/emails/useUpdateEmailTrackingDomain";
import type { AppError } from "@/lib/api/client/normalizeError";
import buildError from "@/lib/helper/buildError";
import EmailEditor from "../EmailEditor";
import TagSelector from "../popup/select/TagSelector";
import { Loading } from "@/components/loader";
import { NumberInput, TextInput } from "@/components/ui/field";
import { cn } from "@/lib/utils";

/* ── small themed primitives ─────────────────────── */

const Eyebrow = ({ children }: { children: React.ReactNode }) => (
    <div className="text-[10px] uppercase tracking-[0.14em] text-slate-400 font-medium">{children}</div>
);

function StatCard({ label, value, sub, accent }: { label: string; value: React.ReactNode; sub?: string; accent?: boolean }) {
    return (
        <div className="px-4 py-3.5">
            <Eyebrow>{label}</Eyebrow>
            <div className={cn("mt-1 text-[24px] font-light leading-none tabular-nums", accent ? "text-sky-600" : "text-slate-900")}>{value}</div>
            {sub && <div className="mt-1.5 text-[10.5px] text-slate-400 font-mono">{sub}</div>}
        </div>
    );
}

function Row({ label, children }: { label: string; children: React.ReactNode }) {
    return (
        <div className="flex items-center justify-between gap-3 px-5 h-10 border-b border-slate-200/60 text-[12.5px]">
            <span className="text-slate-500">{label}</span>
            <span className="text-slate-900 truncate text-right">{children}</span>
        </div>
    );
}

function FieldShell({ label, hint, children }: { label: string; hint?: string; children: React.ReactNode }) {
    return (
        <div>
            <label className="block text-[12px] font-medium text-slate-700 mb-1">{label}</label>
            {children}
            {hint && <p className="text-[10.5px] text-slate-400 mt-1 leading-relaxed">{hint}</p>}
        </div>
    );
}

function NumField({ value, onChange, suffix }: { value: number; onChange: (v: number) => void; suffix?: string }) {
    // Themed number field with our own steppers, no native spinner.
    return (
        <NumberInput
            value={value}
            onChange={onChange}
            suffix={suffix}
            min={0}
            align="right"
            className="w-full h-9"
        />
    );
}

function statusTone(status: string) {
    const s = status?.toLowerCase();
    if (s === "active" || s === "healthy") return "bg-emerald-50 text-emerald-700 border-emerald-100";
    if (s === "warming" || s === "warning") return "bg-amber-50 text-amber-700 border-amber-100";
    if (s === "revoked" || s === "error" || s === "inactive") return "bg-rose-50 text-rose-700 border-rose-100";
    return "bg-slate-100 text-slate-600 border-slate-200";
}

/* ── tabs ─────────────────────── */

const TABS: { key: string; label: string; icon: LucideIcon }[] = [
    { key: "overview", label: "Overview", icon: GaugeIcon },
    { key: "analytics", label: "Analytics", icon: BarChart3Icon },
    { key: "warmup", label: "Warmup", icon: FlameIcon },
    { key: "settings", label: "Settings", icon: Settings2Icon },
];

/* ═══════════════════════════════════════════
   Drawer shell
   ═══════════════════════════════════════════ */

export default function InboxDetails({
    emails,
    view,
    setView,
}: {
    emails: Inbox[] | null;
    view: string;
    setView: React.Dispatch<React.SetStateAction<string>>;
}) {
    const mailbox = emails?.find((e) => e.id === view) ?? null;
    const close = () => setView("");

    return (
        <AnimatePresence>
            {view && mailbox && (
                <>
                    <motion.div
                        initial={{ opacity: 0 }}
                        animate={{ opacity: 1 }}
                        exit={{ opacity: 0 }}
                        transition={{ duration: 0.18 }}
                        className="fixed inset-0 z-40 bg-slate-900/40"
                        onClick={close}
                    />
                    <motion.aside
                        initial={{ x: "100%" }}
                        animate={{ x: 0 }}
                        exit={{ x: "100%" }}
                        transition={{ type: "spring", damping: 32, stiffness: 320 }}
                        className="fixed right-0 top-0 z-50 h-full w-full sm:w-[600px] bg-white border-l border-slate-200 shadow-[0_0_60px_-12px_rgba(15,23,42,0.3)] flex flex-col"
                    >
                        <Detail key={mailbox.id} mailbox={mailbox} onClose={close} />
                    </motion.aside>
                </>
            )}
        </AnimatePresence>
    );
}

/* ── editable fields tracked for the save bar ─────────────────────── */
const EDITABLE: (keyof Inbox)[] = [
    "name", "signature_html", "signature_plain", "signature_sync", "signature_code",
    "tags", "campaign_limit", "min_wait_time", "reply_to",
    "warmup_base", "warmup_max", "warmup_increase", "warmup_reply_rate",
];

function Detail({ mailbox, onClose }: { mailbox: Inbox; onClose: () => void }) {
    const [tab, setTab] = useState("overview");
    const [form, setForm] = useState<Inbox>(mailbox);
    const update = (patch: Partial<Inbox>) => setForm((f) => ({ ...f, ...patch }));

    const status = useAccountStatus(mailbox.id);
    const { from, to } = useMemo(() => {
        const end = new Date();
        const start = new Date();
        start.setDate(start.getDate() - 29);
        const fmt = (d: Date) => d.toISOString().slice(0, 10);
        return { from: fmt(start), to: fmt(end) };
    }, []);
    const warmup = useWarmupAnalytics(mailbox.id, from, to);
    const mutation = useUpdateEmail(mailbox.id);

    const dirty = useMemo(
        () => EDITABLE.some((k) => JSON.stringify(form[k]) !== JSON.stringify(mailbox[k])),
        [form, mailbox],
    );

    const save = async () => {
        const patch: Partial<Inbox> = {};
        for (const k of EDITABLE) {
            if (JSON.stringify(form[k]) !== JSON.stringify(mailbox[k])) {
                (patch as Record<string, unknown>)[k] = form[k];
            }
        }
        try {
            await mutation.mutateAsync(patch);
            toast.success("Mailbox updated");
        } catch (e) {
            toast.error(buildError(e as AppError));
        }
    };

    const initials = mailbox.email.slice(0, 2).toUpperCase();

    return (
        <>
            {/* Header */}
            <div className="shrink-0 px-5 h-14 flex items-center gap-3 border-b border-slate-200">
                <div className="w-8 h-8 rounded-lg bg-sky-50 text-sky-700 flex items-center justify-center text-[11px] font-semibold shrink-0">
                    {initials}
                </div>
                <div className="min-w-0 flex-1">
                    <div className="text-[13px] font-medium text-slate-900 truncate">{mailbox.email}</div>
                    <div className="text-[10.5px] text-slate-400 capitalize">{mailbox.provider?.replace("_", "/")}</div>
                </div>
                <span className={cn("h-5 px-2 rounded-full border text-[10px] font-semibold uppercase tracking-wide inline-flex items-center shrink-0", statusTone(mailbox.status))}>
                    {mailbox.status}
                </span>
                <button onClick={onClose} aria-label="Close" className="w-7 h-7 rounded-md flex items-center justify-center text-slate-400 hover:text-slate-900 hover:bg-slate-100 transition-colors shrink-0">
                    <XIcon className="w-4 h-4" />
                </button>
            </div>

            {/* Tabs */}
            <div className="shrink-0 px-3 flex items-center gap-1 border-b border-slate-200">
                {TABS.map((t) => {
                    const active = tab === t.key;
                    return (
                        <button
                            key={t.key}
                            onClick={() => setTab(t.key)}
                            className={cn(
                                "relative h-10 px-2.5 inline-flex items-center gap-1.5 text-[12.5px] transition-colors",
                                active ? "text-slate-900 font-medium" : "text-slate-500 hover:text-slate-800",
                            )}
                        >
                            <t.icon className="w-3.5 h-3.5" />
                            {t.label}
                            {active && (
                                <motion.span
                                    layoutId="inbox-tab-underline"
                                    className="absolute left-1.5 right-1.5 -bottom-px h-0.5 rounded-full bg-sky-600"
                                    transition={{ type: "spring", duration: 0.3, bounce: 0.15 }}
                                />
                            )}
                        </button>
                    );
                })}
            </div>

            {/* Body */}
            <div className="flex-1 min-h-0 overflow-y-auto">
                {tab === "overview" && <OverviewTab status={status.data} loading={status.isPending} mailbox={mailbox} />}
                {tab === "analytics" && <AnalyticsTab warmup={warmup.data} loading={warmup.isPending} />}
                {tab === "warmup" && <WarmupTab form={form} update={update} status={status.data} />}
                {tab === "settings" && <SettingsTab form={form} update={update} mailbox={mailbox} />}
            </div>

            {/* Save bar — only when something changed */}
            <AnimatePresence>
                {dirty && (
                    <motion.div
                        initial={{ y: 60 }}
                        animate={{ y: 0 }}
                        exit={{ y: 60 }}
                        transition={{ duration: 0.2 }}
                        className="shrink-0 h-14 px-5 flex items-center gap-2 border-t border-slate-200 bg-slate-50/60"
                    >
                        <span className="text-[11.5px] text-slate-500">Unsaved changes</span>
                        <div className="ml-auto flex items-center gap-2">
                            <button onClick={() => setForm(mailbox)} className="h-8 px-3 rounded-md border border-slate-200 hover:border-slate-300 text-[12px] text-slate-700 hover:text-slate-900 transition-colors">
                                Discard
                            </button>
                            <button onClick={save} disabled={mutation.isPending} className="h-8 px-3.5 rounded-md bg-sky-600 hover:bg-sky-700 text-white text-[12px] font-medium inline-flex items-center gap-1.5 transition-colors disabled:opacity-60">
                                {mutation.isPending && <Loading className="!w-3.5 h-3.5 text-white" />}
                                Save changes
                            </button>
                        </div>
                    </motion.div>
                )}
            </AnimatePresence>
        </>
    );
}

/* ── Overview ─────────────────────── */

function OverviewTab({ status, loading, mailbox }: { status?: import("@/lib/api/models/app/analytics/AccountStatus").default; loading: boolean; mailbox: Inbox }) {
    const health = status?.health;
    const usage = status?.daily_usage;
    const ws = status?.warmup_status;

    const healthTone =
        health?.status === "healthy" ? { bar: "bg-emerald-500", text: "text-emerald-600", icon: CheckCircle2Icon }
            : health?.status === "warning" ? { bar: "bg-amber-500", text: "text-amber-600", icon: AlertTriangleIcon }
                : { bar: "bg-rose-500", text: "text-rose-600", icon: AlertCircleIcon };
    const HealthIcon = healthTone.icon;

    const synced = mailbox.last_synced_at ? new Date(mailbox.last_synced_at) : null;

    return (
        <div className="divide-y divide-slate-200/60">
            {/* Health */}
            <div className="px-5 py-4">
                <div className="flex items-center justify-between">
                    <Eyebrow>Health</Eyebrow>
                    {health && (
                        <span className={cn("inline-flex items-center gap-1 text-[11px] font-medium capitalize", healthTone.text)}>
                            <HealthIcon className="w-3.5 h-3.5" /> {health.status}
                        </span>
                    )}
                </div>
                <div className="mt-2 flex items-end gap-2">
                    <span className="text-[30px] font-light leading-none text-slate-900 tabular-nums">{loading ? "—" : (health?.score ?? "—")}</span>
                    <span className="text-[12px] text-slate-400 mb-1">/ 100</span>
                </div>
                <div className="mt-2.5 h-1.5 w-full rounded-full bg-slate-100 overflow-hidden">
                    <div className={cn("h-full rounded-full transition-all", healthTone.bar)} style={{ width: `${health?.score ?? 0}%` }} />
                </div>
                {health?.issues && health.issues.length > 0 && (
                    <ul className="mt-3 space-y-1">
                        {health.issues.map((i, k) => (
                            <li key={k} className="text-[11.5px] text-slate-500 flex items-start gap-1.5">
                                <span className="text-amber-500 mt-0.5">•</span> {i}
                            </li>
                        ))}
                    </ul>
                )}
            </div>

            {/* Key stats */}
            <div className="grid grid-cols-2 divide-x divide-y divide-slate-200/60">
                <StatCard label="Sent today" value={usage ? usage.campaign_sent : "—"} sub={usage ? `of ${usage.campaign_limit}/day cap` : undefined} />
                <StatCard label="Warmup today" value={ws ? ws.current_volume : (usage?.warmup_sent ?? "—")} sub={ws ? `target ${ws.target_volume}` : undefined} accent />
                <StatCard label="Reply rate" value={ws ? `${ws.reply_rate}%` : "—"} sub="warmup replies" />
                <StatCard label="Days warming" value={ws ? ws.days_active : "—"} sub={ws ? `max ${ws.max_volume}/day` : undefined} />
            </div>

            {/* Errors */}
            {status?.errors && status.errors.length > 0 && (
                <div className="px-5 py-4">
                    <Eyebrow>Needs attention</Eyebrow>
                    <div className="mt-2 space-y-2">
                        {status.errors.map((e: AccountError) => (
                            <div key={e.id} className="rounded-md border border-rose-200 bg-rose-50 px-3 py-2">
                                <div className="text-[12px] font-medium text-rose-800">{e.title}</div>
                                <div className="text-[11px] text-rose-700/90 mt-0.5 leading-relaxed">{e.message}</div>
                                {e.action_required && <div className="text-[11px] text-rose-900 mt-1 font-medium">{e.action_required}</div>}
                            </div>
                        ))}
                    </div>
                </div>
            )}

            {/* Identity */}
            <div>
                <Row label="Provider"><span className="capitalize">{mailbox.provider?.replace("_", "/")}</span></Row>
                <Row label="Tracking domain">{mailbox.tracking_domain || <span className="text-slate-400">Not set</span>}</Row>
                <Row label="Daily cap">{mailbox.campaign_limit} / day</Row>
                <Row label="Min gap">{mailbox.min_wait_time} min</Row>
                <Row label="Last synced">
                    <span className="inline-flex items-center gap-1.5 text-slate-600">
                        <ClockIcon className="w-3.5 h-3.5 text-slate-400" />
                        {synced ? synced.toLocaleString() : "Never"}
                    </span>
                </Row>
                <Row label="Connected">
                    <span className="inline-flex items-center gap-1.5 text-slate-600">
                        <CalendarIcon className="w-3.5 h-3.5 text-slate-400" />
                        {new Date(mailbox.created_at).toLocaleDateString()}
                    </span>
                </Row>
            </div>
        </div>
    );
}

/* ── Analytics ─────────────────────── */

function AnalyticsTab({ warmup, loading }: { warmup?: import("@/lib/api/models/app/analytics/WarmupAnalytics").default; loading: boolean }) {
    if (loading) {
        return <div className="py-20 flex items-center justify-center"><Loading className="w-5 h-5 text-sky-500" /></div>;
    }
    if (!warmup || warmup.daily_stats.length === 0) {
        return (
            <div className="px-5 py-16 text-center">
                <BarChart3Icon className="w-5 h-5 text-slate-300 mx-auto mb-2" />
                <p className="text-[12.5px] text-slate-700 font-medium">No warmup activity yet</p>
                <p className="text-[11.5px] text-slate-400 mt-1 max-w-[34ch] mx-auto leading-relaxed">Once this mailbox starts warming, its daily volume and replies will chart here.</p>
            </div>
        );
    }
    const s = warmup.summary;
    const max = Math.max(1, ...warmup.daily_stats.map((d) => Math.max(d.emails_sent, d.target_volume)));

    return (
        <div className="divide-y divide-slate-200/60">
            <div className="grid grid-cols-2 divide-x divide-y divide-slate-200/60">
                <StatCard label="Total sent" value={s.total_sent} sub={`${s.average_daily.toFixed(1)}/day avg`} />
                <StatCard label="Replies" value={s.total_replied} sub={`${s.reply_rate.toFixed(1)}% reply rate`} accent />
                <StatCard label="Target progress" value={`${Math.round(s.target_progress)}%`} sub="toward max volume" />
                <StatCard label="Days active" value={s.days_active} sub={`${warmup.date_range.from} → ${warmup.date_range.to}`} />
            </div>

            <div className="px-5 py-4">
                <div className="flex items-center justify-between mb-3">
                    <Eyebrow>Daily volume</Eyebrow>
                    <div className="flex items-center gap-3 text-[10px] text-slate-400">
                        <span className="inline-flex items-center gap-1"><span className="w-2 h-2 rounded-sm bg-sky-500" /> Sent</span>
                        <span className="inline-flex items-center gap-1"><span className="w-2 h-2 rounded-sm bg-slate-200" /> Target</span>
                    </div>
                </div>
                <div className="flex items-end gap-0.5 h-28">
                    {warmup.daily_stats.map((d) => (
                        <div key={d.date} className="flex-1 min-w-0 relative h-full flex items-end group" title={`${d.date}: ${d.emails_sent} sent / ${d.target_volume} target · ${d.emails_replied} replies`}>
                            <div className="absolute inset-x-0 bottom-0 rounded-sm bg-slate-100" style={{ height: `${(d.target_volume / max) * 100}%` }} />
                            <div className="relative w-full rounded-sm bg-sky-500 group-hover:bg-sky-600 transition-colors" style={{ height: `${(d.emails_sent / max) * 100}%`, minHeight: d.emails_sent > 0 ? 2 : 0 }} />
                        </div>
                    ))}
                </div>
            </div>
        </div>
    );
}

/* ── Warmup (editable) ─────────────────────── */

function WarmupTab({ form, update, status }: { form: Inbox; update: (p: Partial<Inbox>) => void; status?: import("@/lib/api/models/app/analytics/AccountStatus").default }) {
    const ws = status?.warmup_status;
    return (
        <div className="divide-y divide-slate-200/60">
            {ws && (
                <div className="px-5 py-4">
                    <Eyebrow>Live status</Eyebrow>
                    <div className="mt-2 flex items-center gap-2 text-[12.5px] text-slate-700">
                        <FlameIcon className={cn("w-4 h-4", ws.enabled ? "text-orange-500" : "text-slate-300")} />
                        {ws.enabled
                            ? <span>Sending <b className="text-slate-900 tabular-nums">{ws.current_volume}</b> of <b className="text-slate-900 tabular-nums">{ws.target_volume}</b> today · day {ws.days_active}</span>
                            : <span className="text-slate-400">Warmup not active</span>}
                    </div>
                    {ws.enabled && (
                        <div className="mt-2.5 h-1.5 w-full rounded-full bg-slate-100 overflow-hidden">
                            <div className="h-full rounded-full bg-orange-400 transition-all" style={{ width: `${Math.min(100, (ws.current_volume / Math.max(1, ws.target_volume)) * 100)}%` }} />
                        </div>
                    )}
                </div>
            )}

            <div className="px-5 py-5 space-y-5">
                <Eyebrow>Ramp configuration</Eyebrow>
                <FieldShell label="Starting volume" hint="Emails per day when warmup begins.">
                    <NumField value={form.warmup_base} onChange={(v) => update({ warmup_base: v })} suffix="emails / day" />
                </FieldShell>
                <FieldShell label="Daily increase" hint="How many more emails to send each day as reputation builds.">
                    <NumField value={form.warmup_increase} onChange={(v) => update({ warmup_increase: v })} suffix="+ / day" />
                </FieldShell>
                <FieldShell label="Maximum volume" hint="Ceiling the ramp grows toward. Keep conservative for new mailboxes (≈40/day).">
                    <NumField value={form.warmup_max} onChange={(v) => update({ warmup_max: v })} suffix="emails / day" />
                </FieldShell>
                <FieldShell label="Reply rate" hint="Share of warmup mail that gets a reply, to mimic real conversation.">
                    <NumField value={form.warmup_reply_rate} onChange={(v) => update({ warmup_reply_rate: v })} suffix="%" />
                </FieldShell>
            </div>
        </div>
    );
}

/* ── Tracking domain (own save + DNS verify flow) ─────────────────────── */

// TRACKING_TARGET is the shared host customers point their CNAME at.
// Keep in sync with the backend TRACKING_DOMAIN default.
const TRACKING_TARGET = "t.warmbly.com";

function TrackingDomainCard({ mailbox }: { mailbox: Inbox }) {
    const [domain, setDomain] = useState(mailbox.tracking_domain ?? "");
    const [copied, setCopied] = useState(false);
    const mutation = useUpdateEmailTrackingDomain(mailbox.id);

    const saved = (mailbox.tracking_domain ?? "").trim();
    const verified = mailbox.tracking_domain_verified;
    const dirty = domain.trim() !== saved;

    const copyTarget = async () => {
        try {
            await navigator.clipboard.writeText(TRACKING_TARGET);
            setCopied(true);
            window.setTimeout(() => setCopied(false), 1500);
        } catch {
            // clipboard may be unavailable (insecure context); ignore
        }
    };

    const save = async () => {
        const next = domain.trim();
        try {
            const res = await mutation.mutateAsync(next);
            if (!next) {
                toast.success("Tracking domain cleared");
            } else if (res.tracking_domain_verified) {
                toast.success("Tracking domain verified");
            } else {
                toast("Saved. DNS hasn't propagated yet, re-verify in a few minutes.", { icon: "⏳" });
            }
        } catch (e) {
            toast.error(buildError(e as AppError));
        }
    };

    const clear = async () => {
        setDomain("");
        try {
            await mutation.mutateAsync("");
            toast.success("Tracking domain cleared");
        } catch (e) {
            toast.error(buildError(e as AppError));
        }
    };

    return (
        <div className="px-5 py-5 space-y-3">
            <div className="flex items-center justify-between">
                <Eyebrow>Tracking domain</Eyebrow>
                {saved ? (
                    verified ? (
                        <span className="inline-flex items-center gap-1 h-5 px-2 rounded-full border border-emerald-100 bg-emerald-50 text-emerald-700 text-[10px] font-semibold uppercase tracking-wide">
                            <CheckCircle2Icon className="w-3 h-3" /> Verified
                        </span>
                    ) : (
                        <span className="inline-flex items-center gap-1 h-5 px-2 rounded-full border border-amber-100 bg-amber-50 text-amber-700 text-[10px] font-semibold uppercase tracking-wide">
                            <ClockIcon className="w-3 h-3" /> Pending DNS
                        </span>
                    )
                ) : (
                    <span className="inline-flex items-center gap-1 h-5 px-2 rounded-full border border-slate-200 bg-slate-100 text-slate-500 text-[10px] font-semibold uppercase tracking-wide">
                        Not set
                    </span>
                )}
            </div>

            <FieldShell label="Custom tracking domain" hint="Track opens & clicks through your own subdomain instead of the shared host. Improves deliverability.">
                <TextInput value={domain} placeholder="track.yourdomain.com" onChange={setDomain} className="w-full h-9" />
            </FieldShell>

            {domain.trim() && (
                <div className="rounded-md border border-slate-200 bg-slate-50/70 p-3 space-y-2">
                    <div className="text-[11px] text-slate-500 leading-relaxed">
                        Add this CNAME record at your DNS provider, then save to verify:
                    </div>
                    <div className="grid grid-cols-[56px_1fr] gap-x-3 gap-y-1.5 text-[11.5px] items-center">
                        <span className="text-slate-400">Type</span>
                        <span className="font-mono text-slate-700">CNAME</span>
                        <span className="text-slate-400">Name</span>
                        <span className="font-mono text-slate-700 truncate">{domain.trim()}</span>
                        <span className="text-slate-400">Value</span>
                        <span className="font-mono text-slate-700 inline-flex items-center gap-1.5 min-w-0">
                            <span className="truncate">{TRACKING_TARGET}</span>
                            <button
                                type="button"
                                onClick={copyTarget}
                                className="shrink-0 text-slate-400 hover:text-slate-700 transition-colors"
                                aria-label="Copy CNAME target"
                            >
                                {copied ? <CheckIcon className="w-3 h-3 text-emerald-600" /> : <CopyIcon className="w-3 h-3" />}
                            </button>
                        </span>
                    </div>
                </div>
            )}

            <div className="flex items-center gap-2">
                <button
                    onClick={save}
                    disabled={mutation.isPending || (!dirty && (verified || !saved))}
                    className="h-8 px-3.5 rounded-md bg-sky-600 hover:bg-sky-700 text-white text-[12px] font-medium inline-flex items-center gap-1.5 transition-colors disabled:opacity-60"
                >
                    {mutation.isPending && <Loading className="!w-3.5 h-3.5 text-white" />}
                    {!saved || dirty ? "Save & verify" : verified ? "Verified" : "Re-verify"}
                </button>
                {saved && !dirty && (
                    <button
                        onClick={clear}
                        disabled={mutation.isPending}
                        className="h-8 px-3 rounded-md border border-slate-200 hover:border-slate-300 text-[12px] text-slate-600 hover:text-slate-900 transition-colors disabled:opacity-60"
                    >
                        Clear
                    </button>
                )}
            </div>
        </div>
    );
}

/* ── Settings (editable) ─────────────────────── */

function SettingsTab({ form, update, mailbox }: { form: Inbox; update: (p: Partial<Inbox>) => void; mailbox: Inbox }) {
    return (
        <div className="divide-y divide-slate-200/60">
            <div className="px-5 py-5 space-y-4">
                <Eyebrow>Sender profile</Eyebrow>
                <FieldShell label="Display name">
                    <TextInput value={form.name ?? ""} placeholder="First Last" onChange={(v) => update({ name: v })} className="w-full h-9" />
                </FieldShell>
                <FieldShell
                    label="Reply-to"
                    hint={`Where replies land. Leave empty to use ${mailbox.email}.`}
                >
                    <div className="flex items-center gap-1.5">
                        <TextInput
                            value={form.reply_to ?? ""}
                            placeholder={mailbox.email}
                            onChange={(v) => update({ reply_to: v })}
                            className="w-full h-9"
                        />
                        {form.reply_to !== mailbox.email && (
                            <button
                                type="button"
                                onClick={() => update({ reply_to: mailbox.email })}
                                className="h-9 px-2.5 rounded-md border border-slate-200 hover:border-slate-300 text-[12px] text-slate-700 hover:text-slate-900 transition-colors shrink-0"
                            >
                                Use mailbox address
                            </button>
                        )}
                    </div>
                </FieldShell>
            </div>

            <div className="px-5 py-5 space-y-2">
                <Eyebrow>Signature</Eyebrow>
                <div className="overflow-x-auto">
                    <EmailEditor
                        id="inbox-signature"
                        htmlText={form.signature_html}
                        setHtmlText={(v) => update({ signature_html: v })}
                        plainText={form.signature_plain}
                        setPlainText={(v) => update({ signature_plain: v })}
                        sync={form.signature_sync}
                        setSync={(v) => update({ signature_sync: v })}
                        code={form.signature_code}
                        setCode={(v) => update({ signature_code: v })}
                    />
                </div>
            </div>

            <div className="px-5 py-5 space-y-2">
                <Eyebrow>Tags</Eyebrow>
                <TagSelector
                    selected={form.tags}
                    onAdd={(v) => update({ tags: [...form.tags, v] })}
                    onRemove={(v) => update({ tags: form.tags.filter((t) => t !== v) })}
                />
            </div>

            <div className="px-5 py-5 space-y-5">
                <Eyebrow>Sending limits</Eyebrow>
                <FieldShell label="Daily campaign cap" hint="Max cold-campaign emails per day. Default 50; raise only with good reputation.">
                    <NumField value={form.campaign_limit} onChange={(v) => update({ campaign_limit: v })} suffix="emails / day" />
                </FieldShell>
                <FieldShell label="Minimum gap" hint="Smallest delay between two sends from this mailbox.">
                    <NumField value={form.min_wait_time} onChange={(v) => update({ min_wait_time: v })} suffix="minutes" />
                </FieldShell>
            </div>

            <TrackingDomainCard mailbox={mailbox} />

            <div className="flex items-center gap-1.5 px-5 py-3 text-[11px] text-slate-400">
                <SendIcon className="w-3 h-3" /> Changes apply to new sends. <ReplyIcon className="w-3 h-3 ml-1" /> Signature applies to replies too.
            </div>
        </div>
    );
}
