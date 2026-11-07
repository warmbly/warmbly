import { useEffect, useState } from "react";
import toast from "react-hot-toast/headless";
import { DownloadIcon, Loader2Icon } from "lucide-react";
import { Checkbox } from "@/components/ui/checkbox";
import type { AppError } from "@/lib/api/client/normalizeError";
import { downloadBlob } from "@/lib/api/client/app/contacts/exportContacts";
import { exportLeads } from "@/lib/api/client/app/leads/leads";
import type { LeadExportFormat, LeadExportRows, LeadFinderConfig, LeadSelection } from "@/lib/api/models/app/leads/Lead";
import buildError from "@/lib/helper/buildError";
import { cn } from "@/lib/utils";
import LeadDialog from "./LeadDialog";

const FORMATS: { id: LeadExportFormat; label: string; hint: string }[] = [
    { id: "csv", label: "CSV", hint: "Any spreadsheet or CRM" },
    { id: "xlsx", label: "Excel", hint: "Frozen header, numbers as numbers" },
    { id: "json", label: "JSON", hint: "For scripts and tools" },
];

const ROWS: { id: LeadExportRows; label: string; hint: string }[] = [
    { id: "leads", label: "One row per business", hint: "Its own address, with the people in one column" },
    { id: "people", label: "One row per person", hint: "Name, title and address, then the business" },
];

export default function ExportDialog({
    open,
    onClose,
    selection,
    count,
    config,
}: {
    open: boolean;
    onClose: () => void;
    selection: LeadSelection;
    count: number;
    config: LeadFinderConfig;
}) {
    const [format, setFormat] = useState<LeadExportFormat>("csv");
    const [rows, setRows] = useState<LeadExportRows>("leads");
    const [columns, setColumns] = useState<string[]>(config.default_export_columns);
    const [busy, setBusy] = useState(false);
    useEffect(() => {
        if (open) setColumns(config.default_export_columns);
    }, [open, config.default_export_columns]);

    // The people column only exists in per-business rows.
    const sent = rows === "people" ? columns.filter((k) => k !== "people") : columns;
    const toggle = (k: string) => setColumns((c) => (c.includes(k) ? c.filter((x) => x !== k) : [...config.export_columns.map((x) => x.key).filter((x) => c.includes(x) || x === k)]));

    const run = async () => {
        setBusy(true);
        try {
            const blob = await exportLeads({ ...selection, format, columns: sent, rows });
            downloadBlob(blob, `${rows === "people" ? "lead-people" : "leads"}-${new Date().toISOString().slice(0, 10)}.${format}`);
            toast.success(`Exported ${count.toLocaleString()} lead${count === 1 ? "" : "s"}`);
            onClose();
        } catch (e) {
            toast.error(buildError(e as AppError));
        } finally {
            setBusy(false);
        }
    };

    return (
        <LeadDialog
            open={open}
            onClose={onClose}
            icon={<DownloadIcon className="size-3" />}
            eyebrow="Export"
            title={`${count.toLocaleString()} lead${count === 1 ? "" : "s"}`}
            width={560}
            footer={
                <>
                    <span className="mr-auto text-[11px] text-slate-400">
                        {rows === "people" ? `${config.person_columns.length} person columns + ${sent.length}` : `${sent.length} columns`}
                    </span>
                    <button type="button" onClick={onClose} className="h-7 px-3 rounded-md text-[12px] text-slate-600 hover:text-slate-900">
                        Cancel
                    </button>
                    <button
                        type="button"
                        onClick={run}
                        disabled={busy || sent.length === 0}
                        className="h-7 px-3 rounded-md bg-sky-600 text-white text-[12px] font-medium inline-flex items-center gap-1.5 hover:bg-sky-700 disabled:opacity-60"
                    >
                        {busy ? <Loader2Icon className="size-3.5 animate-spin" /> : <DownloadIcon className="size-3.5" />}
                        Download
                    </button>
                </>
            }
        >
            <div className="px-4 py-4 space-y-4">
                <div className="grid grid-cols-2 gap-2">
                    {ROWS.map((r) => (
                        <button
                            key={r.id}
                            type="button"
                            onClick={() => setRows(r.id)}
                            aria-pressed={rows === r.id}
                            className={cn(
                                "rounded-md border px-3 py-2 text-left transition-colors",
                                rows === r.id ? "border-sky-300 bg-sky-50" : "border-slate-200 hover:border-slate-300",
                            )}
                        >
                            <div className={cn("text-[12.5px] font-medium", rows === r.id ? "text-sky-800" : "text-slate-900")}>{r.label}</div>
                            <div className="text-[11px] text-slate-500">{r.hint}</div>
                        </button>
                    ))}
                </div>
                <div className="grid grid-cols-3 gap-2">
                    {FORMATS.map((f) => (
                        <button
                            key={f.id}
                            type="button"
                            onClick={() => setFormat(f.id)}
                            aria-pressed={format === f.id}
                            className={cn(
                                "rounded-md border px-3 py-2 text-left transition-colors",
                                format === f.id ? "border-sky-300 bg-sky-50" : "border-slate-200 hover:border-slate-300",
                            )}
                        >
                            <div className={cn("text-[12.5px] font-medium", format === f.id ? "text-sky-800" : "text-slate-900")}>{f.label}</div>
                            <div className="text-[11px] text-slate-500">{f.hint}</div>
                        </button>
                    ))}
                </div>
                <div>
                    <div className="mb-1.5 flex items-center">
                        <span className="text-[10px] font-medium uppercase tracking-[0.14em] text-slate-400">{rows === "people" ? "Business columns" : "Columns"}</span>
                        <button type="button" onClick={() => setColumns(config.export_columns.map((c) => c.key))} className="ml-auto text-[11px] text-sky-700 hover:underline">
                            All
                        </button>
                        <span className="mx-1.5 text-slate-300">·</span>
                        <button type="button" onClick={() => setColumns(config.default_export_columns)} className="text-[11px] text-sky-700 hover:underline">
                            Default
                        </button>
                    </div>
                    <div className="grid grid-cols-2 sm:grid-cols-3 gap-x-3 gap-y-1">
                        {config.export_columns.filter((c) => rows === "leads" || c.key !== "people").map((c) => (
                            <label key={c.key} className="flex items-center gap-2 py-0.5 text-[12px] text-slate-700 cursor-pointer">
                                <Checkbox tone="slate" checked={columns.includes(c.key)} onChange={() => toggle(c.key)} />
                                {c.label}
                            </label>
                        ))}
                    </div>
                    {rows === "people" && (
                        <p className="mt-2 text-[11px] text-slate-400">
                            Each row starts with {config.person_columns.map((c) => c.label.toLowerCase()).slice(0, 4).join(", ")} and more. Businesses with nobody found are left out.
                        </p>
                    )}
                </div>
            </div>
        </LeadDialog>
    );
}
