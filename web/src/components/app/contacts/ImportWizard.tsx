// ImportWizard — multi-step modal for importing contacts from
// CSV / XLSX. Driven by the two-step backend API:
//
//   1. Upload  — drag & drop or file picker. We do NOT parse the file
//      on the client; the server is the source of truth, and re-doing
//      the work in JS just to "preview" introduces format-handling
//      drift. We do show a friendly waiting state.
//   2. Map     — the server returns columns + suggestions; the user
//      reviews and tweaks. Email is required and auto-mapped if
//      anything looks like an email column.
//   3. Options — dedup strategy, default categories to apply, default
//      campaigns, subscription default. These are global to the
//      import; per-row overrides happen post-import via bulk edit.
//   4. Result  — summary counts + per-row errors. Errors can be
//      downloaded as a CSV the user can fix and re-import.
//
// The same file is re-uploaded on commit so we don't need session
// state. The user can navigate back through steps without losing
// their mapping work.

import React from "react";
import { AnimatePresence, motion } from "framer-motion";
import {
    AlertTriangleIcon,
    ArrowLeftIcon,
    ArrowRightIcon,
    CheckCircle2Icon,
    DownloadIcon,
    FileSpreadsheetIcon,
    Loader2Icon,
    UploadCloudIcon,
    XIcon,
} from "lucide-react";
import toast from "react-hot-toast";
import { useQueryClient } from "@tanstack/react-query";

import {
    importPreviewContacts,
    importCommitContacts,
    type ImportColumnMapping,
    type ImportDedupStrategy,
    type ImportPreview,
    type ImportResult,
} from "@/lib/api/client/app/contacts/importContacts";
import {
    PopoverMenu,
    PopoverMenuContent,
    PopoverMenuItem,
    PopoverMenuLabel,
    PopoverMenuTrigger,
    SelectButton,
} from "@/components/ui/popover-menu";
import { Label, TextInput } from "@/components/ui/field";
import CategoryPicker from "./CategoryPicker";
import { downloadBlob } from "@/lib/api/client/app/contacts/exportContacts";
import { DEDUP_OPTIONS, STANDARD_TARGETS, describeError } from "./importShared";

interface Props {
    open: boolean;
    onClose: () => void;
    // When set (the campaign Leads tab), imported contacts are attached to this
    // campaign and the wizard shows a read-only "Adding to …" indicator.
    lockedCampaign?: { id: string; name: string };
}

type Step = "upload" | "map" | "options" | "result";

export default function ImportWizard({ open, onClose, lockedCampaign }: Props) {
    const [step, setStep] = React.useState<Step>("upload");
    const [file, setFile] = React.useState<File | null>(null);
    const [preview, setPreview] = React.useState<ImportPreview | null>(null);
    const [mapping, setMapping] = React.useState<ImportColumnMapping[]>([]);
    const [hasHeader, setHasHeader] = React.useState<boolean>(true);
    const [dedup, setDedup] = React.useState<ImportDedupStrategy>("skip");
    const [categoryIds, setCategoryIds] = React.useState<string[]>([]);
    const [previewBusy, setPreviewBusy] = React.useState<boolean>(false);
    const [commitBusy, setCommitBusy] = React.useState<boolean>(false);
    const [result, setResult] = React.useState<ImportResult | null>(null);
    const queryClient = useQueryClient();

    function reset() {
        setStep("upload");
        setFile(null);
        setPreview(null);
        setMapping([]);
        setHasHeader(true);
        setDedup("skip");
        setCategoryIds([]);
        setResult(null);
    }

    React.useEffect(() => {
        if (!open) reset();
    }, [open]);

    async function onFileChosen(f: File) {
        setFile(f);
        setPreviewBusy(true);
        try {
            const p = await importPreviewContacts(f);
            setPreview(p);
            setMapping(p.suggested_mapping);
            setHasHeader(p.has_header);
            setStep("map");
        } catch (err) {
            const msg = describeError(err, "Failed to read file.");
            toast.error(msg);
            setFile(null);
        } finally {
            setPreviewBusy(false);
        }
    }

    async function commit() {
        if (!file || !preview) return;
        setCommitBusy(true);
        try {
            const res = await importCommitContacts(file, {
                mapping,
                dedup,
                has_header: hasHeader,
                category_ids: categoryIds.length > 0 ? categoryIds : undefined,
                campaign_ids: lockedCampaign ? [lockedCampaign.id] : undefined,
            });
            setResult(res);
            setStep("result");
            await queryClient.invalidateQueries({ queryKey: ["contacts"] });
            if (res.failed === 0) {
                toast.success(`Imported ${res.imported} · updated ${res.updated} · skipped ${res.skipped}`);
            } else {
                toast(`Done with ${res.failed} errors`, { icon: "⚠️" });
            }
        } catch (err) {
            toast.error(describeError(err, "Import failed."));
        } finally {
            setCommitBusy(false);
        }
    }

    function emailIsMapped(): boolean {
        return mapping.some((m) => m.target === "email");
    }

    return (
        <AnimatePresence>
            {open && (
                <motion.div
                    key="overlay"
                    initial={{ opacity: 0 }}
                    animate={{ opacity: 1 }}
                    exit={{ opacity: 0 }}
                    transition={{ duration: 0.15 }}
                    onClick={onClose}
                    className="fixed inset-0 z-[120] flex items-center justify-center bg-slate-900/30 backdrop-blur-[2px] px-4"
                >
                    <motion.div
                        key="card"
                        initial={{ y: 8, opacity: 0 }}
                        animate={{ y: 0, opacity: 1 }}
                        exit={{ y: 8, opacity: 0 }}
                        transition={{ duration: 0.18 }}
                        onClick={(e) => e.stopPropagation()}
                        className="w-full max-w-[760px] rounded-lg bg-white border border-slate-200 shadow-[0_24px_48px_-12px_rgba(15,23,42,0.18),0_8px_16px_-8px_rgba(15,23,42,0.1)] overflow-hidden flex flex-col max-h-[90dvh]"
                    >
                        <header className="h-12 px-4 border-b border-slate-200 flex items-center gap-2.5 shrink-0">
                            <div className="size-5 rounded bg-slate-100 text-slate-600 flex items-center justify-center">
                                <UploadCloudIcon className="w-3 h-3" />
                            </div>
                            <span className="text-[10px] uppercase tracking-[0.14em] text-slate-400 font-medium">
                                Import
                            </span>
                            <div className="h-4 w-px bg-slate-200" />
                            <span className="text-[12.5px] text-slate-900 font-medium">
                                Contacts
                            </span>
                            {lockedCampaign && (
                                <span className="hidden sm:inline-flex items-center h-5 px-1.5 rounded bg-sky-50 text-sky-700 text-[10px] font-medium max-w-[180px] truncate">
                                    → {lockedCampaign.name}
                                </span>
                            )}
                            <StepDots step={step} />
                            <button
                                type="button"
                                onClick={onClose}
                                aria-label="Close"
                                className="ml-auto size-7 rounded-md text-slate-500 hover:text-slate-900 hover:bg-slate-100 inline-flex items-center justify-center transition-colors"
                            >
                                <XIcon className="w-3.5 h-3.5" />
                            </button>
                        </header>

                        <div className="flex-1 min-h-0 overflow-y-auto px-5 py-4">
                            {step === "upload" && (
                                <UploadStep
                                    file={file}
                                    onFile={onFileChosen}
                                    busy={previewBusy}
                                />
                            )}
                            {step === "map" && preview && (
                                <MapStep
                                    preview={preview}
                                    mapping={mapping}
                                    setMapping={setMapping}
                                    hasHeader={hasHeader}
                                    setHasHeader={setHasHeader}
                                />
                            )}
                            {step === "options" && (
                                <OptionsStep
                                    dedup={dedup}
                                    setDedup={setDedup}
                                    categoryIds={categoryIds}
                                    setCategoryIds={setCategoryIds}
                                />
                            )}
                            {step === "result" && result && (
                                <ResultStep result={result} filename={file?.name ?? "import"} />
                            )}
                        </div>

                        <footer className="min-h-12 py-1.5 md:py-0 px-3 border-t border-slate-200 flex flex-wrap items-center gap-1.5 shrink-0 bg-slate-50/30">
                            {step === "upload" && (
                                <>
                                    <span className="text-[11px] text-slate-400">
                                        CSV, TSV, or XLSX. Max 50 MB · 50,000 rows.
                                    </span>
                                    <button
                                        type="button"
                                        onClick={onClose}
                                        className="ml-auto h-7 px-2.5 rounded-md text-[12px] text-slate-700 hover:text-slate-900 hover:bg-slate-100 transition-colors"
                                    >
                                        Cancel
                                    </button>
                                </>
                            )}
                            {step === "map" && (
                                <>
                                    <button
                                        type="button"
                                        onClick={() => setStep("upload")}
                                        className="h-7 px-2.5 rounded-md text-[12px] text-slate-600 hover:text-slate-900 hover:bg-slate-100 inline-flex items-center gap-1.5 transition-colors"
                                    >
                                        <ArrowLeftIcon className="w-3 h-3" />
                                        Re-upload
                                    </button>
                                    {!emailIsMapped() && (
                                        <span className="text-[11px] text-amber-700 inline-flex items-center gap-1">
                                            <AlertTriangleIcon className="w-3 h-3" />
                                            Map a column to Email
                                        </span>
                                    )}
                                    <button
                                        type="button"
                                        onClick={() => setStep("options")}
                                        disabled={!emailIsMapped()}
                                        className="ml-auto h-7 px-3 rounded-md bg-slate-900 hover:bg-slate-800 text-white text-[12px] font-medium inline-flex items-center gap-1.5 transition-colors disabled:opacity-50"
                                    >
                                        Continue
                                        <ArrowRightIcon className="w-3 h-3" />
                                    </button>
                                </>
                            )}
                            {step === "options" && (
                                <>
                                    <button
                                        type="button"
                                        onClick={() => setStep("map")}
                                        className="h-7 px-2.5 rounded-md text-[12px] text-slate-600 hover:text-slate-900 hover:bg-slate-100 inline-flex items-center gap-1.5 transition-colors"
                                    >
                                        <ArrowLeftIcon className="w-3 h-3" />
                                        Back
                                    </button>
                                    <button
                                        type="button"
                                        onClick={commit}
                                        disabled={commitBusy}
                                        className="ml-auto h-7 px-3 rounded-md bg-slate-900 hover:bg-slate-800 text-white text-[12px] font-medium inline-flex items-center gap-1.5 transition-colors disabled:opacity-60"
                                    >
                                        {commitBusy ? (
                                            <Loader2Icon className="w-3 h-3 animate-spin" />
                                        ) : (
                                            <UploadCloudIcon className="w-3 h-3" />
                                        )}
                                        Import {preview ? preview.total_rows.toLocaleString() : ""} rows
                                    </button>
                                </>
                            )}
                            {step === "result" && (
                                <>
                                    <button
                                        type="button"
                                        onClick={() => {
                                            reset();
                                        }}
                                        className="h-7 px-2.5 rounded-md text-[12px] text-slate-600 hover:text-slate-900 hover:bg-slate-100 transition-colors"
                                    >
                                        Import another file
                                    </button>
                                    <button
                                        type="button"
                                        onClick={onClose}
                                        className="ml-auto h-7 px-3 rounded-md bg-slate-900 hover:bg-slate-800 text-white text-[12px] font-medium transition-colors"
                                    >
                                        Done
                                    </button>
                                </>
                            )}
                        </footer>
                    </motion.div>
                </motion.div>
            )}
        </AnimatePresence>
    );
}

function StepDots({ step }: { step: Step }) {
    const order: Step[] = ["upload", "map", "options", "result"];
    const i = order.indexOf(step);
    return (
        <div className="hidden sm:flex items-center gap-1 ml-2">
            {order.map((_, idx) => (
                <span
                    key={idx}
                    className={`h-1 w-5 rounded-full transition-colors ${
                        idx <= i ? "bg-slate-900" : "bg-slate-200"
                    }`}
                />
            ))}
        </div>
    );
}

// ----- Upload step ------------------------------------------------

function UploadStep({
    file,
    onFile,
    busy,
}: {
    file: File | null;
    onFile: (f: File) => void;
    busy: boolean;
}) {
    const inputRef = React.useRef<HTMLInputElement>(null);
    const [dragging, setDragging] = React.useState(false);

    function pickFile() {
        inputRef.current?.click();
    }

    function onDrop(e: React.DragEvent<HTMLDivElement>) {
        e.preventDefault();
        setDragging(false);
        const f = e.dataTransfer.files?.[0];
        if (f) onFile(f);
    }

    return (
        <div className="space-y-3">
            <input
                ref={inputRef}
                type="file"
                accept=".csv,.tsv,.txt,.xlsx,.xlsm"
                className="hidden"
                onChange={(e) => {
                    const f = e.target.files?.[0];
                    if (f) onFile(f);
                    e.target.value = "";
                }}
            />
            <div
                onClick={pickFile}
                onDragOver={(e) => {
                    e.preventDefault();
                    setDragging(true);
                }}
                onDragLeave={() => setDragging(false)}
                onDrop={onDrop}
                className={`rounded-lg border-2 border-dashed p-8 text-center cursor-pointer transition-colors ${
                    dragging
                        ? "border-slate-900 bg-slate-50"
                        : "border-slate-200 hover:border-slate-300 hover:bg-slate-50/50"
                }`}
            >
                {busy ? (
                    <>
                        <Loader2Icon className="w-7 h-7 mx-auto text-slate-400 animate-spin" />
                        <p className="text-[12.5px] text-slate-700 font-medium mt-3">
                            Reading your file…
                        </p>
                    </>
                ) : file ? (
                    <>
                        <FileSpreadsheetIcon className="w-7 h-7 mx-auto text-emerald-600" />
                        <p className="text-[12.5px] text-slate-900 font-medium mt-3">
                            {file.name}
                        </p>
                        <p className="text-[11.5px] text-slate-500 mt-1">
                            {(file.size / 1024).toFixed(1)} KB · choose another to replace
                        </p>
                    </>
                ) : (
                    <>
                        <UploadCloudIcon className="w-7 h-7 mx-auto text-slate-400" />
                        <p className="text-[13px] text-slate-900 font-medium mt-3">
                            Drag & drop a file
                        </p>
                        <p className="text-[11.5px] text-slate-500 mt-1">
                            or click to browse — CSV, TSV, or XLSX
                        </p>
                    </>
                )}
            </div>

            <div className="rounded-md border border-slate-200 bg-slate-50/40 p-3">
                <p className="text-[11px] text-slate-700 font-medium mb-1">
                    What we expect
                </p>
                <ul className="text-[11px] text-slate-500 space-y-0.5 list-disc pl-4 leading-snug">
                    <li>One row per contact. First row should be column headers.</li>
                    <li>At minimum, a column with email addresses.</li>
                    <li>Anything we don't recognise stays untouched — you'll map columns next.</li>
                    <li>We dedupe on lowercased email. Choose how to handle existing matches on the next screen.</li>
                </ul>
            </div>
        </div>
    );
}

// ----- Map step --------------------------------------------------

export function MapStep({
    preview,
    mapping,
    setMapping,
    hasHeader,
    setHasHeader,
}: {
    preview: ImportPreview;
    mapping: ImportColumnMapping[];
    setMapping: React.Dispatch<React.SetStateAction<ImportColumnMapping[]>>;
    hasHeader: boolean;
    setHasHeader: (v: boolean) => void;
}) {
    function updateMapping(idx: number, next: ImportColumnMapping) {
        setMapping((cur) =>
            cur.map((m) => (m.index === idx ? next : m)).concat(cur.some((m) => m.index === idx) ? [] : [next]),
        );
    }

    function getMapping(idx: number): ImportColumnMapping {
        return mapping.find((m) => m.index === idx) ?? { index: idx, target: "ignore" };
    }

    return (
        <div className="space-y-3">
            <div className="flex items-center gap-2">
                <div className="flex-1">
                    <p className="text-[12.5px] text-slate-900 font-medium">{preview.filename}</p>
                    <p className="text-[11px] text-slate-500">
                        {preview.format.toUpperCase()} · {preview.total_rows.toLocaleString()} rows · {preview.columns.length} columns
                    </p>
                </div>
                <label className="inline-flex items-center gap-1.5 text-[11.5px] text-slate-700 cursor-pointer">
                    <input
                        type="checkbox"
                        className="w-3.5 h-3.5 rounded accent-slate-900"
                        checked={hasHeader}
                        onChange={(e) => setHasHeader(e.target.checked)}
                    />
                    First row is header
                </label>
            </div>

            <div className="rounded-md border border-slate-200 overflow-x-auto">
                <table className="w-full text-left">
                    <thead className="bg-slate-50/60">
                        <tr className="border-b border-slate-200">
                            <th className="px-3 py-2 text-[10px] font-medium text-slate-400 uppercase tracking-[0.14em] w-12">#</th>
                            <th className="px-3 py-2 text-[10px] font-medium text-slate-400 uppercase tracking-[0.14em]">Column</th>
                            <th className="hidden md:table-cell px-3 py-2 text-[10px] font-medium text-slate-400 uppercase tracking-[0.14em]">Sample</th>
                            <th className="px-3 py-2 text-[10px] font-medium text-slate-400 uppercase tracking-[0.14em] md:w-56">Maps to</th>
                        </tr>
                    </thead>
                    <tbody>
                        {preview.columns.map((col, idx) => {
                            const m = getMapping(idx);
                            const sample = (preview.sample_rows[0]?.[idx] ?? "").toString();
                            return (
                                <tr key={idx} className="border-b border-slate-100 last:border-b-0">
                                    <td className="px-3 py-2 text-[11px] text-slate-400 font-mono">{idx + 1}</td>
                                    <td className="px-3 py-2 text-[12px] text-slate-900 font-medium truncate max-w-[140px]">
                                        {col}
                                    </td>
                                    <td className="hidden md:table-cell px-3 py-2 text-[11.5px] text-slate-500 truncate max-w-[200px] font-mono">
                                        {sample || <span className="text-slate-300">—</span>}
                                    </td>
                                    <td className="px-3 py-2">
                                        <TargetPicker
                                            value={m}
                                            onChange={(next) => updateMapping(idx, next)}
                                        />
                                    </td>
                                </tr>
                            );
                        })}
                    </tbody>
                </table>
            </div>
        </div>
    );
}

export function TargetPicker({
    value,
    onChange,
}: {
    value: ImportColumnMapping;
    onChange: (next: ImportColumnMapping) => void;
}) {
    // Custom-field rows are tagged with target="custom" (sentinel); the
    // user-typed name lives in custom_key. We also accept the legacy
    // "custom:<key>" form in case a saved mapping comes in that shape.
    const isCustom =
        value.target === "custom" || value.target.toString().startsWith("custom:");
    const stdLabel = STANDARD_TARGETS.find((t) => t.id === value.target)?.label;
    const customKey = value.custom_key ?? "";
    const label = isCustom
        ? customKey
            ? `Custom: ${customKey}`
            : "Custom field…"
        : stdLabel ?? "Ignore";

    return (
        <div className="flex items-center gap-1.5">
            <PopoverMenu align="start">
                <PopoverMenuTrigger asChild>
                    <SelectButton label={label} className="flex-1" />
                </PopoverMenuTrigger>
                <PopoverMenuContent minWidth={200}>
                    <PopoverMenuLabel>Standard</PopoverMenuLabel>
                    {STANDARD_TARGETS.map((t) => (
                        <PopoverMenuItem
                            key={t.id}
                            selected={value.target === t.id && !isCustom}
                            onSelect={() =>
                                onChange({ index: value.index, target: t.id })
                            }
                        >
                            {t.label}
                        </PopoverMenuItem>
                    ))}
                    <PopoverMenuLabel>Custom</PopoverMenuLabel>
                    <PopoverMenuItem
                        selected={isCustom}
                        onSelect={() =>
                            onChange({
                                index: value.index,
                                target: "custom",
                                custom_key: customKey,
                            })
                        }
                    >
                        Use as custom field…
                    </PopoverMenuItem>
                </PopoverMenuContent>
            </PopoverMenu>
            {isCustom && (
                <TextInput
                    value={customKey}
                    onChange={(v) =>
                        onChange({ index: value.index, target: "custom", custom_key: v })
                    }
                    placeholder="field name"
                    className="w-24 md:w-32"
                />
            )}
        </div>
    );
}

// ----- Options step ----------------------------------------------

function OptionsStep({
    dedup,
    setDedup,
    categoryIds,
    setCategoryIds,
}: {
    dedup: ImportDedupStrategy;
    setDedup: (v: ImportDedupStrategy) => void;
    categoryIds: string[];
    setCategoryIds: (v: string[]) => void;
}) {
    return (
        <div className="space-y-5">
            <section>
                <h2 className="text-[10px] uppercase tracking-[0.14em] font-semibold text-slate-500 mb-2">
                    Duplicate handling
                </h2>
                <p className="text-[11px] text-slate-400 leading-tight mb-3">
                    We dedupe on lowercased email. Decide what happens when a row in your file matches a contact you already have.
                </p>
                <div className="space-y-2">
                    {DEDUP_OPTIONS.map((opt) => (
                        <label
                            key={opt.id}
                            className={`block rounded-md border p-2.5 cursor-pointer transition-colors ${
                                dedup === opt.id
                                    ? "border-slate-900 bg-slate-50"
                                    : "border-slate-200 hover:border-slate-300"
                            }`}
                        >
                            <div className="flex items-start gap-2">
                                <input
                                    type="radio"
                                    name="dedup"
                                    className="mt-0.5 accent-slate-900"
                                    checked={dedup === opt.id}
                                    onChange={() => setDedup(opt.id)}
                                />
                                <div className="flex-1 min-w-0">
                                    <div className="text-[12px] font-medium text-slate-900 leading-tight">
                                        {opt.label}
                                    </div>
                                    <div className="text-[11px] text-slate-500 leading-snug mt-0.5">
                                        {opt.hint}
                                    </div>
                                </div>
                            </div>
                        </label>
                    ))}
                </div>
            </section>

            <section>
                <h2 className="text-[10px] uppercase tracking-[0.14em] font-semibold text-slate-500 mb-2">
                    Apply categories
                </h2>
                <p className="text-[11px] text-slate-400 leading-tight mb-2">
                    Every imported contact will get these categories. Skip if you don't want to tag the whole batch.
                </p>
                <CategoryPicker value={categoryIds} onChange={setCategoryIds} />
            </section>

            <section className="rounded-md border border-slate-200 bg-slate-50/40 p-3">
                <Label className="text-[10.5px] text-slate-500">Heads up</Label>
                <p className="text-[11px] text-slate-600 leading-snug mt-1">
                    Rows with missing or invalid emails are reported as errors and not imported.
                    You can download an error CSV at the end and re-import after fixing.
                </p>
            </section>
        </div>
    );
}

// ----- Result step ----------------------------------------------

export function ResultStep({ result, filename }: { result: ImportResult; filename: string }) {
    function downloadErrors() {
        if (!result.errors || result.errors.length === 0) return;
        const rows = [["line", "email", "reason"]];
        for (const e of result.errors) {
            rows.push([String(e.line), e.email ?? "", e.reason.replace(/\r?\n/g, " ")]);
        }
        const csv = rows
            .map((r) =>
                r
                    .map((v) => (/[,"\n]/.test(v) ? `"${v.replace(/"/g, '""')}"` : v))
                    .join(","),
            )
            .join("\n");
        const blob = new Blob(["﻿" + csv], { type: "text/csv;charset=utf-8" });
        downloadBlob(blob, filename.replace(/\.[^.]+$/, "") + "-errors.csv");
    }

    return (
        <div className="space-y-4">
            <div className="flex items-center gap-3">
                {result.failed === 0 ? (
                    <CheckCircle2Icon className="w-8 h-8 text-emerald-600 shrink-0" />
                ) : (
                    <AlertTriangleIcon className="w-8 h-8 text-amber-600 shrink-0" />
                )}
                <div className="flex-1">
                    <p className="text-[13.5px] text-slate-900 font-semibold">
                        {result.failed === 0 ? "Import complete" : "Import finished with errors"}
                    </p>
                    <p className="text-[11.5px] text-slate-500 leading-snug mt-0.5">
                        Processed {result.total.toLocaleString()} rows in{" "}
                        {durationText(result.started_at, result.ended_at)}.
                    </p>
                </div>
            </div>

            <div className="grid grid-cols-2 md:grid-cols-4 gap-2">
                <StatCard label="Imported"  value={result.imported} accent="emerald" />
                <StatCard label="Updated"   value={result.updated}  accent="sky" />
                <StatCard label="Skipped"   value={result.skipped}  accent="slate" />
                <StatCard label="Failed"    value={result.failed}   accent={result.failed > 0 ? "red" : "slate"} />
            </div>

            {result.errors && result.errors.length > 0 && (
                <div className="rounded-md border border-slate-200 overflow-hidden">
                    <div className="px-3 h-9 border-b border-slate-200 bg-slate-50/60 flex items-center gap-2">
                        <span className="text-[11px] uppercase tracking-[0.14em] text-slate-500 font-medium">
                            Errors
                        </span>
                        <span className="text-[11px] text-slate-500">
                            {result.errors.length}
                        </span>
                        <button
                            type="button"
                            onClick={downloadErrors}
                            className="ml-auto h-6 px-2 rounded text-[11px] text-slate-700 hover:text-slate-900 hover:bg-slate-100 inline-flex items-center gap-1 transition-colors"
                        >
                            <DownloadIcon className="w-3 h-3" />
                            Download errors
                        </button>
                    </div>
                    <div className="max-h-56 overflow-y-auto">
                        <table className="w-full text-left">
                            <thead className="bg-white sticky top-0">
                                <tr className="border-b border-slate-100">
                                    <th className="px-3 py-1.5 text-[10px] font-medium text-slate-400 uppercase tracking-[0.14em] w-12">Line</th>
                                    <th className="hidden md:table-cell px-3 py-1.5 text-[10px] font-medium text-slate-400 uppercase tracking-[0.14em]">Email</th>
                                    <th className="px-3 py-1.5 text-[10px] font-medium text-slate-400 uppercase tracking-[0.14em]">Reason</th>
                                </tr>
                            </thead>
                            <tbody>
                                {result.errors.slice(0, 200).map((e, i) => (
                                    <tr key={i} className="border-b border-slate-100 last:border-b-0">
                                        <td className="px-3 py-1.5 text-[11px] text-slate-500 font-mono">{e.line}</td>
                                        <td className="hidden md:table-cell px-3 py-1.5 text-[11.5px] text-slate-700 truncate max-w-[180px]">
                                            {e.email || <span className="text-slate-300">—</span>}
                                        </td>
                                        <td className="px-3 py-1.5 text-[11.5px] text-slate-700 leading-snug">{e.reason}</td>
                                    </tr>
                                ))}
                            </tbody>
                        </table>
                    </div>
                </div>
            )}
        </div>
    );
}

function StatCard({
    label,
    value,
    accent,
}: {
    label: string;
    value: number;
    accent: "emerald" | "sky" | "slate" | "red";
}) {
    const ring = {
        emerald: "ring-emerald-200 bg-emerald-50 text-emerald-700",
        sky:     "ring-sky-200 bg-sky-50 text-sky-700",
        slate:   "ring-slate-200 bg-slate-50 text-slate-700",
        red:     "ring-red-200 bg-red-50 text-red-700",
    }[accent];
    return (
        <div className={`rounded-md ring-1 p-2.5 ${ring}`}>
            <div className="text-[10px] uppercase tracking-[0.14em] font-medium opacity-75">
                {label}
            </div>
            <div className="text-[18px] font-semibold tabular-nums mt-0.5">
                {value.toLocaleString()}
            </div>
        </div>
    );
}

function durationText(start: string, end: string): string {
    const s = new Date(start).getTime();
    const e = new Date(end).getTime();
    if (Number.isNaN(s) || Number.isNaN(e)) return "—";
    const ms = e - s;
    if (ms < 1000) return `${ms} ms`;
    const sec = ms / 1000;
    if (sec < 60) return `${sec.toFixed(1)} s`;
    return `${(sec / 60).toFixed(1)} min`;
}
