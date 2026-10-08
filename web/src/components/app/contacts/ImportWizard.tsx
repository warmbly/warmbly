// ImportWizard runs a contact file import as a background job: upload once, map, review the whole file, then follow it.
// A draft autosaves and lives in the URL, so a reload or a closed dialog picks up where it was.

import React from "react";
import { AnimatePresence, motion } from "framer-motion";
import {
    ArrowLeftIcon,
    ArrowRightIcon,
    CheckIcon,
    CloudCheckIcon,
    Columns3Icon,
    ListChecksIcon,
    Loader2Icon,
    RocketIcon,
    Trash2Icon,
    UploadCloudIcon,
    XIcon,
    type LucideIcon,
} from "lucide-react";
import toast from "react-hot-toast/headless";
import { keepPreviousData, useQuery, useQueryClient } from "@tanstack/react-query";

import type { ImportColumnMapping, ImportDedupStrategy } from "@/lib/api/client/app/contacts/importContacts";
import {
    analyzeContactImport,
    cancelContactImport,
    createContactImport,
    getContactImport,
    saveContactImportDraft,
    startContactImport,
} from "@/lib/api/client/app/contacts/contactImports";
import { CONTACT_IMPORTS_KEY, useContactImport } from "@/lib/api/hooks/app/contacts/useContactImports";
import { isImportActive, type ContactImport } from "@/lib/api/models/app/contacts/ContactImport";
import { useSegments } from "@/lib/api/hooks/app/segments";
import { useConfirm } from "@/hooks/context/confirm";
import { MAX_IMPORT_UPLOAD_BYTES, describeError, mappingProblem } from "./importShared";
import UploadStep from "./import/UploadStep";
import MapStep from "./import/MapStep";
import ReviewStep from "./import/ReviewStep";
import RunStep from "./import/RunStep";

interface Props {
    open: boolean;
    onClose: () => void;
    // When set (the campaign Leads tab), imported contacts are attached to this
    // campaign and the wizard shows a read-only "Adding to …" indicator.
    lockedCampaign?: { id: string; name: string };
    // When set (a segment's member list), every imported row is pinned into
    // this segment, the same way the campaign target works.
    lockedSegment?: { id: string; name: string; color?: string };
    // Opens straight onto an import: a draft resumes, a run shows its progress.
    initialImportId?: string | null;
    // The step a draft resumes on.
    initialStep?: ImportStep;
    // Reports the import and step on screen, so the page can keep them in its URL.
    onRoute?: (importId: string | null, step: ImportStep) => void;
}

export type ImportStep = "upload" | "map" | "review" | "run";
type Step = ImportStep;
type SaveState = "idle" | "saving" | "saved" | "error";

const STEPS: { key: Step; label: string; icon: LucideIcon }[] = [
    { key: "upload", label: "Upload", icon: UploadCloudIcon },
    { key: "map", label: "Map columns", icon: Columns3Icon },
    { key: "review", label: "Review", icon: ListChecksIcon },
    { key: "run", label: "Import", icon: RocketIcon },
];

const paneVariants = {
    enter: (dir: 1 | -1) => ({ x: dir * 28, opacity: 0 }),
    center: { x: 0, opacity: 1 },
    exit: (dir: 1 | -1) => ({ x: dir * -28, opacity: 0 }),
};

const ACCEPTED = /\.(csv|tsv|txt|xlsx|xlsm)$/i;

// Every footer step button is the same blue; a disabled one turns neutral grey.
const FOOTER_BUTTON =
    "h-8 px-3.5 rounded-md text-[12.5px] font-medium inline-flex items-center gap-1.5 transition-colors shrink-0 disabled:cursor-not-allowed disabled:bg-slate-100 disabled:text-slate-400 disabled:shadow-none disabled:ring-slate-200";
const NEXT_BUTTON = "bg-sky-600 hover:bg-sky-700 text-white shadow-sm shadow-sky-600/25 ring-1 ring-inset ring-sky-700/40";
const PRIMARY_BUTTON = NEXT_BUTTON;

export default function ImportWizard({ open, onClose, lockedCampaign, lockedSegment, initialImportId, initialStep, onRoute }: Props) {
    const [step, setStep] = React.useState<Step>("upload");
    const [direction, setDirection] = React.useState<1 | -1>(1);
    const [draft, setDraft] = React.useState<ContactImport | null>(null);
    const [runId, setRunId] = React.useState<string | null>(null);
    const [mapping, setMapping] = React.useState<ImportColumnMapping[]>([]);
    const [hasHeader, setHasHeader] = React.useState(true);
    const [dedup, setDedup] = React.useState<ImportDedupStrategy>("skip");
    const [subscribedDefault, setSubscribedDefault] = React.useState(true);
    const [categoryIds, setCategoryIds] = React.useState<string[]>([]);
    const [campaignIds, setCampaignIds] = React.useState<string[]>([]);
    const [segmentIds, setSegmentIds] = React.useState<string[]>([]);
    const [uploading, setUploading] = React.useState(false);
    const [uploadFraction, setUploadFraction] = React.useState<number | null>(null);
    const [fileName, setFileName] = React.useState<string>("");
    const [starting, setStarting] = React.useState(false);
    const [resuming, setResuming] = React.useState(false);
    const [saveState, setSaveState] = React.useState<SaveState>("idle");
    // The options last written to the draft; autosave only sends a change.
    const savedRef = React.useRef<string>("");
    const queryClient = useQueryClient();
    const confirm = useConfirm();
    const segments = useSegments(open);
    const running = useContactImport(step === "run" ? runId : null);

    const reset = React.useCallback(() => {
        setStep("upload");
        setDirection(1);
        setDraft(null);
        setRunId(null);
        setMapping([]);
        setHasHeader(true);
        setDedup("skip");
        setSubscribedDefault(true);
        setCategoryIds([]);
        setCampaignIds([]);
        setSegmentIds([]);
        setUploading(false);
        setUploadFraction(null);
        setFileName("");
        setStarting(false);
        setResuming(false);
        setSaveState("idle");
        savedRef.current = "";
    }, []);

    function go(next: Step) {
        const from = STEPS.findIndex((s) => s.key === step);
        const to = STEPS.findIndex((s) => s.key === next);
        setDirection(to >= from ? 1 : -1);
        setStep(next);
    }

    // resume puts the wizard back where an import is: a draft on its mapping or
    // review with every choice it had, anything else on its progress or result.
    const resume = React.useCallback(
        (imp: ContactImport, at?: Step) => {
            if (imp.status !== "draft" || !imp.preview) {
                setDraft(null);
                setRunId(imp.id);
                setDirection(1);
                setStep("run");
                return;
            }
            const o = imp.options;
            const restored = {
                mapping: o?.mapping?.length ? o.mapping : imp.preview.suggested_mapping,
                hasHeader: o ? o.has_header : imp.preview.has_header,
                dedup: o?.dedup || "skip",
                subscribedDefault: o?.subscribed_default ?? true,
                categoryIds: o?.category_ids ?? [],
                campaignIds: (o?.campaign_ids ?? []).filter((id) => id !== lockedCampaign?.id),
                segmentIds: (o?.segment_ids ?? []).filter((id) => id !== lockedSegment?.id),
            };
            // The next autosave compares against what was just restored.
            savedRef.current = "";
            setRunId(null);
            setDraft(imp);
            setFileName(imp.filename);
            setMapping(restored.mapping);
            setHasHeader(restored.hasHeader);
            setDedup(restored.dedup);
            setSubscribedDefault(restored.subscribedDefault);
            setCategoryIds(restored.categoryIds);
            setCampaignIds(restored.campaignIds);
            setSegmentIds(restored.segmentIds);
            setDirection(1);
            setStep(at === "review" ? "review" : "map");
            setSaveState(o ? "saved" : "idle");
        },
        [lockedCampaign, lockedSegment],
    );

    React.useEffect(() => {
        if (!open) {
            reset();
            return;
        }
        if (!initialImportId) return;
        let live = true;
        setResuming(true);
        getContactImport(initialImportId)
            .then((imp) => live && resume(imp, initialStep))
            .catch(() => {
                if (!live) return;
                toast.error("That import is no longer available. Drafts are kept for 24 hours.");
                setStep("upload");
            })
            .finally(() => live && setResuming(false));
        return () => {
            live = false;
        };
        // initialStep is read once, when the import opens.
        // eslint-disable-next-line react-hooks/exhaustive-deps
    }, [open, initialImportId, reset, resume]);

    // The page keeps the import and step in its URL, so a reload lands here again.
    const routeId = runId ?? draft?.id ?? null;
    React.useEffect(() => {
        if (open && !resuming) onRoute?.(routeId, step);
    }, [open, resuming, routeId, step, onRoute]);

    // The segment the wizard was opened inside always travels with the import.
    const targetSegmentIds = React.useMemo(
        () => (lockedSegment ? [lockedSegment.id, ...segmentIds.filter((id) => id !== lockedSegment.id)] : segmentIds),
        [lockedSegment, segmentIds],
    );
    const segmentNames = React.useMemo(() => {
        const names = new Map((segments.data ?? []).map((seg) => [seg.id, seg.name]));
        if (lockedSegment && !names.has(lockedSegment.id)) names.set(lockedSegment.id, lockedSegment.name);
        return names;
    }, [segments.data, lockedSegment]);

    const mapProblem = draft ? mappingProblem(mapping.filter((m) => m.index < (draft.preview?.columns.length ?? 0))) : null;

    const analysis = useQuery({
        queryKey: [...CONTACT_IMPORTS_KEY, draft?.id, "analysis", hasHeader, mapping],
        queryFn: () => analyzeContactImport(draft!.id, { mapping, has_header: hasHeader }),
        enabled: open && step === "review" && !!draft && !mapProblem,
        staleTime: Infinity,
        gcTime: 5 * 60_000,
        placeholderData: keepPreviousData,
        retry: false,
    });

    const draftOptions = React.useMemo(
        () => ({
            mapping,
            dedup,
            has_header: hasHeader,
            subscribed_default: subscribedDefault,
            category_ids: categoryIds,
            campaign_ids: campaignIds,
            segment_ids: segmentIds,
        }),
        [mapping, dedup, hasHeader, subscribedDefault, categoryIds, campaignIds, segmentIds],
    );

    // Autosave: every change to a draft reaches the server shortly after it is made.
    React.useEffect(() => {
        if (!open || !draft || runId || resuming) return;
        const body = JSON.stringify(draftOptions);
        if (savedRef.current === "") {
            // The state just restored or created is what the server already has.
            savedRef.current = body;
            return;
        }
        if (body === savedRef.current) {
            // An edit undone before it was sent: the server already has this.
            setSaveState((s) => (s === "saving" ? "saved" : s));
            return;
        }
        setSaveState("saving");
        const timer = window.setTimeout(() => {
            saveContactImportDraft(draft.id, draftOptions)
                .then(() => {
                    savedRef.current = body;
                    setSaveState("saved");
                })
                .catch(() => setSaveState("error"));
        }, 700);
        return () => window.clearTimeout(timer);
    }, [open, draft, runId, resuming, draftOptions]);

    async function onFile(f: File) {
        if (!ACCEPTED.test(f.name)) {
            toast.error("That file type can't be imported. Use CSV, TSV or XLSX.");
            return;
        }
        if (f.size > MAX_IMPORT_UPLOAD_BYTES) {
            toast.error("The file is larger than 50 MB. Split it into smaller files.");
            return;
        }
        if (f.size === 0) {
            toast.error("The file is empty.");
            return;
        }
        setFileName(f.name);
        setUploading(true);
        setUploadFraction(0);
        try {
            const created = await createContactImport(f, (frac) => setUploadFraction(frac));
            setUploadFraction(null);
            resume(created);
        } catch (err) {
            toast.error(describeError(err, "The file could not be read."));
        } finally {
            setUploading(false);
            setUploadFraction(null);
        }
    }

    // A discarded draft is dropped on the server now rather than when it expires.
    const discardDraft = React.useCallback(() => {
        if (draft && !runId) {
            void cancelContactImport(draft.id)
                .catch(() => undefined)
                .finally(() => void queryClient.invalidateQueries({ queryKey: [...CONTACT_IMPORTS_KEY, "list"] }));
        }
    }, [draft, runId, queryClient]);

    function discardAndRestart() {
        confirm.show("Discard this import? Nothing has been imported from it.", async () => {
            discardDraft();
            reset();
        });
    }

    async function start() {
        if (!draft) return;
        setStarting(true);
        try {
            const imp = await startContactImport(draft.id, {
                mapping,
                dedup,
                has_header: hasHeader,
                subscribed_default: subscribedDefault,
                category_ids: categoryIds.length > 0 ? categoryIds : undefined,
                campaign_ids: lockedCampaign ? [lockedCampaign.id] : campaignIds.length > 0 ? campaignIds : undefined,
                segment_ids: targetSegmentIds.length > 0 ? targetSegmentIds : undefined,
            });
            queryClient.setQueryData([...CONTACT_IMPORTS_KEY, imp.id], imp);
            void queryClient.invalidateQueries({ queryKey: [...CONTACT_IMPORTS_KEY, "list"] });
            setRunId(imp.id);
            go("run");
        } catch (err) {
            toast.error(describeError(err, "The import could not start."));
        } finally {
            setStarting(false);
        }
    }

    const runningNow = !!running.data && isImportActive(running.data.status);
    const dirty = !!draft && !runId;

    // Closing never loses work: a draft stays saved, a run keeps running.
    const requestClose = React.useCallback(() => {
        if (uploading || starting) return;
        if (dirty) {
            const pending = JSON.stringify(draftOptions) !== savedRef.current;
            if (pending && draft) void saveContactImportDraft(draft.id, draftOptions).catch(() => undefined);
            toast("Draft saved. Continue it from Import within 24 hours.", { icon: "💾" });
            void queryClient.invalidateQueries({ queryKey: [...CONTACT_IMPORTS_KEY, "list"] });
        } else if (runningNow) {
            toast("The import keeps running. Reopen Import to check on it.", { icon: "⏳" });
        }
        onClose();
    }, [uploading, starting, dirty, draft, draftOptions, queryClient, onClose, runningNow]);

    React.useEffect(() => {
        if (!open) return;
        const onKey = (e: KeyboardEvent) => {
            if (e.key !== "Escape") return;
            // An open dropdown or the discard confirm owns this Escape.
            if (document.querySelector("[data-floating], [role='alertdialog']")) return;
            e.preventDefault();
            requestClose();
        };
        document.addEventListener("keydown", onKey);
        return () => document.removeEventListener("keydown", onKey);
    }, [open, requestClose]);

    function chooseAnotherFile() {
        confirm.show("Choose another file? This mapping is discarded.", async () => {
            discardDraft();
            reset();
        });
    }

    const reachable = (s: Step): boolean => {
        if (runId) return s === "run";
        if (s === "upload" || s === "run") return false;
        if (s === "map") return !!draft;
        return !!draft && !mapProblem;
    };

    const a = analysis.data;
    // The button says what will happen, not how many rows the file had.
    const importLabel = (() => {
        if (!a) return "Import";
        const updating = dedup !== "skip" && a.existing > 0;
        if (a.new === 0) return updating ? `Update ${a.existing.toLocaleString()} contacts` : "Add existing contacts";
        return `Import ${a.new.toLocaleString()} new${updating ? ` · update ${a.existing.toLocaleString()}` : ""}`;
    })();
    const reviewBlocked = !a || !!a.problem || analysis.isFetching;
    const reviewHint = analysis.isFetching
        ? "Checking the file…"
        : a?.problem
          ? "Resolve the problem above to continue."
          : a && a.new + a.existing === 0
            ? "No row in this file can be imported."
            : null;

    return (
        <AnimatePresence>
            {open && (
                <motion.div
                    key="overlay"
                    initial={{ opacity: 0 }}
                    animate={{ opacity: 1 }}
                    exit={{ opacity: 0 }}
                    transition={{ duration: 0.15 }}
                    onMouseDown={requestClose}
                    className="fixed inset-0 z-[120] flex items-end sm:items-center justify-center bg-slate-900/30 backdrop-blur-[2px] sm:px-4"
                >
                    <motion.div
                        key="card"
                        role="dialog"
                        aria-modal="true"
                        aria-label="Import contacts"
                        initial={{ y: 8, opacity: 0, scale: 0.985 }}
                        animate={{ y: 0, opacity: 1, scale: 1 }}
                        exit={{ y: 8, opacity: 0, scale: 0.985 }}
                        transition={{ duration: 0.18, ease: [0.22, 1, 0.36, 1] }}
                        onMouseDown={(e) => e.stopPropagation()}
                        className="w-full max-w-[920px] rounded-t-lg sm:rounded-lg bg-white border border-slate-200 shadow-[0_24px_48px_-12px_rgba(15,23,42,0.18),0_8px_16px_-8px_rgba(15,23,42,0.1)] overflow-hidden flex flex-col h-[92dvh] sm:h-auto sm:max-h-[90dvh]"
                    >
                        <header className="h-12 px-4 border-b border-slate-200 flex items-center gap-2.5 shrink-0">
                            <div className="size-5 rounded bg-slate-100 text-slate-600 flex items-center justify-center">
                                <UploadCloudIcon className="w-3 h-3" />
                            </div>
                            <span className="text-[10px] uppercase tracking-[0.14em] text-slate-400 font-medium">Import</span>
                            <div className="h-4 w-px bg-slate-200" />
                            <span className="text-[12.5px] text-slate-900 font-medium">Contacts</span>
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
                            <button
                                type="button"
                                onClick={requestClose}
                                aria-label="Close"
                                className="ml-auto size-7 rounded-md text-slate-500 hover:text-slate-900 hover:bg-slate-100 inline-flex items-center justify-center transition-colors"
                            >
                                <XIcon className="w-3.5 h-3.5" />
                            </button>
                        </header>

                        <Stepper step={step} reachable={reachable} onGo={go} runDone={!!running.data && !runningNow} />

                        <div className="flex-1 min-h-0 overflow-y-auto overflow-x-hidden">
                            <AnimatePresence mode="wait" initial={false} custom={direction}>
                                <motion.div
                                    key={step}
                                    custom={direction}
                                    variants={paneVariants}
                                    initial="enter"
                                    animate="center"
                                    exit="exit"
                                    transition={{ duration: 0.18, ease: [0.22, 1, 0.36, 1] }}
                                    className="px-4 sm:px-5 py-4 min-h-[360px]"
                                >
                                    {!resuming && step === "upload" && (
                                        <UploadStep
                                            onFile={onFile}
                                            busy={uploading}
                                            uploadFraction={uploadFraction}
                                            fileName={fileName}
                                            onOpenImport={(imp) => resume(imp)}
                                        />
                                    )}
                                    {resuming && (
                                        <div className="py-16 flex flex-col items-center gap-2 text-slate-500">
                                            <Loader2Icon className="w-5 h-5 animate-spin" />
                                            <span className="text-[12px]">Picking up where you left off…</span>
                                        </div>
                                    )}
                                    {!resuming && step === "map" && draft?.preview && (
                                        <MapStep
                                            importId={draft.id}
                                            preview={draft.preview}
                                            mapping={mapping}
                                            setMapping={setMapping}
                                            hasHeader={hasHeader}
                                            setHasHeader={setHasHeader}
                                        />
                                    )}
                                    {!resuming && step === "review" && (
                                        <ReviewStep
                                            analysis={analysis.data}
                                            analysisLoading={analysis.isFetching}
                                            analysisError={analysis.error}
                                            onRetryAnalysis={() => void analysis.refetch()}
                                            fileName={draft?.filename ?? fileName}
                                            dedup={dedup}
                                            setDedup={setDedup}
                                            subscribedDefault={subscribedDefault}
                                            setSubscribedDefault={setSubscribedDefault}
                                            categoryIds={categoryIds}
                                            setCategoryIds={setCategoryIds}
                                            campaignIds={campaignIds}
                                            setCampaignIds={setCampaignIds}
                                            segmentIds={segmentIds}
                                            setSegmentIds={setSegmentIds}
                                            lockedCampaign={lockedCampaign}
                                            lockedSegment={lockedSegment}
                                        />
                                    )}
                                    {!resuming && step === "run" && runId && <RunStep importId={runId} segmentNames={segmentNames} />}
                                </motion.div>
                            </AnimatePresence>
                        </div>

                        <footer className="min-h-14 py-2 px-3 sm:px-4 border-t border-slate-200 flex flex-wrap items-center gap-1.5 shrink-0 bg-white">
                            {step === "upload" && (
                                <>
                                    <span className="text-[11px] text-slate-400">Nothing is imported until you confirm on the review step.</span>
                                    <button
                                        type="button"
                                        onClick={requestClose}
                                        disabled={uploading}
                                        className="ml-auto h-7 px-2.5 rounded-md text-[12px] text-slate-700 hover:text-slate-900 hover:bg-slate-100 transition-colors disabled:opacity-50"
                                    >
                                        Cancel
                                    </button>
                                </>
                            )}
                            {step === "map" && (
                                <>
                                    <button
                                        type="button"
                                        onClick={chooseAnotherFile}
                                        className="h-7 px-2.5 rounded-md text-[12px] text-slate-600 hover:text-slate-900 hover:bg-slate-100 inline-flex items-center gap-1.5 transition-colors"
                                    >
                                        <ArrowLeftIcon className="w-3 h-3" />
                                        Another file
                                    </button>
                                    <DiscardButton onClick={discardAndRestart} />
                                    <SaveIndicator state={saveState} />
                                    <FooterHint text={mapProblem} />
                                    <button
                                        type="button"
                                        onClick={() => go("review")}
                                        disabled={!!mapProblem}
                                        className={`ml-auto ${FOOTER_BUTTON} ${NEXT_BUTTON}`}
                                    >
                                        Review
                                        <ArrowRightIcon className="w-3 h-3" />
                                    </button>
                                </>
                            )}
                            {step === "review" && (
                                <>
                                    <button
                                        type="button"
                                        onClick={() => go("map")}
                                        disabled={starting}
                                        className="h-7 px-2.5 rounded-md text-[12px] text-slate-600 hover:text-slate-900 hover:bg-slate-100 inline-flex items-center gap-1.5 transition-colors disabled:opacity-50"
                                    >
                                        <ArrowLeftIcon className="w-3 h-3" />
                                        Back
                                    </button>
                                    <DiscardButton onClick={discardAndRestart} />
                                    <SaveIndicator state={saveState} />
                                    <FooterHint text={reviewHint} tone={analysis.isFetching ? "slate" : "amber"} />
                                    <button
                                        type="button"
                                        onClick={start}
                                        disabled={starting || reviewBlocked || (!!a && a.new + a.existing === 0)}
                                        className={`ml-auto ${FOOTER_BUTTON} ${PRIMARY_BUTTON}`}
                                    >
                                        {starting ? <Loader2Icon className="w-3 h-3 animate-spin" /> : <RocketIcon className="w-3 h-3" />}
                                        {importLabel}
                                    </button>
                                </>
                            )}
                            {step === "run" && (
                                <>
                                    {!runningNow && (
                                        <button
                                            type="button"
                                            onClick={reset}
                                            className="h-7 px-2.5 rounded-md text-[12px] text-slate-600 hover:text-slate-900 hover:bg-slate-100 inline-flex items-center gap-1.5 transition-colors"
                                        >
                                            <UploadCloudIcon className="w-3 h-3" />
                                            Import another file
                                        </button>
                                    )}
                                    <button
                                        type="button"
                                        onClick={requestClose}
                                        className={`ml-auto ${FOOTER_BUTTON} ${NEXT_BUTTON}`}
                                    >
                                        {runningNow ? "Keep running in background" : "Done"}
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

function DiscardButton({ onClick }: { onClick: () => void }) {
    return (
        <button
            type="button"
            onClick={onClick}
            title="Discard this import"
            className="h-7 px-2 rounded-md text-[12px] text-slate-500 hover:text-red-700 hover:bg-red-50 inline-flex items-center gap-1.5 transition-colors"
        >
            <Trash2Icon className="w-3 h-3" />
            <span className="hidden sm:inline">Discard</span>
        </button>
    );
}

// SaveIndicator says whether the draft on screen is safe to leave.
function SaveIndicator({ state }: { state: SaveState }) {
    if (state === "idle") return null;
    const view = {
        saving: { icon: <Loader2Icon className="w-3 h-3 animate-spin" />, text: "Saving…", tone: "text-slate-400" },
        saved: { icon: <CloudCheckIcon className="w-3 h-3" />, text: "Draft saved", tone: "text-emerald-600" },
        error: { icon: <CloudCheckIcon className="w-3 h-3" />, text: "Not saved; retrying on the next change", tone: "text-amber-700" },
    }[state];
    return (
        <span className={`hidden sm:inline-flex items-center gap-1 text-[11px] ${view.tone}`} title="Reload or close any time; the import picks up where it was.">
            {view.icon}
            {view.text}
        </span>
    );
}

function FooterHint({ text, tone = "amber" }: { text: string | null; tone?: "amber" | "slate" }) {
    return (
        <AnimatePresence initial={false}>
            {text && (
                <motion.span
                    key={text}
                    initial={{ opacity: 0, y: 3 }}
                    animate={{ opacity: 1, y: 0 }}
                    exit={{ opacity: 0 }}
                    className={`text-[11px] inline-flex items-center gap-1 min-w-0 ${tone === "amber" ? "text-amber-700" : "text-slate-500"}`}
                >
                    {tone === "slate" && <Loader2Icon className="w-3 h-3 animate-spin shrink-0" />}
                    <span className="truncate">{text}</span>
                </motion.span>
            )}
        </AnimatePresence>
    );
}

function Stepper({
    step,
    reachable,
    onGo,
    runDone,
}: {
    step: Step;
    reachable: (s: Step) => boolean;
    onGo: (s: Step) => void;
    runDone: boolean;
}) {
    const current = STEPS.findIndex((s) => s.key === step);
    return (
        <nav aria-label="Import steps" className="shrink-0 px-3 sm:px-4 h-10 border-b border-slate-200 flex items-center gap-1 overflow-x-auto">
            {STEPS.map((s, i) => {
                const done = i < current || (s.key === "run" && runDone);
                const active = i === current;
                const canGo = !active && reachable(s.key);
                return (
                    <React.Fragment key={s.key}>
                        {i > 0 && <span className={`h-px w-4 sm:w-8 shrink-0 ${i <= current ? "bg-sky-300" : "bg-slate-200"}`} />}
                        <button
                            type="button"
                            disabled={!canGo}
                            onClick={() => onGo(s.key)}
                            aria-current={active ? "step" : undefined}
                            className={`h-7 px-2 rounded-md inline-flex items-center gap-1.5 text-[12px] shrink-0 transition-colors ${
                                active ? "text-slate-900 font-medium bg-slate-100" : done ? "text-slate-600" : "text-slate-400"
                            } ${canGo ? "hover:bg-slate-50 hover:text-slate-900" : "cursor-default"}`}
                        >
                            <span
                                className={`size-4 rounded-full flex items-center justify-center shrink-0 ${
                                    done ? "bg-sky-600 text-white" : active ? "bg-slate-900 text-white" : "bg-slate-200 text-slate-500"
                                }`}
                            >
                                {done ? <CheckIcon className="w-2.5 h-2.5" /> : <s.icon className="w-2.5 h-2.5" />}
                            </span>
                            <span className={active ? "" : "hidden sm:inline"}>{s.label}</span>
                        </button>
                    </React.Fragment>
                );
            })}
        </nav>
    );
}
