// A report's public link: mint it, copy it the one time it is shown, rotate or revoke it.

import React from "react";
import { CheckIcon, CopyIcon, Link2Icon, Loader2Icon, RefreshCwIcon, ShieldOffIcon } from "lucide-react";
import toast from "react-hot-toast/headless";

import { Label } from "@/components/ui/field";
import { SelectMenu } from "@/components/ui/select-menu";
import { useConfirm } from "@/hooks/context/confirm";
import type { WriteGuard } from "@/hooks/usePermission";
import { useCreateReportLink, useRevokeReportLink } from "@/lib/api/hooks/app/reports";
import type ClientReport from "@/lib/api/models/app/reports/ClientReport";
import type { AppError } from "@/lib/api/client/normalizeError";
import buildError from "@/lib/helper/buildError";
import { cn } from "@/lib/utils";
import ReportEmbedSnippet from "./ReportEmbedSnippet";
import { formatDate, linkExpiry, linkState } from "./reportText";

const EXPIRY_OPTIONS = [
    { value: "7", label: "7 days" },
    { value: "30", label: "30 days" },
    { value: "90", label: "90 days" },
    { value: "180", label: "180 days" },
    { value: "365", label: "1 year" },
];

const STATE_PILL = {
    shared: { label: "Shared", cls: "bg-emerald-50 text-emerald-700 border-emerald-100" },
    expired: { label: "Expired", cls: "bg-amber-50 text-amber-700 border-amber-100" },
    off: { label: "Not shared", cls: "bg-slate-50 text-slate-500 border-slate-200" },
} as const;

export default function ReportSharePanel({ report, write }: { report: ClientReport; write: WriteGuard }) {
    const confirm = useConfirm();
    const mint = useCreateReportLink();
    const revoke = useRevokeReportLink();
    const [days, setDays] = React.useState("90");
    // The URL is returned only by the mint call, so it lives here until the page is left.
    const [minted, setMinted] = React.useState<{ url: string; hint: string } | null>(null);
    const [copied, setCopied] = React.useState(false);
    const [problem, setProblem] = React.useState<string | null>(null);

    const state = linkState(report);
    const pill = STATE_PILL[state];
    // A teammate rotating or revoking the link makes the one shown here stale.
    const shown = minted && report.link?.hint === minted.hint ? minted : null;

    async function create() {
        setProblem(null);
        try {
            const res = await mint.mutateAsync({ id: report.id, expiresInDays: Number(days) });
            setMinted({ url: res.url, hint: res.link.hint });
            setCopied(false);
        } catch (err) {
            const e = err as AppError;
            if (e.code === "report_links_unavailable" || e.code === "invalid_report_link") setProblem(e.message);
            else toast.error(buildError(e));
        }
    }

    function rotate() {
        confirm.show(
            "Make a new link? The current link stops working right away, including in emails already sent.",
            create,
            undefined,
            { title: "Rotate link", confirmLabel: "Rotate link" },
        );
    }

    function askRevoke() {
        confirm.show(
            "Turn off sharing? The link stops working, and so does every link already sent in scheduled emails.",
            async () => {
                try {
                    await revoke.mutateAsync(report.id);
                    setMinted(null);
                    toast.success("Sharing turned off");
                } catch (err) {
                    toast.error(buildError(err as AppError));
                }
            },
            undefined,
            { title: "Revoke link", confirmLabel: "Revoke link" },
        );
    }

    async function copy() {
        if (!shown) return;
        await navigator.clipboard.writeText(shown.url);
        setCopied(true);
        toast.success("Link copied");
    }

    return (
        <div className="flex flex-col gap-4 max-w-[640px]">
            <div className="flex items-start gap-3">
                <div className="size-8 rounded-md bg-slate-100 flex items-center justify-center shrink-0">
                    <Link2Icon className="w-3.5 h-3.5 text-slate-500" />
                </div>
                <div className="min-w-0 flex-1">
                    <div className="flex items-center gap-2">
                        <span className="text-[13px] font-medium text-slate-900">Share link</span>
                        <span className={cn("inline-flex items-center h-5 px-1.5 rounded border text-[10.5px] font-medium", pill.cls)}>{pill.label}</span>
                    </div>
                    <p className="text-[11.5px] text-slate-500 leading-relaxed">
                        Anyone with the link sees this report without signing in. It never shows contacts, mailboxes or settings beyond what the report includes.
                    </p>
                    {report.link && (
                        <p className="mt-1 text-[11.5px] text-slate-500">
                            Link starting <span className="font-mono text-slate-700">{report.link.hint}</span>, made {formatDate(report.link.created_at)}.{" "}
                            {linkExpiry(report)}.
                        </p>
                    )}
                </div>
            </div>

            {shown && (
                <div className="rounded-md border border-sky-200 bg-sky-50/60 p-3">
                    <Label className="text-sky-700">Your link</Label>
                    <div className="flex items-center gap-1.5">
                        <input
                            readOnly
                            value={shown.url}
                            onFocus={(e) => e.currentTarget.select()}
                            aria-label="Report link"
                            className="flex-1 min-w-0 h-7 px-2.5 rounded-md border border-sky-200 bg-white font-mono text-[12px] text-slate-800 outline-none focus:border-sky-400 focus:ring-2 focus:ring-sky-100"
                        />
                        <button
                            type="button"
                            onClick={() => void copy()}
                            className="h-7 px-2.5 rounded-md bg-sky-600 hover:bg-sky-700 text-white text-[12px] font-medium inline-flex items-center gap-1.5 transition-colors shrink-0"
                        >
                            {copied ? <CheckIcon className="w-3 h-3" /> : <CopyIcon className="w-3 h-3" />}
                            {copied ? "Copied" : "Copy"}
                        </button>
                    </div>
                    <p className="mt-1.5 text-[11px] text-sky-800/80 leading-relaxed">
                        Copy it now. The full link is not shown again; make a new one if you lose it.
                    </p>
                </div>
            )}

            <div className="flex flex-wrap items-end gap-2">
                <div>
                    <Label>Expires after</Label>
                    <SelectMenu value={days} onChange={setDays} options={EXPIRY_OPTIONS} minWidth={140} aria-label="Link expiry" disabled={!write.allowed} />
                </div>
                {state === "shared" ? (
                    <>
                        <button
                            type="button"
                            onClick={write.guard(rotate)}
                            disabled={mint.isPending}
                            className="h-7 px-2.5 rounded-md border border-slate-200 hover:border-slate-300 bg-white text-slate-700 text-[12px] font-medium inline-flex items-center gap-1.5 transition-colors disabled:opacity-60"
                        >
                            {mint.isPending ? <Loader2Icon className="w-3 h-3 animate-spin" /> : <RefreshCwIcon className="w-3 h-3" />}
                            Rotate link
                        </button>
                        <button
                            type="button"
                            onClick={write.guard(askRevoke)}
                            disabled={revoke.isPending}
                            className="h-7 px-2.5 rounded-md text-rose-600 hover:bg-rose-50 text-[12px] font-medium inline-flex items-center gap-1.5 transition-colors disabled:opacity-60"
                        >
                            <ShieldOffIcon className="w-3 h-3" />
                            Revoke
                        </button>
                    </>
                ) : (
                    <>
                        <button
                            type="button"
                            onClick={write.guard(() => void create())}
                            disabled={mint.isPending}
                            className="h-7 px-2.5 rounded-md bg-sky-600 hover:bg-sky-700 text-white text-[12px] font-medium inline-flex items-center gap-1.5 transition-colors disabled:opacity-60"
                        >
                            {mint.isPending ? <Loader2Icon className="w-3 h-3 animate-spin" /> : <Link2Icon className="w-3 h-3" />}
                            {state === "expired" ? "Make a new link" : "Create link"}
                        </button>
                        {state === "expired" && (
                            <button
                                type="button"
                                onClick={write.guard(askRevoke)}
                                disabled={revoke.isPending}
                                className="h-7 px-2.5 rounded-md text-slate-600 hover:bg-slate-100 text-[12px] font-medium transition-colors disabled:opacity-60"
                            >
                                Remove
                            </button>
                        )}
                    </>
                )}
            </div>
            {problem && <p className="text-[11.5px] text-red-600">{problem}</p>}
            {state === "shared" && !shown && (
                <p className="text-[11px] text-slate-400">The full link was shown once when it was made. Rotate it to get a new one to copy.</p>
            )}

            <div className="pt-2 border-t border-slate-200">
                <Label>Embed</Label>
                <ReportEmbedSnippet report={report} />
            </div>
        </div>
    );
}
