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
//   3. Options — dedup strategy, categories to apply, campaigns and
//      segments to join. These are global to the import; per-row
//      overrides happen post-import via bulk edit.
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
    BracesIcon,
    CheckCircle2Icon,
    CheckIcon,
    DownloadIcon,
    FileSpreadsheetIcon,
    Loader2Icon,
    PlusIcon,
    ShieldCheckIcon,
    SparklesIcon,
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
import { CampaignMultiPicker, SegmentMultiPicker } from "@/components/app/segments/SegmentPickers";
import { useSegments } from "@/lib/api/hooks/app/segments";
import { downloadBlob } from "@/lib/api/client/app/contacts/exportContacts";
import useCustomFieldKeys from "@/lib/api/hooks/app/contacts/useCustomFieldKeys";
import {
    CUSTOM_KEY_RULES,
    DEDUP_OPTIONS,
    STANDARD_TARGETS,
    VERIFICATION_VOCABULARY_LABELS,
    customKeyOf,
    customKeyStatus,
    describeError,
    foldCustomKey,
    isCustomTarget,
    isValidCustomKey,
    mappingProblem,
    matchExistingKey,
    normalizeCustomKey,
    suggestCustomKey,
    targetIdentity,
} from "./importShared";
import { Checkbox } from "@/components/ui/checkbox";

interface Props {
    open: boolean;
    onClose: () => void;
    // When set (the campaign Leads tab), imported contacts are attached to this
    // campaign and the wizard shows a read-only "Adding to …" indicator.
    lockedCampaign?: { id: string; name: string };
    // When set (a segment's member list), every imported row is pinned into
    // this segment, the same way the campaign target works. Without it an
    // import started from inside a segment created contacts that were nowhere
    // in it (issue #381).
    lockedSegment?: { id: string; name: string; color?: string };
}

type Step = "upload" | "map" | "options" | "result";

export default function ImportWizard({ open, onClose, lockedCampaign, lockedSegment }: Props) {
    const [step, setStep] = React.useState<Step>("upload");
    const [file, setFile] = React.useState<File | null>(null);
    const [preview, setPreview] = React.useState<ImportPreview | null>(null);
    const [mapping, setMapping] = React.useState<ImportColumnMapping[]>([]);
    const [hasHeader, setHasHeader] = React.useState<boolean>(true);
    const [dedup, setDedup] = React.useState<ImportDedupStrategy>("skip");
    const [categoryIds, setCategoryIds] = React.useState<string[]>([]);
    const [campaignIds, setCampaignIds] = React.useState<string[]>([]);
    const [segmentIds, setSegmentIds] = React.useState<string[]>([]);
    const [previewBusy, setPreviewBusy] = React.useState<boolean>(false);
    const [commitBusy, setCommitBusy] = React.useState<boolean>(false);
    const [result, setResult] = React.useState<ImportResult | null>(null);
    const queryClient = useQueryClient();
    const segments = useSegments(open);

    function reset() {
        setStep("upload");
        setFile(null);
        setPreview(null);
        setMapping([]);
        setHasHeader(true);
        setDedup("skip");
        setCategoryIds([]);
        setCampaignIds([]);
        setSegmentIds([]);
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

    // The segment the wizard was opened inside always travels with the
    // import, whether or not the user opened the options step.
    const targetSegmentIds = React.useMemo(
        () => (lockedSegment ? [lockedSegment.id, ...segmentIds.filter((id) => id !== lockedSegment.id)] : segmentIds),
        [lockedSegment, segmentIds],
    );

    // What the result step names back. A segment the list has not loaded
    // falls back to the locked one's name rather than showing an id.
    const pinnedSegmentNames = React.useMemo(() => {
        const byId = new Map((segments.data ?? []).map((seg) => [seg.id, seg.name]));
        return targetSegmentIds.map((id) => byId.get(id) ?? (id === lockedSegment?.id ? lockedSegment.name : "a segment"));
    }, [segments.data, targetSegmentIds, lockedSegment]);

    async function commit() {
        if (!file || !preview) return;
        setCommitBusy(true);
        try {
            const res = await importCommitContacts(file, {
                mapping,
                dedup,
                has_header: hasHeader,
                category_ids: categoryIds.length > 0 ? categoryIds : undefined,
                campaign_ids: lockedCampaign ? [lockedCampaign.id] : campaignIds.length > 0 ? campaignIds : undefined,
                segment_ids: targetSegmentIds.length > 0 ? targetSegmentIds : undefined,
            });
            setResult(res);
            setStep("result");
            // Segment counts and pinned-member lists move with the import, so
            // the page behind the wizard is right before the spine event lands.
            await Promise.all([
                queryClient.invalidateQueries({ queryKey: ["contacts"] }),
                queryClient.invalidateQueries({ queryKey: ["segments"] }),
            ]);
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

    // Any reason the mapping can't be committed (missing email, an unnamed or
    // unusable custom-field name). Caught here so a mistyped field name costs
    // one glance instead of a whole import.
    const mapProblem = mappingProblem(mapping);

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
                            {lockedSegment && (
                                <span className="hidden sm:inline-flex items-center gap-1 h-5 px-1.5 rounded bg-sky-50 text-sky-700 text-[10px] font-medium max-w-[180px]">
                                    <span className="shrink-0">→</span>
                                    {lockedSegment.color && (
                                        <span className="size-2 rounded-full shrink-0" style={{ backgroundColor: lockedSegment.color }} />
                                    )}
                                    <span className="truncate">{lockedSegment.name}</span>
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
                                    campaignIds={campaignIds}
                                    setCampaignIds={setCampaignIds}
                                    segmentIds={segmentIds}
                                    setSegmentIds={setSegmentIds}
                                    campaignLocked={!!lockedCampaign}
                                    lockedSegment={lockedSegment}
                                />
                            )}
                            {step === "result" && result && (
                                <ResultStep
                                    result={result}
                                    filename={file?.name ?? "import"}
                                    pinnedSegments={pinnedSegmentNames}
                                />
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
                                    {mapProblem && (
                                        <span className="text-[11px] text-amber-700 inline-flex items-center gap-1">
                                            <AlertTriangleIcon className="w-3 h-3 shrink-0" />
                                            {mapProblem}
                                        </span>
                                    )}
                                    <button
                                        type="button"
                                        onClick={() => setStep("options")}
                                        disabled={!!mapProblem}
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

    const { data: existingKeys = [] } = useCustomFieldKeys();

    // A column the judgment placed keeps its marker until the user changes it.
    function isInferred(idx: number, m: ImportColumnMapping): boolean {
        if (!preview.inferred_columns?.includes(idx)) return false;
        const s = preview.suggested_mapping.find((x) => x.index === idx);
        return !!s && s.target === m.target && (s.custom_key ?? "") === (m.custom_key ?? "");
    }

    // Which columns write to each destination, so a row can say when another
    // column shares its field. Column numbers are 1-based, as on screen.
    const writers = new Map<string, number[]>();
    for (const m of mapping) {
        const id = targetIdentity(m);
        if (id === null || m.index >= preview.columns.length) continue;
        writers.set(id, [...(writers.get(id) ?? []), m.index + 1]);
    }
    function takenKeysFor(idx: number): Map<string, number> {
        const taken = new Map<string, number>();
        for (const [id, cols] of writers) {
            const other = cols.find((c) => c !== idx + 1);
            if (id.startsWith("custom:") && other !== undefined) taken.set(id.slice(7), other);
        }
        return taken;
    }

    // Columns we didn't recognise default to Ignore, which means a CRM export
    // with a dozen extra columns is a dozen dropdowns. Offer the obvious bulk
    // action for the ones whose header is already a usable field name, landing
    // on the workspace's own spelling when the field already exists.
    // Only with a real header row: without one the columns are synthesised
    // ("Column 4"), which is a legal field name but never the one you want.
    const claimable: { idx: number; key: string }[] = [];
    if (hasHeader) {
        const claimed = new Set([...writers.keys()]);
        preview.columns.forEach((header, idx) => {
            if (getMapping(idx).target !== "ignore") return;
            const key = matchExistingKey(header, existingKeys) ?? suggestCustomKey(header);
            // A field another column already fills is left for the user to decide.
            if (key === "" || claimed.has(`custom:${key}`)) return;
            claimed.add(`custom:${key}`);
            claimable.push({ idx, key });
        });
    }

    function claimAllAsCustom() {
        setMapping((cur) => {
            const next = [...cur];
            for (const { idx, key } of claimable) {
                const at = next.findIndex((m) => m.index === idx);
                const entry: ImportColumnMapping = { index: idx, target: "custom", custom_key: key };
                if (at >= 0) next[at] = entry;
                else next.push(entry);
            }
            return next;
        });
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
                {claimable.length > 0 && (
                    <button
                        type="button"
                        onClick={claimAllAsCustom}
                        className="h-7 px-2.5 rounded-md border border-slate-200 hover:border-slate-300 text-slate-700 hover:text-slate-900 text-[11.5px] font-medium transition-colors shrink-0"
                    >
                        Keep {claimable.length} more as custom fields
                    </button>
                )}
                <label className="inline-flex items-center gap-1.5 text-[11.5px] text-slate-700 cursor-pointer">
                    <Checkbox tone="slate"
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
                                            header={col}
                                            onChange={(next) => updateMapping(idx, next)}
                                            existingKeys={existingKeys}
                                            takenKeys={takenKeysFor(idx)}
                                            writers={writers.get(targetIdentity(m) ?? "") ?? []}
                                            inferred={isInferred(idx, m)}
                                        />
                                        <AnimatePresence initial={false}>
                                            {m.target === "verification_status" && (
                                                <motion.p
                                                    key="vocab"
                                                    initial={{ opacity: 0, y: -4 }}
                                                    animate={{ opacity: 1, y: 0 }}
                                                    exit={{ opacity: 0, y: -4 }}
                                                    className="mt-1 text-[10.5px] text-emerald-700 inline-flex items-center gap-1"
                                                >
                                                    <ShieldCheckIcon className="w-3 h-3" />
                                                    {m.verification_provider && VERIFICATION_VOCABULARY_LABELS[m.verification_provider]
                                                        ? `${VERIFICATION_VOCABULARY_LABELS[m.verification_provider]} results recognised`
                                                        : "Verification results recognised; these leads skip the built-in check"}
                                                </motion.p>
                                            )}
                                        </AnimatePresence>
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

// MappingNote says what a custom mapping will do to the workspace's fields
// (fill an existing one, create one, or nearly duplicate one) and when another
// column writes to the same place.
function MappingNote({
    mapping,
    existingKeys,
    writers,
    column,
    onUseExisting,
}: {
    mapping: ImportColumnMapping;
    existingKeys: string[];
    // Every column writing where this one does, in the order the importer
    // applies them: the last non-empty cell is the one kept.
    writers: number[];
    column: number;
    onUseExisting: (key: string) => void;
}) {
    const sharedWith = writers.filter((c) => c !== column);
    const kept = writers[writers.length - 1];
    const key = customKeyOf(mapping);
    const status = isCustomTarget(mapping.target) && isValidCustomKey(key) ? customKeyStatus(key, existingKeys) : null;
    if (!status && sharedWith.length === 0) return null;
    return (
        <div className="mt-1 space-y-0.5">
            {status?.kind === "existing" && (
                <p className="text-[10.5px] text-emerald-700 inline-flex items-center gap-1">
                    <CheckIcon className="w-3 h-3 shrink-0" />
                    Fills your existing field
                </p>
            )}
            {status?.kind === "new" && (
                <p className="text-[10.5px] text-sky-700 inline-flex items-center gap-1">
                    <PlusIcon className="w-3 h-3 shrink-0" />
                    Creates a new field
                </p>
            )}
            {status?.kind === "similar" && (
                <p className="text-[10.5px] text-amber-700 flex flex-wrap items-center gap-x-1">
                    <AlertTriangleIcon className="w-3 h-3 shrink-0" />
                    <span>
                        You already have <span className="font-medium">{status.existing}</span>.
                    </span>
                    <button
                        type="button"
                        onClick={() => onUseExisting(status.existing)}
                        className="font-medium text-amber-800 underline underline-offset-2 hover:text-amber-900"
                    >
                        Use it
                    </button>
                </p>
            )}
            {sharedWith.length > 0 && (
                <p className="text-[10.5px] text-slate-500 leading-snug">
                    Also filled by column {sharedWith.join(", ")}. {sharedWith.length === 1 ? "When both have a value" : "When several do"}, column {kept}'s is kept.
                </p>
            )}
        </div>
    );
}

export function TargetPicker({
    value,
    onChange,
    header,
    existingKeys = [],
    takenKeys,
    writers = [],
    inferred = false,
}: {
    value: ImportColumnMapping;
    onChange: (next: ImportColumnMapping) => void;
    /** The column's header, used to pre-fill the custom-field name. */
    header?: string;
    /** The workspace's custom fields, most used first. */
    existingKeys?: string[];
    /** Custom fields other columns already write to, with that column's number. */
    takenKeys?: Map<string, number>;
    /** Every column writing where this one does, in the order they are applied. */
    writers?: number[];
    /** The suggestion came from what the header means, not what it says. */
    inferred?: boolean;
}) {
    const [open, setOpen] = React.useState(false);
    const [query, setQuery] = React.useState("");
    // Set once the user names a new field, so the name box stays put when
    // what they type happens to match an existing field.
    const [naming, setNaming] = React.useState(false);

    const isCustom = isCustomTarget(value.target.toString());
    const customKey = isCustom ? customKeyOf(value) : "";
    const rawKey = value.custom_key ?? customKey;
    const keyExists = isCustom && existingKeys.includes(customKey);
    const isExisting = keyExists && !naming;
    const keyInvalid = isCustom && rawKey.trim() !== "" && !isValidCustomKey(rawKey);
    const stdLabel = STANDARD_TARGETS.find((t) => t.id === value.target)?.label;
    const label = isExisting ? customKey : isCustom ? (keyExists ? "Existing field" : "New field") : stdLabel ?? "Ignore";

    const q = normalizeCustomKey(query);
    const fq = foldCustomKey(q);
    const matches = (text: string) => fq === "" || foldCustomKey(text).includes(fq);
    const standard = STANDARD_TARGETS.filter((t) => matches(t.label));
    const custom = existingKeys.filter(matches);
    // Typing a name nobody has yet offers to create it, the way a tag input does.
    const canCreate = q !== "" && isValidCustomKey(q) && !existingKeys.includes(q);

    function setMenuOpen(o: boolean) {
        setOpen(o);
        if (!o) setQuery("");
    }

    function pickStandard(id: string) {
        setNaming(false);
        onChange({ index: value.index, target: id });
    }

    function pickExisting(key: string) {
        setNaming(false);
        onChange({ index: value.index, target: "custom", custom_key: key });
    }

    function pickNew(name: string) {
        setNaming(true);
        onChange({ index: value.index, target: "custom", custom_key: name });
    }

    // Enter takes an exact name first, then the first match; with nothing
    // typed it does nothing, so it can never remap a column by accident.
    function pickFirst() {
        if (q === "") return;
        const exactStd = standard.find((t) => t.label.toLowerCase() === q.toLowerCase());
        if (exactStd) pickStandard(exactStd.id);
        else if (existingKeys.includes(q)) pickExisting(q);
        else if (standard.length > 0) pickStandard(standard[0].id);
        else if (custom.length > 0) pickExisting(custom[0]);
        else if (canCreate) pickNew(q);
        else return;
        setMenuOpen(false);
    }

    return (
        <>
            <div className="flex items-center gap-1.5">
                <PopoverMenu align="start" open={open} onOpenChange={setMenuOpen}>
                    <PopoverMenuTrigger asChild>
                        <SelectButton
                            icon={keyExists ? <BracesIcon className="w-3 h-3" /> : isCustom ? <PlusIcon className="w-3 h-3" /> : undefined}
                            label={label}
                            title={keyExists ? `Existing custom field: ${customKey}` : undefined}
                            className="flex-1 min-w-0"
                        />
                    </PopoverMenuTrigger>
                    <PopoverMenuContent minWidth={240} className="py-0 w-[260px]">
                        <div className="px-2 py-1.5 border-b border-slate-200">
                            <input
                                value={query}
                                onChange={(e) => setQuery(e.target.value)}
                                onKeyDown={(e) => {
                                    if (e.key === "Enter") {
                                        e.preventDefault();
                                        pickFirst();
                                    }
                                }}
                                placeholder={existingKeys.length > 0 ? "Search or name a new field…" : "Search or name a field…"}
                                autoFocus
                                aria-label="Search fields"
                                className="w-full h-5 bg-transparent text-[16px] md:text-[12px] text-slate-900 placeholder:text-slate-400 outline-none"
                            />
                        </div>
                        <div className="max-h-[min(60vh,360px)] overflow-y-auto py-1">
                            {standard.length > 0 && <PopoverMenuLabel>Standard</PopoverMenuLabel>}
                            {standard.map((t) => (
                                <PopoverMenuItem
                                    key={t.id}
                                    selected={value.target === t.id && !isCustom}
                                    onSelect={() => pickStandard(t.id)}
                                >
                                    {t.label}
                                </PopoverMenuItem>
                            ))}
                            {custom.length > 0 && <PopoverMenuLabel>Your custom fields</PopoverMenuLabel>}
                            {custom.map((key) => {
                                const takenBy = takenKeys?.get(key);
                                return (
                                    <PopoverMenuItem
                                        key={key}
                                        icon={<BracesIcon className="w-3 h-3" />}
                                        selected={isCustom && customKey === key}
                                        onSelect={() => pickExisting(key)}
                                        trailing={
                                            isCustom && customKey === key ? undefined : takenBy !== undefined ? (
                                                <span className="text-[10.5px] text-slate-400">column {takenBy}</span>
                                            ) : undefined
                                        }
                                    >
                                        {key}
                                    </PopoverMenuItem>
                                );
                            })}
                            <PopoverMenuLabel>New</PopoverMenuLabel>
                            {canCreate ? (
                                <PopoverMenuItem icon={<PlusIcon className="w-3 h-3" />} onSelect={() => pickNew(q)}>
                                    Create “{q}”
                                </PopoverMenuItem>
                            ) : (
                                <PopoverMenuItem
                                    icon={<PlusIcon className="w-3 h-3" />}
                                    selected={isCustom && !isExisting}
                                    // Start from the column header: "Company Mobile"
                                    // is a valid field name, so there is nothing to
                                    // type in the common case.
                                    onSelect={() => pickNew(isCustom && !isExisting ? rawKey : suggestCustomKey(header ?? ""))}
                                >
                                    New custom field…
                                </PopoverMenuItem>
                            )}
                            {q !== "" && !canCreate && standard.length === 0 && custom.length === 0 && (
                                <p className="px-3 pb-1.5 text-[11px] text-slate-400 leading-snug">{CUSTOM_KEY_RULES}</p>
                            )}
                        </div>
                    </PopoverMenuContent>
                </PopoverMenu>
                {isCustom && !isExisting && (
                    <TextInput
                        value={rawKey}
                        onChange={(v) => {
                            setNaming(true);
                            onChange({ index: value.index, target: "custom", custom_key: v });
                        }}
                        placeholder="field name"
                        invalid={keyInvalid}
                        title={keyInvalid ? CUSTOM_KEY_RULES : undefined}
                        className="w-24 md:w-32"
                    />
                )}
            </div>
            {inferred && (
                <p className="mt-1 text-[10.5px] text-sky-700 inline-flex items-center gap-1">
                    <SparklesIcon className="w-3 h-3 shrink-0" />
                    Matched by meaning. Check it.
                </p>
            )}
            <MappingNote
                mapping={value}
                existingKeys={existingKeys}
                writers={writers}
                column={value.index + 1}
                onUseExisting={pickExisting}
            />
        </>
    );
}

// ----- Options step ----------------------------------------------

function OptionsStep({
    dedup,
    setDedup,
    categoryIds,
    setCategoryIds,
    campaignIds,
    setCampaignIds,
    segmentIds,
    setSegmentIds,
    campaignLocked,
    lockedSegment,
}: {
    dedup: ImportDedupStrategy;
    setDedup: (v: ImportDedupStrategy) => void;
    categoryIds: string[];
    setCategoryIds: (v: string[]) => void;
    campaignIds: string[];
    setCampaignIds: (v: string[]) => void;
    segmentIds: string[];
    setSegmentIds: (v: string[]) => void;
    // From a campaign's Leads tab the target campaign is fixed, so the
    // campaign picker is hidden and the header chip shows the target instead.
    campaignLocked: boolean;
    // From a segment's member list the segment is fixed and always applied;
    // the picker stays so more segments can be added alongside it.
    lockedSegment?: { id: string; name: string; color?: string };
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

            {!campaignLocked && (
                <section>
                    <h2 className="text-[10px] uppercase tracking-[0.14em] font-semibold text-slate-500 mb-2">
                        Add to campaigns
                    </h2>
                    <p className="text-[11px] text-slate-400 leading-tight mb-2">
                        Every imported contact joins these campaigns as a lead. An active campaign starts emailing them
                        on its own schedule.
                    </p>
                    <CampaignMultiPicker value={campaignIds} onChange={setCampaignIds} />
                </section>
            )}

            <section>
                <h2 className="text-[10px] uppercase tracking-[0.14em] font-semibold text-slate-500 mb-2">
                    Add to segments
                </h2>
                <p className="text-[11px] text-slate-400 leading-tight mb-2">
                    {lockedSegment
                        ? "Every imported contact is pinned into this segment, whether or not it matches the segment's conditions. Add more below."
                        : "Every imported contact is pinned into these segments. A segment linked to a campaign enrols them there automatically."}
                </p>
                {lockedSegment && (
                    <div className="mb-2 flex items-center gap-1.5 rounded-md border border-sky-200 bg-sky-50/60 px-2 h-7">
                        <span
                            className="size-2 rounded-full shrink-0"
                            style={{ backgroundColor: lockedSegment.color ?? "#0284c7" }}
                        />
                        <span className="text-[12px] font-medium text-sky-900 truncate">{lockedSegment.name}</span>
                        <span className="ml-auto text-[10px] uppercase tracking-[0.14em] text-sky-700 shrink-0">
                            Always
                        </span>
                    </div>
                )}
                <SegmentMultiPicker value={segmentIds} onChange={setSegmentIds} exclude={lockedSegment?.id} />
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

export function ResultStep({
    result,
    filename,
    pinnedSegments,
}: {
    result: ImportResult;
    filename: string;
    // Names of the segments every imported, updated and skipped row was
    // pinned into, so the wizard confirms the membership it just wrote.
    pinnedSegments?: string[];
}) {
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
                        {result.segments_pinned && pinnedSegments && pinnedSegments.length > 0 && (
                            <> Pinned into {pinnedSegments.join(", ")}.</>
                        )}
                    </p>
                </div>
            </div>

            <div className="grid grid-cols-2 md:grid-cols-4 gap-2">
                <StatCard label="Imported"  value={result.imported} accent="emerald" />
                <StatCard label="Updated"   value={result.updated}  accent="sky" />
                <StatCard label="Skipped"   value={result.skipped}  accent="slate" />
                <StatCard label="Failed"    value={result.failed}   accent={result.failed > 0 ? "red" : "slate"} />
            </div>

            {result.segments_pinned === false && (
                <div className="rounded-md border border-amber-200 bg-amber-50 px-3 py-2.5 flex items-start gap-2">
                    <AlertTriangleIcon className="w-3.5 h-3.5 mt-px shrink-0 text-amber-600" />
                    <div className="min-w-0">
                        <p className="text-[12.5px] font-medium text-amber-900">
                            The contacts are in, but not in the segment
                        </p>
                        <p className="text-[11.5px] text-amber-800/90 leading-relaxed mt-0.5">
                            The rows imported; the membership write did not. The reason is in the notes below. Select
                            them in your contact list and use <span className="font-medium">Segment</span> to add them,
                            or run the import again.
                        </p>
                    </div>
                </div>
            )}

            {result.quality?.flagged && (
                <div className="rounded-md border border-amber-200 bg-amber-50 px-3 py-2.5 flex items-start gap-2">
                    <AlertTriangleIcon className="w-3.5 h-3.5 mt-px shrink-0 text-amber-600" />
                    <div className="min-w-0">
                        <p className="text-[12.5px] font-medium text-amber-900">This list looks low quality</p>
                        <p className="text-[11.5px] text-amber-800/90 leading-relaxed mt-0.5">
                            {result.quality.summary} They are imported, but sending to them risks the reputation of
                            every mailbox in this workspace. Clean the list before launching a campaign with it.
                        </p>
                    </div>
                </div>
            )}

            {result.errors && result.errors.length > 0 && (
                <div className="rounded-md border border-slate-200 overflow-hidden">
                    <div className="px-3 h-9 border-b border-slate-200 bg-slate-50/60 flex items-center gap-2">
                        <span className="text-[11px] uppercase tracking-[0.14em] text-slate-500 font-medium">
                            {result.failed === 0 ? "Notes" : "Errors"}
                        </span>
                        <span className="text-[11px] text-slate-500">
                            {result.errors_truncated
                                ? `${result.errors.length.toLocaleString()} of ${result.failed.toLocaleString()}`
                                : result.errors.length.toLocaleString()}
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
                                        <td className="px-3 py-1.5 text-[11px] text-slate-500 font-mono">
                                            {e.line > 0 ? e.line : <span className="text-slate-300">—</span>}
                                        </td>
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
