// A domain the customer already owns, moving over to Warmbly: where it stands, what to change at the
// registrar or DNS provider, and anything in the way of mail. Used in the buy flow and the domain drawer.
import React from "react";
import { AnimatePresence, motion, useReducedMotion } from "framer-motion";
import { AlertTriangleIcon, CheckCircle2Icon, ClockIcon, Loader2Icon, RefreshCwIcon, RotateCcwIcon, Trash2Icon } from "lucide-react";
import { useConfirm } from "@/hooks/context/confirm";
import { useCheckStoreDomainConnection, useDeleteDnsRecord, useRegenerateStoreDomainConnection } from "@/lib/api/hooks/app/store/useStore";
import type { DnsConflict, StoreDomain } from "@/lib/api/models/app/store/Store";
import { fullRecordName, manualRecords, timeLeft } from "@/lib/buy/connection";
import { cn } from "@/lib/utils";
import CopyButton from "./CopyButton";
import { PressButton } from "./Press";

/** Re-renders every half minute, for countdowns and "checked 2 min ago". */
function useNow(ms = 30_000): number {
    const [now, setNow] = React.useState(() => Date.now());
    React.useEffect(() => {
        const t = window.setInterval(() => setNow(Date.now()), ms);
        return () => window.clearInterval(t);
    }, [ms]);
    return now;
}

function ago(v: Date | string | null | undefined, now: number): string | null {
    if (!v) return null;
    const t = (v instanceof Date ? v : new Date(v)).getTime();
    if (Number.isNaN(t)) return null;
    const mins = Math.max(0, Math.floor((now - t) / 60_000));
    if (mins < 1) return "Checked just now";
    if (mins < 60) return `Checked ${mins} min ago`;
    return `Checked ${Math.floor(mins / 60)} h ago`;
}

type Tone = "sky" | "emerald" | "amber" | "red";
const TONES: Record<Tone, string> = {
    sky: "bg-sky-50 border-sky-100 text-sky-800",
    emerald: "bg-emerald-50 border-emerald-100 text-emerald-800",
    amber: "bg-amber-50 border-amber-200 text-amber-900",
    red: "bg-red-50 border-red-100 text-red-800",
};

function headline(d: StoreDomain): { text: string; tone: Tone } {
    const c = d.connection;
    const ns = c?.method === "nameservers";
    switch (c?.status) {
        case "active":
            return (d.dns_conflicts ?? []).length > 0 ? { text: "Connected, with records in the way of mail", tone: "amber" } : { text: "Connected", tone: "emerald" };
        case "moved":
            return { text: ns ? "The nameservers moved away from Warmbly" : "Records Warmbly needs went missing", tone: "red" };
        case "expired":
            return { text: "These instructions expired", tone: "amber" };
        default:
            return { text: ns ? "Waiting for the new nameservers" : "Waiting for the records", tone: "sky" };
    }
}

export default function ConnectionGuide({ domain: d, canEdit }: { domain: StoreDomain; canEdit: boolean }) {
    const reduced = useReducedMotion();
    const now = useNow();
    const check = useCheckStoreDomainConnection();
    const regenerate = useRegenerateStoreDomainConnection();
    const c = d.connection;
    if (!c) return null;
    const head = headline(d);
    const left = c.status === "pending" ? timeLeft(c.expires_at, now) : null;
    const checked = ago(c.checked_at, now);
    const showSteps = c.status === "pending" || c.status === "moved";
    const conflicts = d.dns_conflicts ?? [];

    return (
        <div className="space-y-3">
            <div className={cn("rounded-md border px-3 py-2 flex flex-wrap items-center gap-x-2.5 gap-y-1", TONES[head.tone])} role="status" aria-live="polite">
                <StatusIcon status={c.status} tone={head.tone} />
                <span className="text-[12.5px] font-medium">{head.text}</span>
                <span className="ml-auto flex items-center gap-2 text-[11px] opacity-80">
                    {left && (
                        <span className="inline-flex items-center gap-1 tabular-nums">
                            <ClockIcon className="w-3 h-3" /> {left}
                        </span>
                    )}
                    {checked && <span>{checked}</span>}
                </span>
            </div>

            <AnimatePresence initial={false} mode="wait">
                <motion.div
                    key={`${c.status}-${c.method}`}
                    initial={reduced ? false : { opacity: 0, y: 4 }}
                    animate={{ opacity: 1, y: 0 }}
                    exit={reduced ? undefined : { opacity: 0 }}
                    transition={{ duration: 0.18 }}
                    className="space-y-3"
                >
                    {c.status === "expired" && (
                        <p className="text-[12px] text-slate-600 leading-relaxed">
                            The change did not show up within 48 hours, so these instructions lapsed. Start again for a fresh set; anything you already changed
                            can stay as it is until then.
                        </p>
                    )}
                    {c.status === "active" && conflicts.length === 0 && (
                        <p className="text-[12px] text-slate-600 leading-relaxed">
                            {c.method === "nameservers"
                                ? `${d.name} runs on Warmbly's DNS now. Keep these nameservers in place at your registrar.`
                                : `${d.name} is connected. Keep the records in place at your DNS provider.`}
                        </p>
                    )}
                    {showSteps && c.method === "nameservers" && <NameserverSteps d={d} moved={c.status === "moved"} />}
                    {showSteps && c.method === "manual" && <ManualSteps d={d} moved={c.status === "moved"} />}
                </motion.div>
            </AnimatePresence>

            {conflicts.length > 0 && <Conflicts d={d} conflicts={conflicts} canEdit={canEdit} />}

            {canEdit && (
                <div className="flex flex-wrap items-center gap-2">
                    {c.status === "expired" ? (
                        <PressButton disabled={regenerate.isPending} onClick={() => regenerate.mutate(d.id)}>
                            {regenerate.isPending ? <Loader2Icon className="w-3 h-3 animate-spin" /> : <RotateCcwIcon className="w-3 h-3" />} Start again
                        </PressButton>
                    ) : (
                        <PressButton tone="secondary" disabled={check.isPending} onClick={() => check.mutate(d.id)}>
                            <RefreshCwIcon className={cn("w-3 h-3", check.isPending && "animate-spin")} /> {check.isPending ? "Checking…" : "Check now"}
                        </PressButton>
                    )}
                    {c.status === "pending" && <span className="text-[11px] text-slate-400">Warmbly also checks on its own and updates this page.</span>}
                </div>
            )}
        </div>
    );
}

function StatusIcon({ status, tone }: { status: string; tone: Tone }) {
    const reduced = useReducedMotion();
    if (status === "active" && tone === "emerald") return <CheckCircle2Icon className="w-3.5 h-3.5 shrink-0" />;
    if (status === "pending")
        return (
            <span className="relative size-2 shrink-0" aria-hidden="true">
                {!reduced && <span className="absolute inset-0 rounded-full bg-sky-400 animate-ping opacity-60" />}
                <span className="relative block size-2 rounded-full bg-sky-500" />
            </span>
        );
    return <AlertTriangleIcon className="w-3.5 h-3.5 shrink-0" />;
}

function Step({ n, children }: { n: number; children: React.ReactNode }) {
    return (
        <li className="flex gap-2.5">
            <span className="size-5 rounded-full bg-slate-100 text-slate-500 text-[10.5px] font-semibold inline-flex items-center justify-center shrink-0 mt-px">{n}</span>
            <div className="min-w-0 flex-1 text-[12px] text-slate-700 leading-relaxed">{children}</div>
        </li>
    );
}

function NameserverSteps({ d, moved }: { d: StoreDomain; moved: boolean }) {
    const current = d.connection?.current_nameservers ?? [];
    return (
        <ol className="space-y-3">
            <Step n={1}>
                {moved
                    ? `Set the nameservers for ${d.name} back to these two at its registrar.`
                    : `Sign in where you registered ${d.name} and replace its nameservers with these two.`}
                <ul className="mt-1.5 rounded-md border border-slate-200 divide-y divide-slate-100 bg-white">
                    {(d.nameservers ?? []).map((ns) => (
                        <li key={ns} className="pl-2.5 pr-1 h-8 flex items-center gap-2">
                            <code className="flex-1 min-w-0 font-mono text-[12px] text-slate-900 truncate select-all">{ns}</code>
                            <CopyButton value={ns} label={ns} text />
                        </li>
                    ))}
                    {(d.nameservers ?? []).length === 0 && <li className="px-2.5 h-8 flex items-center text-[11.5px] text-slate-400">Preparing the nameservers…</li>}
                </ul>
                {current.length > 0 && (
                    <p className="mt-1.5 text-[11px] text-slate-500">
                        Public DNS shows <span className="font-mono">{current.join(", ")}</span> right now.
                    </p>
                )}
            </Step>
            <Step n={2}>Turn DNSSEC off for {d.name} at the registrar if it is on, or the change does not take.</Step>
            {!moved && (
                <Step n={3}>
                    That is all. The records {d.name} serves today are copied over, so your website and current mail keep working. Most registrars apply the
                    change within an hour; it can take longer.
                </Step>
            )}
        </ol>
    );
}

function ManualSteps({ d, moved }: { d: StoreDomain; moved: boolean }) {
    const records = manualRecords(d.dns);
    return (
        <div className="space-y-2">
            <p className="text-[12px] text-slate-700 leading-relaxed">
                {moved
                    ? `Add these records back at the DNS provider for ${d.name}.`
                    : `Add these records at the DNS provider for ${d.name}, starting with the ownership record. Leave the records you have now as they are, unless one is listed as in the way below.`}
            </p>
            {records.length === 0 ? (
                <div className="space-y-1.5" aria-busy="true">
                    {Array.from({ length: 3 }).map((_, i) => (
                        <div key={i} className="h-9 rounded skeleton-shimmer" />
                    ))}
                </div>
            ) : (
                <ul className="rounded-md border border-slate-200 divide-y divide-slate-100 bg-white">
                    {records.map((r) => (
                        <li key={r.id} className={cn("px-2.5 py-2 space-y-1", r.purpose === "ownership" && "bg-sky-50/40")}>
                            <div className="flex items-center gap-2 min-w-0">
                                <span className="w-12 shrink-0 font-mono text-[10.5px] text-slate-500">{r.type}</span>
                                <code className="flex-1 min-w-0 font-mono text-[11.5px] text-slate-900 truncate" title={fullRecordName(r.name, d.name)}>
                                    {r.name}
                                </code>
                                {r.purpose === "ownership" && (
                                    <span className="shrink-0 h-[18px] px-1.5 rounded-full bg-sky-50 border border-sky-100 text-sky-700 text-[10px] font-medium inline-flex items-center">
                                        First
                                    </span>
                                )}
                                <CopyButton value={r.name} label={`${r.type} name`} />
                            </div>
                            <div className="flex items-center gap-2 min-w-0 pl-14">
                                <code className="flex-1 min-w-0 font-mono text-[11px] text-slate-600 break-all">
                                    {r.priority !== null && r.priority !== undefined ? `${r.priority} ` : ""}
                                    {r.content}
                                </code>
                                <CopyButton value={r.content} label={`${r.type} value`} />
                            </div>
                        </li>
                    ))}
                </ul>
            )}
            <p className="text-[11px] text-slate-400">Some providers want the full name, like {fullRecordName("_dmarc", d.name)}. TTL can stay at its default.</p>
        </div>
    );
}

function Conflicts({ d, conflicts, canEdit }: { d: StoreDomain; conflicts: DnsConflict[]; canEdit: boolean }) {
    const confirm = useConfirm();
    const del = useDeleteDnsRecord();
    const remove = (c: DnsConflict) =>
        confirm.show(
            `Remove the ${c.type} record ${c.name} (${c.content}) from ${d.name}? It clashes with the records mail needs.`,
            async () => {
                if (c.record_id) await del.mutateAsync({ domainId: d.id, recordId: c.record_id }).catch(() => undefined);
            },
            undefined,
            { title: "Remove record", confirmLabel: "Remove record" },
        );
    return (
        <div className="rounded-md border border-amber-200 bg-amber-50/50 p-2.5 space-y-2">
            <p className="text-[12px] text-amber-900 leading-relaxed flex items-start gap-1.5">
                <AlertTriangleIcon className="w-3.5 h-3.5 mt-0.5 shrink-0 text-amber-600" />
                {conflicts.length === 1 ? "This record is" : "These records are"} in the way of mail. Mailboxes can be added once {conflicts.length === 1 ? "it is" : "they are"}{" "}
                gone.
            </p>
            <ul className="rounded-md border border-amber-100 bg-white divide-y divide-slate-100">
                {conflicts.map((c, i) => (
                    <li key={c.record_id ?? `${c.type}-${c.name}-${i}`} className="px-2.5 py-1.5 flex flex-wrap items-center gap-2 min-w-0">
                        <span className="w-12 shrink-0 font-mono text-[10.5px] text-slate-500">{c.type}</span>
                        <span className="w-24 shrink-0 font-mono text-[11px] text-slate-800 truncate" title={c.name}>
                            {c.name}
                        </span>
                        <span className="flex-1 min-w-0 font-mono text-[11px] text-slate-500 truncate" title={c.content}>
                            {c.content}
                        </span>
                        {c.record_id ? (
                            canEdit && (
                                <PressButton tone="danger" className="h-6 px-2 text-[11px]" onClick={() => remove(c)}>
                                    <Trash2Icon className="w-3 h-3" /> Remove
                                </PressButton>
                            )
                        ) : (
                            <span className="text-[11px] text-amber-800">Remove it at your DNS provider</span>
                        )}
                    </li>
                ))}
            </ul>
        </div>
    );
}
