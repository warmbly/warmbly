// Run: a background import as it moves, then what it did. Everything here is
// read from the import itself, so it is the same view for whoever opens it.

import React from "react";
import useBrowseState from "@/hooks/useBrowseState";
import { browseText } from "@/lib/browse-contacts-campaigns";
import { AnimatePresence, motion } from "framer-motion";
import {
    AlertTriangleIcon,
    CheckCircle2Icon,
    CircleSlashIcon,
    ClockIcon,
    DownloadIcon,
    InfoIcon,
    Loader2Icon,
    RefreshCwIcon,
    ShieldAlertIcon,
    UserPlusIcon,
    XCircleIcon,
    XIcon,
} from "lucide-react";
import toast from "react-hot-toast/headless";
import { useMutation, useQueryClient } from "@tanstack/react-query";

import { useContactImport, CONTACT_IMPORTS_KEY } from "@/lib/api/hooks/app/contacts/useContactImports";
import { cancelContactImport, downloadContactImportFailures } from "@/lib/api/client/app/contacts/contactImports";
import { downloadBlob } from "@/lib/api/client/app/contacts/exportContacts";
import { isImportActive, type ContactImport } from "@/lib/api/models/app/contacts/ContactImport";
import { SearchInput } from "@/components/ui/field";
import AnimatedNumber from "@/components/ui/AnimatedNumber";
import { useConfirm } from "@/hooks/context/confirm";
import { describeError } from "../importShared";
import StatCard from "./StatCard";

export default function RunStep({ importId, segmentNames }: { importId: string; segmentNames?: Map<string, string> }) {
    const { data: imp, error } = useContactImport(importId);

    if (!imp) {
        return error ? (
            <p className="py-10 text-center text-[12px] text-red-600">{describeError(error, "The import could not be loaded.")}</p>
        ) : (
            <div className="py-12 flex justify-center">
                <Loader2Icon className="w-5 h-5 text-slate-400 animate-spin" />
            </div>
        );
    }
    return isImportActive(imp.status) || imp.status === "draft" ? (
        <Progress imp={imp} />
    ) : (
        <Finished imp={imp} pinned={(imp.options?.segment_ids ?? []).map((id) => segmentNames?.get(id) ?? "a segment")} />
    );
}

function Progress({ imp }: { imp: ContactImport }) {
    const confirm = useConfirm();
    const queryClient = useQueryClient();
    const cancel = useMutation({
        mutationFn: () => cancelContactImport(imp.id),
        onSuccess: (next) => {
            queryClient.setQueryData([...CONTACT_IMPORTS_KEY, imp.id], next);
            void queryClient.invalidateQueries({ queryKey: ["contacts"] });
        },
        onError: (err) => toast.error(describeError(err, "The import could not be cancelled.")),
    });

    const pct = imp.total > 0 ? Math.min(100, (imp.processed / imp.total) * 100) : 0;
    const eta = estimate(imp);

    return (
        <div className="space-y-4">
            <div className="flex items-center gap-3">
                <div className="size-10 rounded-full bg-sky-50 text-sky-600 flex items-center justify-center shrink-0">
                    {imp.status === "queued" ? <ClockIcon className="w-5 h-5" /> : <Loader2Icon className="w-5 h-5 animate-spin" />}
                </div>
                <div className="min-w-0 flex-1">
                    <p className="text-[13.5px] font-semibold text-slate-900">
                        {imp.status === "queued" ? "Waiting to start…" : "Importing contacts"}
                    </p>
                    <p className="text-[11.5px] text-slate-500 truncate">{imp.filename}</p>
                </div>
                <button
                    type="button"
                    disabled={cancel.isPending}
                    onClick={() =>
                        confirm.show(
                            "Stop this import? Rows already imported stay in your contacts.",
                            async () => {
                                await cancel.mutateAsync();
                            },
                        )
                    }
                    className="h-7 px-2.5 rounded-md border border-slate-200 hover:border-red-200 hover:bg-red-50 text-[12px] text-slate-700 hover:text-red-700 inline-flex items-center gap-1.5 transition-colors disabled:opacity-50"
                >
                    <XIcon className="w-3 h-3" />
                    Stop
                </button>
            </div>

            <div>
                <div className="flex items-baseline justify-between gap-2 mb-1.5">
                    <span className="text-[12px] text-slate-700 tabular-nums">
                        <AnimatedNumber value={imp.processed} /> of {imp.total.toLocaleString()} rows
                    </span>
                    <span className="text-[11.5px] text-slate-500 tabular-nums">
                        {eta ?? `${Math.floor(pct)}%`}
                    </span>
                </div>
                <div className="h-2 rounded-full bg-slate-100 overflow-hidden relative">
                    {imp.status === "queued" || imp.processed === 0 ? (
                        <div className="absolute inset-y-0 w-1/3 bg-sky-400 rounded-full progress-sweep" />
                    ) : (
                        <motion.div
                            className="h-full bg-sky-500 rounded-full"
                            initial={false}
                            animate={{ width: `${Math.max(2, pct)}%` }}
                            transition={{ duration: 0.5, ease: [0.22, 1, 0.36, 1] }}
                        />
                    )}
                </div>
            </div>

            <Counts imp={imp} />

            <div className="rounded-md border border-slate-200 bg-slate-50/40 px-3 py-2.5 flex items-start gap-2">
                <InfoIcon className="w-3.5 h-3.5 mt-px text-slate-400 shrink-0" />
                <p className="text-[11.5px] text-slate-600 leading-snug">
                    You can close this window. The import keeps running, survives a refresh, and your teammates see it
                    finish too. Reopen Import to check on it.
                </p>
            </div>
        </div>
    );
}

function Counts({ imp }: { imp: ContactImport }) {
    return (
        <div className="grid grid-cols-2 md:grid-cols-4 gap-2">
            <StatCard label="Imported" value={imp.imported} accent="emerald" icon={UserPlusIcon} />
            <StatCard label="Updated" value={imp.updated} accent="sky" icon={RefreshCwIcon} />
            <StatCard label="Skipped" value={imp.skipped} accent="slate" icon={CircleSlashIcon} />
            <StatCard label="Failed" value={imp.failed} accent={imp.failed > 0 ? "red" : "slate"} icon={XCircleIcon} />
        </div>
    );
}

function Finished({ imp, pinned }: { imp: ContactImport; pinned: string[] }) {
    const [query, setQuery] = useBrowseState(`contacts:imports:${imp.id}:failures:query`, "", browseText);
    const [downloading, setDownloading] = React.useState(false);

    async function download() {
        setDownloading(true);
        try {
            const blob = await downloadContactImportFailures(imp.id);
            downloadBlob(blob, imp.filename.replace(/\.[^.]+$/, "") + "-failed.csv");
        } catch (err) {
            toast.error(describeError(err, "The failed rows could not be downloaded."));
        } finally {
            setDownloading(false);
        }
    }

    const head =
        imp.status === "completed"
            ? imp.failed === 0
                ? { icon: CheckCircle2Icon, tone: "text-emerald-600", title: "Import complete" }
                : { icon: AlertTriangleIcon, tone: "text-amber-600", title: "Import finished with some rows left out" }
            : imp.status === "cancelled"
              ? { icon: CircleSlashIcon, tone: "text-slate-500", title: "Import stopped" }
              : { icon: XCircleIcon, tone: "text-red-600", title: "Import could not finish" };

    const q = query.trim().toLowerCase();
    const failures = (imp.failures ?? []).filter(
        (f) => q === "" || (f.email ?? "").toLowerCase().includes(q) || f.reason.toLowerCase().includes(q) || String(f.line) === q,
    );

    return (
        <div className="space-y-4">
            <div className="flex items-center gap-3">
                <head.icon className={`w-8 h-8 shrink-0 ${head.tone}`} />
                <div className="min-w-0 flex-1">
                    <p className="text-[13.5px] font-semibold text-slate-900">{head.title}</p>
                    <p className="text-[11.5px] text-slate-500 leading-snug mt-0.5">
                        {imp.filename} · {imp.processed.toLocaleString()} of {imp.total.toLocaleString()} rows
                        {imp.started_at && imp.finished_at && <> in {durationText(imp.started_at, imp.finished_at)}</>}.
                        {imp.segments_pinned && pinned.length > 0 && <> Pinned into {pinned.join(", ")}.</>}
                    </p>
                </div>
            </div>

            <Counts imp={imp} />

            {imp.status === "cancelled" && imp.processed < imp.total && (
                <Notice tone="slate" icon={CircleSlashIcon} title={`${(imp.total - imp.processed).toLocaleString()} rows were not imported`}>
                    Everything before the stop is in your contacts. Import the file again to add the rest; rows already here are
                    matched, not duplicated.
                </Notice>
            )}
            {imp.error && (
                <Notice tone="red" icon={XCircleIcon} title="Why it stopped">
                    {imp.error}
                </Notice>
            )}
            {imp.segments_pinned === false && (
                <Notice tone="amber" icon={AlertTriangleIcon} title="The contacts are in, but not in the segment">
                    The rows imported; the membership write did not. Select them in your contact list and use{" "}
                    <span className="font-medium">Segment</span> to add them, or run the import again.
                </Notice>
            )}
            {imp.notes.length > 0 &&
                imp.notes.map((n, i) => (
                    <Notice key={i} tone="amber" icon={InfoIcon} title="Note">
                        {n}
                    </Notice>
                ))}
            {imp.quality?.flagged && (
                <Notice tone="amber" icon={ShieldAlertIcon} title="This list looks low quality">
                    {imp.quality.summary} They are imported, but sending to them risks the reputation of every mailbox in this
                    workspace. Clean the list before launching a campaign with it.
                </Notice>
            )}

            {imp.failed > 0 && (
                <div className="rounded-md border border-slate-200 overflow-hidden">
                    <div className="px-3 min-h-9 py-1 border-b border-slate-200 bg-slate-50/60 flex flex-wrap items-center gap-2">
                        <XCircleIcon className="w-3 h-3 text-red-500" />
                        <span className="text-[11px] uppercase tracking-[0.14em] text-slate-500 font-medium">Failed rows</span>
                        <span className="text-[11px] text-slate-500 tabular-nums">
                            {(imp.failures?.length ?? 0) < imp.failed
                                ? `first ${(imp.failures?.length ?? 0).toLocaleString()} of ${imp.failed.toLocaleString()}`
                                : imp.failed.toLocaleString()}
                        </span>
                        <div className="ml-auto flex items-center gap-1.5">
                            {(imp.failures?.length ?? 0) > 8 && (
                                <SearchInput value={query} onChange={setQuery} placeholder="Filter…" className="w-36" />
                            )}
                            <button
                                type="button"
                                onClick={download}
                                disabled={downloading}
                                className="h-7 px-2 rounded-md border border-slate-200 bg-white text-[11.5px] text-slate-700 hover:text-slate-900 hover:border-slate-300 inline-flex items-center gap-1.5 transition-colors disabled:opacity-60"
                                title="Every failed row as uploaded, with the reason in the last column. Fix it and import it again."
                            >
                                {downloading ? <Loader2Icon className="w-3 h-3 animate-spin" /> : <DownloadIcon className="w-3 h-3" />}
                                Download to fix
                            </button>
                        </div>
                    </div>
                    <div className="max-h-56 overflow-y-auto">
                        <table className="w-full text-left">
                            <thead className="bg-white sticky top-0">
                                <tr className="border-b border-slate-100">
                                    <th className="px-3 py-1.5 text-[10px] font-medium text-slate-400 uppercase tracking-[0.14em] w-14">Line</th>
                                    <th className="hidden md:table-cell px-3 py-1.5 text-[10px] font-medium text-slate-400 uppercase tracking-[0.14em]">Email</th>
                                    <th className="px-3 py-1.5 text-[10px] font-medium text-slate-400 uppercase tracking-[0.14em]">Reason</th>
                                </tr>
                            </thead>
                            <tbody>
                                <AnimatePresence initial={false}>
                                    {failures.map((f) => (
                                        <motion.tr
                                            key={f.line}
                                            layout="position"
                                            initial={{ opacity: 0 }}
                                            animate={{ opacity: 1 }}
                                            exit={{ opacity: 0 }}
                                            className="border-b border-slate-100 last:border-b-0"
                                        >
                                            <td className="px-3 py-1.5 text-[11px] text-slate-500 font-mono">{f.line}</td>
                                            <td className="hidden md:table-cell px-3 py-1.5 text-[11.5px] text-slate-700 truncate max-w-[200px]">
                                                {f.email || <span className="text-slate-300">—</span>}
                                            </td>
                                            <td className="px-3 py-1.5 text-[11.5px] text-slate-700 leading-snug">{f.reason}</td>
                                        </motion.tr>
                                    ))}
                                </AnimatePresence>
                            </tbody>
                        </table>
                        {failures.length === 0 && <p className="px-3 py-4 text-center text-[11.5px] text-slate-400">No row matches.</p>}
                    </div>
                </div>
            )}
        </div>
    );
}

function Notice({
    tone,
    icon: Icon,
    title,
    children,
}: {
    tone: "amber" | "red" | "slate";
    icon: typeof InfoIcon;
    title: string;
    children: React.ReactNode;
}) {
    const t = {
        amber: { box: "border-amber-200 bg-amber-50", icon: "text-amber-600", title: "text-amber-900", body: "text-amber-800/90" },
        red: { box: "border-red-200 bg-red-50", icon: "text-red-600", title: "text-red-900", body: "text-red-800/90" },
        slate: { box: "border-slate-200 bg-slate-50", icon: "text-slate-500", title: "text-slate-900", body: "text-slate-600" },
    }[tone];
    return (
        <div className={`rounded-md border px-3 py-2.5 flex items-start gap-2 ${t.box}`}>
            <Icon className={`w-3.5 h-3.5 mt-px shrink-0 ${t.icon}`} />
            <div className="min-w-0">
                <p className={`text-[12.5px] font-medium ${t.title}`}>{title}</p>
                <p className={`text-[11.5px] leading-relaxed mt-0.5 ${t.body}`}>{children}</p>
            </div>
        </div>
    );
}

// estimate is the time left at the rate the import has kept so far, from the
// server's own clock, so every viewer reads the same figure.
function estimate(imp: ContactImport): string | null {
    if (imp.status !== "running" || !imp.started_at || imp.processed <= 0) return null;
    const elapsed = (new Date(imp.updated_at).getTime() - new Date(imp.started_at).getTime()) / 1000;
    if (!(elapsed > 1)) return null;
    const rate = imp.processed / elapsed;
    const left = (imp.total - imp.processed) / rate;
    const perSec = rate >= 10 ? `${Math.round(rate).toLocaleString()} rows/s` : `${rate.toFixed(1)} rows/s`;
    if (left < 1) return `${perSec} · finishing`;
    if (left < 60) return `${perSec} · about ${Math.ceil(left)} s left`;
    return `${perSec} · about ${Math.ceil(left / 60)} min left`;
}

function durationText(start: Date | string, end: Date | string): string {
    const ms = new Date(end).getTime() - new Date(start).getTime();
    if (Number.isNaN(ms) || ms < 0) return "a moment";
    if (ms < 1000) return `${ms} ms`;
    const sec = ms / 1000;
    if (sec < 60) return `${sec.toFixed(1)} s`;
    return `${(sec / 60).toFixed(1)} min`;
}
