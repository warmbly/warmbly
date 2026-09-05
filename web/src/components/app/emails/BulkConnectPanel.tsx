// BulkConnectPanel — the CSV route into the connect-mailbox modal.
//
// Parses the file in the browser, shows what it found, then streams the rows
// through POST /emails/onboarding/smtp-imap/bulk in small batches so a
// 3,000 row file reports live progress instead of one request that times out.
// Every row ends as connected, skipped (already here) or failed with a reason,
// and the failed rows come back as a CSV with the columns the user uploaded
// plus an error column, so they can fix and re-upload just those. Re-uploading
// is safe: a mailbox that is already connected is skipped, never doubled.
//
// Rows past the workspace's allowance are refused by the server without
// dialling anything; the preview says so before the run starts and offers the
// request-more dialog.

import React from "react";
import { motion } from "framer-motion";
import { useQueryClient } from "@tanstack/react-query";
import toast from "react-hot-toast";
import {
    AlertTriangleIcon,
    CheckCircle2Icon,
    DownloadIcon,
    FileSpreadsheetIcon,
    Loader2Icon,
    RotateCcwIcon,
    UploadIcon,
    XIcon,
} from "lucide-react";
import { DitherMeter } from "@/components/ui/dither";
import addEmailsBulk, { BULK_CONNECT_BATCH } from "@/lib/api/client/app/emails/addEmailsBulk";
import useMailboxAllowance from "@/lib/api/hooks/app/emails/useMailboxAllowance";
import type { BulkConnectRow } from "@/lib/api/models/app/emails/BulkConnect";
import { downloadBlob } from "@/lib/api/client/app/contacts/exportContacts";
import { downloadTemplate, failedRowsCSV, parseBulkFile, type BulkRow } from "./bulkConnectCsv";
import type { AppError } from "@/lib/api/client/normalizeError";
import buildError from "@/lib/helper/buildError";
import { cn } from "@/lib/utils";

// ---- the panel ------------------------------------------------------------

type Step = "upload" | "preview" | "run" | "result";

export default function BulkConnectPanel({
    onDone,
    onAllowance,
}: {
    onDone: () => void;
    /** Opens the mailbox allowance dialog. */
    onAllowance: () => void;
}) {
    const qc = useQueryClient();
    const allowance = useMailboxAllowance();
    const [step, setStep] = React.useState<Step>("upload");
    const [filename, setFilename] = React.useState("");
    const [columns, setColumns] = React.useState<string[]>([]);
    const [rows, setRows] = React.useState<BulkRow[]>([]);
    const [parsing, setParsing] = React.useState(false);
    const [dragging, setDragging] = React.useState(false);
    // The run: which rows are still to go, and the ask to stop after the
    // batch in flight. A ref because the loop reads it between awaits.
    const cancelRef = React.useRef(false);
    const [cancelling, setCancelling] = React.useState(false);
    const [startedAt, setStartedAt] = React.useState<number>(0);
    const fileInput = React.useRef<HTMLInputElement>(null);

    const ready = rows.filter((r) => r.status === "ready" || r.status === "pending").length;
    const invalid = rows.filter((r) => r.status === "invalid").length;
    const remaining = allowance.data?.remaining ?? null;
    const full = remaining !== null && remaining <= 0;
    const overBy = remaining !== null && ready > remaining ? ready - remaining : 0;

    async function onFile(file: File) {
        setParsing(true);
        try {
            const parsed = await parseBulkFile(file);
            setFilename(file.name);
            setColumns(parsed.columns);
            setRows(parsed.rows);
            setStep("preview");
        } catch (err) {
            toast.error((err as Error).message);
        } finally {
            setParsing(false);
        }
    }

    // Sends every "ready" row in batches, updating the table as answers come
    // back. The list behind the modal refreshes after each batch so
    // teammates watching it see the mailboxes arrive.
    async function run() {
        if (ready === 0) return;
        cancelRef.current = false;
        setCancelling(false);
        setStartedAt(Date.now());
        setStep("run");
        setRows((prev) => prev.map((r) => (r.status === "ready" ? { ...r, status: "pending" } : r)));

        const queue = rows.map((r, i) => ({ r, i })).filter(({ r }) => r.status === "ready");
        for (let at = 0; at < queue.length; at += BULK_CONNECT_BATCH) {
            if (cancelRef.current) break;
            const batch = queue.slice(at, at + BULK_CONNECT_BATCH);
            let answers: BulkConnectRow[];
            try {
                const res = await addEmailsBulk(batch.map(({ r }) => r.account!));
                answers = res.data;
            } catch (err) {
                const msg = buildError(err as AppError);
                answers = batch.map((_, j) => ({ row: j, email: batch[j].r.account!.email, status: "failed", code: "request_failed", message: msg }));
            }
            setRows((prev) => {
                const next = [...prev];
                for (const a of answers) {
                    const target = batch[a.row];
                    if (!target) continue;
                    next[target.i] = { ...next[target.i], status: a.status, code: a.code, message: a.message };
                }
                return next;
            });
            qc.invalidateQueries({ queryKey: ["emails"] });
            qc.invalidateQueries({ queryKey: ["analytics", "accounts"] });
        }
        // Anything still pending was never sent because of a cancel.
        setRows((prev) => prev.map((r) => (r.status === "pending" ? { ...r, status: "ready" } : r)));
        setStep("result");
    }

    function retryFailed() {
        setRows((prev) =>
            prev.map((r) => (r.status === "failed" && r.account ? { ...r, status: "ready", code: undefined, message: undefined } : r)),
        );
        setStep("preview");
    }

    function reset() {
        setRows([]);
        setColumns([]);
        setFilename("");
        setStep("upload");
    }

    if (step === "upload") {
        return (
            <div className="p-4 space-y-3">
                <div
                    onDragOver={(e) => {
                        e.preventDefault();
                        setDragging(true);
                    }}
                    onDragLeave={() => setDragging(false)}
                    onDrop={(e) => {
                        e.preventDefault();
                        setDragging(false);
                        const f = e.dataTransfer.files?.[0];
                        if (f) void onFile(f);
                    }}
                    onClick={() => fileInput.current?.click()}
                    className={cn(
                        "rounded-md border border-dashed p-6 flex flex-col items-center justify-center text-center cursor-pointer transition-colors",
                        dragging ? "border-sky-400 bg-sky-50" : "border-slate-300 hover:border-slate-400 hover:bg-slate-50/60",
                    )}
                >
                    <input
                        ref={fileInput}
                        type="file"
                        accept=".csv,text/csv"
                        className="hidden"
                        onChange={(e) => {
                            const f = e.target.files?.[0];
                            if (f) void onFile(f);
                            e.target.value = "";
                        }}
                    />
                    {parsing ? (
                        <Loader2Icon className="w-5 h-5 text-slate-400 animate-spin" />
                    ) : (
                        <UploadIcon className="w-5 h-5 text-slate-400" />
                    )}
                    <p className="text-[13px] font-medium text-slate-900 mt-2">Drop a CSV here, or click to choose one</p>
                    <p className="text-[11.5px] text-slate-500 mt-0.5">
                        One mailbox per row. Any provider that speaks SMTP and IMAP.
                    </p>
                </div>

                <div className="rounded-md border border-slate-200 bg-slate-50/40 p-3">
                    <div className="flex items-center justify-between gap-2">
                        <span className="text-[10px] uppercase tracking-[0.14em] text-slate-500 font-medium">Columns</span>
                        <button
                            type="button"
                            onClick={downloadTemplate}
                            className="h-6 px-2 rounded text-[11px] text-slate-700 hover:text-slate-900 hover:bg-slate-100 inline-flex items-center gap-1 transition-colors"
                        >
                            <DownloadIcon className="w-3 h-3" />
                            Download template
                        </button>
                    </div>
                    <p className="text-[11.5px] text-slate-600 leading-relaxed mt-1.5">
                        <code className="font-mono text-[11px]">email</code>, <code className="font-mono text-[11px]">smtp_host</code> and{" "}
                        <code className="font-mono text-[11px]">imap_host</code> are required, plus a password. Everything else
                        has a default: ports 587 and 993, security from the port, the address as the login, the name from
                        the address. One <code className="font-mono text-[11px]">password</code> column covers both legs when
                        they share a login.
                    </p>
                </div>

                <AllowanceNote allowance={allowance.data} onAllowance={onAllowance} />
            </div>
        );
    }

    if (step === "preview") {
        const sample = rows.slice(0, 6);
        return (
            <div className="flex flex-col min-h-0">
                <div className="p-4 space-y-3">
                    <div className="flex items-center gap-2 min-w-0">
                        <FileSpreadsheetIcon className="w-4 h-4 text-slate-500 shrink-0" />
                        <span className="text-[12.5px] text-slate-900 font-medium truncate">{filename}</span>
                        <button
                            type="button"
                            onClick={reset}
                            className="ml-auto h-6 px-2 rounded text-[11px] text-slate-500 hover:text-slate-900 hover:bg-slate-100 inline-flex items-center gap-1 transition-colors shrink-0"
                        >
                            <XIcon className="w-3 h-3" />
                            Choose another file
                        </button>
                    </div>

                    <div className="grid grid-cols-3 gap-2">
                        <StatCard label="Rows" value={rows.length} accent="slate" />
                        <StatCard label="Ready" value={ready} accent="emerald" />
                        <StatCard label="Invalid" value={invalid} accent={invalid > 0 ? "red" : "slate"} />
                    </div>

                    {full ? (
                        <Banner tone="red" title="Your mailbox allowance is full">
                            Nothing in this file can be connected until the allowance is raised.{" "}
                            <button type="button" onClick={onAllowance} className="underline font-medium">
                                Request more
                            </button>{" "}
                            and come back to this file; it can be re-uploaded as it is.
                        </Banner>
                    ) : overBy > 0 ? (
                        <Banner tone="amber" title={`Room for ${remaining!.toLocaleString()} more mailboxes, ${ready.toLocaleString()} in this file`}>
                            The first {remaining!.toLocaleString()} rows will be connected and the remaining{" "}
                            {overBy.toLocaleString()} listed as failed so you can{" "}
                            <button type="button" onClick={onAllowance} className="underline font-medium">
                                request more
                            </button>{" "}
                            and re-upload only those.
                        </Banner>
                    ) : null}

                    {invalid > 0 && (
                        <Banner tone="amber" title={`${invalid.toLocaleString()} rows cannot be sent`}>
                            They are missing something the connect needs. They are left out of the run and included in
                            the failed rows download at the end, with the reason.
                        </Banner>
                    )}

                    <RowsTable rows={sample} more={rows.length - sample.length} />
                </div>

                <div className="px-4 py-2.5 border-t border-slate-200 bg-slate-50/60 flex items-center gap-2 min-w-0 sticky bottom-0">
                    <div className="text-[11px] text-slate-500 min-w-0 flex-1 truncate">
                        Every credential is verified against its server before it is saved.
                    </div>
                    <motion.button
                        type="button"
                        onClick={() => void run()}
                        disabled={ready === 0 || full}
                        whileTap={ready > 0 && !full ? { scale: 0.97 } : undefined}
                        className={cn(
                            "shrink-0 h-7 px-3 rounded-md text-[12px] font-medium inline-flex items-center gap-1.5 transition-colors",
                            "bg-slate-900 hover:bg-slate-800 text-white disabled:opacity-50 disabled:cursor-not-allowed",
                        )}
                    >
                        <UploadIcon className="w-3 h-3" />
                        Connect {ready.toLocaleString()} {ready === 1 ? "mailbox" : "mailboxes"}
                    </motion.button>
                </div>
            </div>
        );
    }

    const connected = rows.filter((r) => r.status === "connected").length;
    const skipped = rows.filter((r) => r.status === "skipped").length;
    const failed = rows.filter((r) => r.status === "failed").length;
    const pending = rows.filter((r) => r.status === "pending").length;
    const total = ready + connected + skipped + failed + pending;

    if (step === "run") {
        const done = connected + skipped + failed;
        const frac = total > 0 ? done / total : 0;
        return (
            <div className="p-4 space-y-3">
                <div className="flex items-center gap-3">
                    <Loader2Icon className="w-5 h-5 text-sky-600 animate-spin shrink-0" />
                    <div className="min-w-0 flex-1">
                        <p className="text-[13.5px] text-slate-900 font-semibold">
                            Connecting {total.toLocaleString()} {total === 1 ? "mailbox" : "mailboxes"}
                        </p>
                        <p className="text-[11.5px] text-slate-500 leading-snug mt-0.5">
                            Each one is verified against its server, so this takes a few seconds per mailbox. You can keep
                            this open in the background; the list fills in live.
                        </p>
                    </div>
                </div>
                <div>
                    <div className="flex items-baseline justify-between gap-2 mb-1">
                        <span className="text-[12px] text-slate-700 font-medium">Progress</span>
                        <span className="text-[11.5px] font-mono tabular-nums text-slate-700">
                            {done.toLocaleString()}
                            <span className="text-slate-400"> / {total.toLocaleString()}</span>
                        </span>
                    </div>
                    <DitherMeter frac={frac} tone="sky" height={6} />
                </div>
                <div className="grid grid-cols-3 gap-2">
                    <StatCard label="Connected" value={connected} accent="emerald" />
                    <StatCard label="Skipped" value={skipped} accent="slate" />
                    <StatCard label="Failed" value={failed} accent={failed > 0 ? "red" : "slate"} />
                </div>
                <div className="flex items-center justify-end">
                    <button
                        type="button"
                        disabled={cancelling}
                        onClick={() => {
                            cancelRef.current = true;
                            setCancelling(true);
                        }}
                        className="h-7 px-3 rounded-md border border-slate-200 text-[12px] text-slate-700 hover:bg-slate-50 inline-flex items-center gap-1.5 transition-colors disabled:opacity-50"
                    >
                        <XIcon className="w-3 h-3" />
                        {cancelling ? "Stopping after this batch…" : "Stop"}
                    </button>
                </div>
            </div>
        );
    }

    // result
    const failedRows = rows.filter((r) => r.status === "failed" || r.status === "invalid");
    const notSent = rows.filter((r) => r.status === "ready").length;
    const canRetry = rows.some((r) => r.status === "failed" && r.account);
    const elapsed = startedAt ? Date.now() - startedAt : 0;

    function downloadFailed() {
        if (failedRows.length === 0) return;
        const csv = failedRowsCSV(failedRows, columns);
        downloadBlob(
            new Blob(["﻿" + csv], { type: "text/csv;charset=utf-8" }),
            filename.replace(/\.[^.]+$/, "") + "-failed.csv",
        );
    }

    return (
        <div className="flex flex-col min-h-0">
            <div className="p-4 space-y-3">
                <div className="flex items-center gap-3">
                    {failedRows.length === 0 && notSent === 0 ? (
                        <CheckCircle2Icon className="w-7 h-7 text-emerald-600 shrink-0" />
                    ) : (
                        <AlertTriangleIcon className="w-7 h-7 text-amber-600 shrink-0" />
                    )}
                    <div className="min-w-0 flex-1">
                        <p className="text-[13.5px] text-slate-900 font-semibold">
                            {notSent > 0
                                ? "Stopped early"
                                : failedRows.length === 0
                                  ? "All mailboxes connected"
                                  : "Finished with some failures"}
                        </p>
                        <p className="text-[11.5px] text-slate-500 leading-snug mt-0.5">
                            {connected.toLocaleString()} connected
                            {skipped > 0 ? `, ${skipped.toLocaleString()} already here` : ""}
                            {failedRows.length > 0 ? `, ${failedRows.length.toLocaleString()} did not connect` : ""}
                            {notSent > 0 ? `, ${notSent.toLocaleString()} not attempted` : ""}
                            {elapsed > 0 ? ` in ${durationText(elapsed)}` : ""}.
                        </p>
                    </div>
                </div>

                <div className="grid grid-cols-3 gap-2">
                    <StatCard label="Connected" value={connected} accent="emerald" />
                    <StatCard label="Skipped" value={skipped} accent="slate" />
                    <StatCard label="Failed" value={failedRows.length} accent={failedRows.length > 0 ? "red" : "slate"} />
                </div>

                {failedRows.some((r) => r.code === "mailbox_allowance_reached") && (
                    <Banner tone="amber" title="Some rows were past your mailbox allowance">
                        They were not tried.{" "}
                        <button type="button" onClick={onAllowance} className="underline font-medium">
                            Request more
                        </button>
                        , then re-upload the failed rows download; everything already connected is skipped.
                    </Banner>
                )}

                {failedRows.length > 0 && (
                    <div className="rounded-md border border-slate-200 overflow-hidden">
                        <div className="px-3 h-9 border-b border-slate-200 bg-slate-50/60 flex items-center gap-2">
                            <span className="text-[11px] uppercase tracking-[0.14em] text-slate-500 font-medium">
                                Did not connect
                            </span>
                            <span className="text-[11px] text-slate-500">{failedRows.length.toLocaleString()}</span>
                            <button
                                type="button"
                                onClick={downloadFailed}
                                title="The rows as you uploaded them, including passwords, plus an error column"
                                className="ml-auto h-6 px-2 rounded text-[11px] text-slate-700 hover:text-slate-900 hover:bg-slate-100 inline-flex items-center gap-1 transition-colors"
                            >
                                <DownloadIcon className="w-3 h-3" />
                                Download failed rows
                            </button>
                        </div>
                        <div className="max-h-52 overflow-y-auto">
                            <table className="w-full text-left">
                                <thead className="bg-white sticky top-0">
                                    <tr className="border-b border-slate-100">
                                        <th className="px-3 py-1.5 text-[10px] font-medium text-slate-400 uppercase tracking-[0.14em] w-12">Line</th>
                                        <th className="px-3 py-1.5 text-[10px] font-medium text-slate-400 uppercase tracking-[0.14em]">Email</th>
                                        <th className="px-3 py-1.5 text-[10px] font-medium text-slate-400 uppercase tracking-[0.14em]">Reason</th>
                                    </tr>
                                </thead>
                                <tbody>
                                    {failedRows.slice(0, 200).map((r) => (
                                        <tr key={r.line} className="border-b border-slate-100 last:border-b-0">
                                            <td className="px-3 py-1.5 text-[11px] text-slate-500 font-mono">{r.line}</td>
                                            <td className="px-3 py-1.5 text-[11.5px] text-slate-700 truncate max-w-[200px]">
                                                {r.raw.email || <span className="text-slate-300">—</span>}
                                            </td>
                                            <td className="px-3 py-1.5 text-[11.5px] text-slate-700 leading-snug">
                                                {r.problem ?? r.message ?? r.code}
                                            </td>
                                        </tr>
                                    ))}
                                </tbody>
                            </table>
                        </div>
                    </div>
                )}
            </div>

            <div className="px-4 py-2.5 border-t border-slate-200 bg-slate-50/60 flex items-center gap-2 min-w-0 sticky bottom-0">
                <div className="text-[11px] text-slate-500 min-w-0 flex-1 truncate">
                    {failedRows.length > 0
                        ? "Fix the failed rows in the download and upload that file; connected ones are skipped."
                        : "Warmup and sync start on every new mailbox right away."}
                </div>
                {(canRetry || notSent > 0) && (
                    <button
                        type="button"
                        onClick={retryFailed}
                        className="shrink-0 h-7 px-3 rounded-md border border-slate-200 text-[12px] text-slate-700 hover:bg-slate-50 inline-flex items-center gap-1.5 transition-colors"
                    >
                        <RotateCcwIcon className="w-3 h-3" />
                        {notSent > 0 ? "Continue" : "Retry failed"}
                    </button>
                )}
                <button
                    type="button"
                    onClick={onDone}
                    className="shrink-0 h-7 px-3 rounded-md bg-slate-900 hover:bg-slate-800 text-white text-[12px] font-medium inline-flex items-center gap-1.5 transition-colors"
                >
                    <CheckCircle2Icon className="w-3 h-3" />
                    Done
                </button>
            </div>
        </div>
    );
}

// ---- pieces ---------------------------------------------------------------

function AllowanceNote({
    allowance,
    onAllowance,
}: {
    allowance: ReturnType<typeof useMailboxAllowance>["data"];
    onAllowance: () => void;
}) {
    if (!allowance || allowance.allowance == null) return null;
    const remaining = allowance.remaining ?? 0;
    return (
        <p className="text-[11.5px] text-slate-500 leading-relaxed">
            This workspace holds {allowance.used.toLocaleString()} of {allowance.allowance.toLocaleString()} mailboxes,
            so {remaining.toLocaleString()} more fit right now.{" "}
            <button type="button" onClick={onAllowance} className="underline text-slate-700 hover:text-slate-900">
                {remaining === 0 ? "Request more before uploading" : "Need more?"}
            </button>
        </p>
    );
}

function RowsTable({ rows, more }: { rows: BulkRow[]; more: number }) {
    return (
        <div className="rounded-md border border-slate-200 overflow-hidden">
            <table className="w-full text-left">
                <thead className="bg-slate-50/60">
                    <tr className="border-b border-slate-200">
                        <th className="px-3 py-1.5 text-[10px] font-medium text-slate-400 uppercase tracking-[0.14em] w-12">Line</th>
                        <th className="px-3 py-1.5 text-[10px] font-medium text-slate-400 uppercase tracking-[0.14em]">Email</th>
                        <th className="hidden md:table-cell px-3 py-1.5 text-[10px] font-medium text-slate-400 uppercase tracking-[0.14em]">SMTP</th>
                        <th className="hidden md:table-cell px-3 py-1.5 text-[10px] font-medium text-slate-400 uppercase tracking-[0.14em]">IMAP</th>
                        <th className="px-3 py-1.5 text-[10px] font-medium text-slate-400 uppercase tracking-[0.14em]">Status</th>
                    </tr>
                </thead>
                <tbody>
                    {rows.map((r) => (
                        <tr key={r.line} className="border-b border-slate-100 last:border-b-0">
                            <td className="px-3 py-1.5 text-[11px] text-slate-500 font-mono">{r.line}</td>
                            <td className="px-3 py-1.5 text-[11.5px] text-slate-800 truncate max-w-[180px]">{r.raw.email}</td>
                            <td className="hidden md:table-cell px-3 py-1.5 text-[11.5px] text-slate-600 truncate max-w-[160px]">
                                {r.account ? `${r.account.smtp.host}:${r.account.smtp.port}` : "—"}
                            </td>
                            <td className="hidden md:table-cell px-3 py-1.5 text-[11.5px] text-slate-600 truncate max-w-[160px]">
                                {r.account ? `${r.account.imap.host}:${r.account.imap.port}` : "—"}
                            </td>
                            <td className="px-3 py-1.5 text-[11.5px]">
                                {r.status === "invalid" ? (
                                    <span className="text-red-600" title={r.problem}>
                                        {r.problem}
                                    </span>
                                ) : (
                                    <span className="text-emerald-600">Ready</span>
                                )}
                            </td>
                        </tr>
                    ))}
                </tbody>
            </table>
            {more > 0 && (
                <div className="px-3 py-1.5 text-[11px] text-slate-400 border-t border-slate-100 bg-slate-50/40">
                    and {more.toLocaleString()} more
                </div>
            )}
        </div>
    );
}

function Banner({ tone, title, children }: { tone: "amber" | "red"; title: string; children: React.ReactNode }) {
    const cls =
        tone === "red"
            ? "border-red-200 bg-red-50 text-red-900 [&_p]:text-red-800/90"
            : "border-amber-200 bg-amber-50 text-amber-900 [&_p]:text-amber-800/90";
    return (
        <div className={cn("rounded-md border px-3 py-2.5 flex items-start gap-2", cls)}>
            <AlertTriangleIcon className="w-3.5 h-3.5 mt-px shrink-0 opacity-80" />
            <div className="min-w-0">
                <p className="text-[12.5px] font-medium !text-current">{title}</p>
                <p className="text-[11.5px] leading-relaxed mt-0.5">{children}</p>
            </div>
        </div>
    );
}

function StatCard({ label, value, accent }: { label: string; value: number; accent: "emerald" | "slate" | "red" }) {
    const ring = {
        emerald: "ring-emerald-200 bg-emerald-50 text-emerald-700",
        slate: "ring-slate-200 bg-slate-50 text-slate-700",
        red: "ring-red-200 bg-red-50 text-red-700",
    }[accent];
    return (
        <div className={`rounded-md ring-1 p-2.5 ${ring}`}>
            <div className="text-[10px] uppercase tracking-[0.14em] font-medium opacity-75">{label}</div>
            <div className="text-[18px] font-semibold tabular-nums mt-0.5">{value.toLocaleString()}</div>
        </div>
    );
}

function durationText(ms: number): string {
    if (ms < 1000) return `${ms} ms`;
    const sec = ms / 1000;
    if (sec < 60) return `${sec.toFixed(0)} s`;
    return `${(sec / 60).toFixed(1)} min`;
}
