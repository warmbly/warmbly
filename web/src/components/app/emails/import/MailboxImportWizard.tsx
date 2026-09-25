// MailboxImportWizard: the "Import mailboxes" route of the connect modal, and
// the result screen reopened from the mailboxes page.
//
// Source (file or pasted list) -> Columns (skipped when a saved mapping or a
// known vendor export covers every column) -> Review -> Import. The server
// reads the list, detects each domain's host and settings, and runs the job;
// this component only holds the choices until the job exists.

import React from "react";
import { AnimatePresence, motion } from "framer-motion";
import { AlertCircleIcon, ChevronLeftIcon, ChevronRightIcon, Loader2Icon, UploadIcon } from "lucide-react";
import toast from "react-hot-toast";
import { usePreviewMailboxImport, useCreateMailboxImport } from "@/lib/api/hooks/app/emails/useMailboxImportActions";
import type {
    ImportMapping,
    MailboxImportInput,
    MailboxImportPreview,
    MailboxImportSettings,
    OnExisting,
} from "@/lib/api/models/app/emails/MailboxImport";
import { isAllowanceError } from "@/hooks/useMailboxOAuth";
import type { AppError } from "@/lib/api/client/normalizeError";
import buildError from "@/lib/helper/buildError";
import { cn } from "@/lib/utils";
import { mappedAutomatically, mappingFromColumns, mappingProblem } from "./importFields";
import SourceStep from "./SourceStep";
import MapStep from "./MapStep";
import ReviewStep from "./ReviewStep";
import RunStep from "./RunStep";
import { dnsFollowUps, domainChoiceIssue, domainOptions, emptyPicks, type DomainPicks } from "./domainChoiceRules";
import { Stepper, WizardPanes } from "./wizard";

type StepKey = "source" | "map" | "review" | "run";

const STEPS: { key: StepKey; label: string }[] = [
    { key: "source", label: "Source" },
    { key: "map", label: "Columns" },
    { key: "review", label: "Review" },
    { key: "run", label: "Import" },
];

export default function MailboxImportWizard({
    onDone,
    onAllowance,
    onDirtyChange,
    importId: initialImportId,
}: {
    onDone: () => void;
    /** Opens the mailbox allowance dialog. */
    onAllowance?: () => void;
    /** True while a list is loaded that has not been imported yet. */
    onDirtyChange?: (dirty: boolean) => void;
    /** Opens an existing import at its result screen. */
    importId?: string;
}) {
    const [step, setStep] = React.useState<StepKey>(initialImportId ? "run" : "source");
    const [direction, setDirection] = React.useState<1 | -1>(1);
    const [nudged, setNudged] = React.useState(false);

    const [file, setFile] = React.useState<File | null>(null);
    const [pasteText, setPasteText] = React.useState("");
    const [preview, setPreview] = React.useState<MailboxImportPreview | null>(null);
    const [previewing, setPreviewing] = React.useState(false);
    const [previewError, setPreviewError] = React.useState<string | null>(null);

    const [mapping, setMapping] = React.useState<ImportMapping>({});
    const [mappingTouched, setMappingTouched] = React.useState(false);
    const [hasHeader, setHasHeader] = React.useState<boolean | null>(null);
    const [sharedPassword, setSharedPassword] = React.useState("");
    const [autoMapped, setAutoMapped] = React.useState(false);

    const [onExisting, setOnExisting] = React.useState<OnExisting>("update");
    const [settings, setSettings] = React.useState<MailboxImportSettings>({});
    const [domainPicks, setDomainPicks] = React.useState<DomainPicks>(emptyPicks);
    const [dnsLeft, setDnsLeft] = React.useState(0);
    const [importId, setImportId] = React.useState<string | null>(initialImportId ?? null);

    const { mutateAsync: previewAsync } = usePreviewMailboxImport();
    const create = useCreateMailboxImport();

    const text = pasteText.trim();
    const hasSource = !!file || text !== "";
    const dirty = !importId && hasSource;
    React.useEffect(() => {
        onDirtyChange?.(dirty);
    }, [dirty, onDirtyChange]);

    // Only the newest preview may land; an older answer arriving late is dropped.
    const seq = React.useRef(0);
    const runPreview = React.useCallback(
        async (input: MailboxImportInput): Promise<MailboxImportPreview | null> => {
            const id = ++seq.current;
            setPreviewing(true);
            setPreviewError(null);
            try {
                const p = await previewAsync(input);
                if (id !== seq.current) return null;
                setPreview(p);
                return p;
            } catch (e) {
                if (id === seq.current) setPreviewError(buildError(e as AppError));
                return null;
            } finally {
                if (id === seq.current) setPreviewing(false);
            }
        },
        [previewAsync],
    );

    // A fresh list starts from the server's own reading of it.
    const adopt = React.useCallback((p: MailboxImportPreview) => {
        const m = p.mapping && Object.keys(p.mapping).length > 0 ? p.mapping : mappingFromColumns(p.columns);
        setMapping(m);
        setMappingTouched(false);
        setHasHeader(null);
        setAutoMapped(mappedAutomatically(p));
    }, []);

    const go = React.useCallback((target: StepKey) => {
        const from = STEPS.findIndex((s) => s.key === step);
        const to = STEPS.findIndex((s) => s.key === target);
        setDirection(to >= from ? 1 : -1);
        setNudged(false);
        // Opening the columns by hand makes them part of the way back from review.
        if (target === "map") setAutoMapped(false);
        setStep(target);
    }, [step]);

    async function onFile(f: File) {
        setFile(f);
        setPasteText("");
        setSharedPassword("");
        setPreview(null);
        const p = await runPreview({ file: f, options: { has_header: null } });
        if (!p) return;
        adopt(p);
        if (p.summary.total === 0) return;
        go(mappedAutomatically(p) ? "review" : "map");
    }

    // A pasted list is read as it is typed, so the counts under the box stay current.
    // Coming back to this step with the same text keeps the mapping made for it.
    const readText = React.useRef("");
    React.useEffect(() => {
        if (file || step !== "source" || text === readText.current) return;
        if (!text) {
            readText.current = "";
            seq.current++;
            setPreview(null);
            setPreviewing(false);
            setPreviewError(null);
            return;
        }
        const t = window.setTimeout(() => {
            readText.current = text;
            void runPreview({ text, options: { has_header: null } }).then((p) => {
                if (p) adopt(p);
            });
        }, 500);
        return () => window.clearTimeout(t);
    }, [text, file, step, runPreview, adopt]);

    // Mapping, header and shared password edits re-read the list, debounced.
    const [revision, setRevision] = React.useState(0);
    const latest = React.useRef({ file, text, mapping, mappingTouched, hasHeader, sharedPassword });
    React.useEffect(() => {
        latest.current = { file, text, mapping, mappingTouched, hasHeader, sharedPassword };
    });
    React.useEffect(() => {
        if (revision === 0) return;
        const t = window.setTimeout(() => {
            const l = latest.current;
            void runPreview({
                ...(l.file ? { file: l.file } : { text: l.text }),
                mapping: l.mappingTouched ? l.mapping : undefined,
                options: { has_header: l.hasHeader, shared_password: l.sharedPassword || undefined },
            });
        }, 450);
        return () => window.clearTimeout(t);
    }, [revision, runPreview]);
    const bump = () => setRevision((r) => r + 1);

    const summary = preview?.summary;
    const importCount = summary ? summary.ready + summary.needs_signin + (onExisting === "update" ? summary.existing : 0) : 0;
    const allowance = preview?.allowance;
    const allowanceFull = !!allowance && allowance.allowance != null && (allowance.remaining ?? 0) <= 0;

    function stepIssue(key: StepKey): string | null {
        if (key === "source") {
            if (!hasSource) return "Drop a file or paste a list first.";
            if (!file && text !== readText.current) return "Still reading the list.";
            if (previewing && !preview) return "Still reading the list.";
            if (previewError && !preview) return previewError;
            if (!preview) return "Still reading the list.";
            if (preview.summary.total === 0) return "No mailboxes found in this list.";
            return null;
        }
        if (key === "map") return mappingProblem(mapping, sharedPassword) ?? previewError;
        if (key === "review") {
            if (previewing) return "Still checking the list.";
            if (previewError) return previewError;
            if (importCount === 0) return "Nothing to import: no row is ready. Fix the issues above or change the columns.";
            if (allowanceFull && importCount === (summary?.ready ?? 0) + (summary?.needs_signin ?? 0))
                return "Your mailbox allowance is full. Request more first.";
            return domainChoiceIssue(preview?.domains ?? [], domainPicks);
        }
        return null;
    }

    const order = STEPS.map((s) => s.key);
    const at = order.indexOf(step);
    const reachable = (target: StepKey) => {
        if (importId) return target === "run";
        const t = order.indexOf(target);
        if (target === "run") return false;
        for (let i = 0; i < t; i++) if (stepIssue(order[i])) return false;
        return true;
    };
    const issue = stepIssue(step);

    function next() {
        if (issue) {
            setNudged(true);
            return;
        }
        if (step === "source") go(autoMapped ? "review" : "map");
        else if (step === "map") go("review");
    }

    function back() {
        if (step === "review") go(autoMapped ? "source" : "map");
        else if (step === "map") go("source");
    }

    async function submit() {
        if (issue) {
            setNudged(true);
            return;
        }
        if (create.isPending) return;
        try {
            const job = await create.mutateAsync({
                ...(file ? { file } : { text }),
                mapping,
                options: {
                    has_header: hasHeader,
                    shared_password: sharedPassword || undefined,
                    on_existing: onExisting,
                    save_mapping: true,
                    settings: Object.keys(settings).length > 0 ? settings : undefined,
                    ...domainOptions(preview?.domains ?? [], domainPicks),
                },
            });
            setDnsLeft(dnsFollowUps(preview?.domains ?? [], domainPicks));
            setImportId(job.id);
            go("run");
        } catch (e) {
            if (isAllowanceError(e) && onAllowance) {
                onAllowance();
                return;
            }
            toast.error(buildError(e as AppError));
        }
    }

    function startOver() {
        seq.current++;
        readText.current = "";
        setFile(null);
        setPasteText("");
        setPreview(null);
        setPreviewError(null);
        setPreviewing(false);
        setMapping({});
        setMappingTouched(false);
        setHasHeader(null);
        setSharedPassword("");
        setAutoMapped(false);
        setOnExisting("update");
        setSettings({});
        setDomainPicks(emptyPicks());
        setDnsLeft(0);
        setImportId(null);
        go("source");
    }

    return (
        <div className="flex flex-col">
            <Stepper
                steps={STEPS}
                at={at}
                canClick={(key, i) => !importId && key !== "run" && (i < at || reachable(key))}
                goTo={go}
                titleFor={(key) => (key === "map" && autoMapped ? "Mapped automatically" : undefined)}
            />

            <WizardPanes stepKey={step} direction={direction}>
                {step === "source" && (
                    <SourceStep
                        file={file}
                        pasteText={pasteText}
                        onPaste={(v) => {
                            if (file) setFile(null);
                            setPasteText(v);
                        }}
                        onFile={(f) => void onFile(f)}
                        onClearFile={startOver}
                        preview={preview}
                        previewing={previewing}
                        previewError={previewError}
                        onAllowance={onAllowance}
                    />
                )}
                {step === "map" && preview && (
                    <MapStep
                        preview={preview}
                        mapping={mapping}
                        onMapping={(m) => {
                            setMapping(m);
                            setMappingTouched(true);
                            bump();
                        }}
                        hasHeader={hasHeader ?? preview.has_header}
                        onHasHeader={(v) => {
                            setHasHeader(v);
                            bump();
                        }}
                        sharedPassword={sharedPassword}
                        onSharedPassword={(v) => {
                            setSharedPassword(v);
                            bump();
                        }}
                        previewing={previewing}
                    />
                )}
                {step === "review" && preview && (
                    <ReviewStep
                        preview={preview}
                        previewing={previewing}
                        autoMapped={autoMapped}
                        onEditMapping={() => go("map")}
                        onExisting={onExisting}
                        setOnExisting={setOnExisting}
                        settings={settings}
                        setSettings={setSettings}
                        domainPicks={domainPicks}
                        setDomainPicks={setDomainPicks}
                        onAllowance={onAllowance}
                    />
                )}
                {step === "run" && importId && (
                    <RunStep
                        importId={importId}
                        onDone={onDone}
                        onAllowance={onAllowance}
                        onImportAnother={startOver}
                        dnsFollowUps={dnsLeft}
                    />
                )}
            </WizardPanes>

            {step !== "run" && (
                <div className="px-3 min-h-12 py-1.5 sm:py-0 sm:h-12 border-t border-slate-200 flex items-center gap-1.5 bg-slate-50/80 backdrop-blur-sm sticky bottom-0 z-[2]">
                    {step !== "source" ? (
                        <button
                            type="button"
                            onClick={back}
                            disabled={create.isPending}
                            className="h-7 px-2.5 rounded-md text-[12px] text-slate-700 hover:text-slate-900 hover:bg-slate-100 inline-flex items-center gap-1 transition-colors disabled:opacity-50"
                        >
                            <ChevronLeftIcon className="w-3 h-3" />
                            Back
                        </button>
                    ) : (
                        <span className="text-[11px] text-slate-400 pl-1 hidden sm:inline">Passwords are encrypted and never shown again.</span>
                    )}
                    <div className="ml-auto flex items-center gap-2.5 min-w-0">
                        <AnimatePresence initial={false}>
                            {nudged && issue && (
                                <motion.span
                                    key={issue}
                                    initial={{ opacity: 0, x: 6 }}
                                    animate={{ opacity: 1, x: 0 }}
                                    exit={{ opacity: 0, x: 6 }}
                                    transition={{ duration: 0.14 }}
                                    role="status"
                                    className="text-[11.5px] text-amber-700 inline-flex items-center gap-1 min-w-0"
                                >
                                    <AlertCircleIcon className="w-3 h-3 shrink-0" />
                                    <span className="truncate">{issue}</span>
                                </motion.span>
                            )}
                        </AnimatePresence>
                        {step === "review" ? (
                            <motion.button
                                type="button"
                                onClick={() => void submit()}
                                whileTap={!issue && !create.isPending ? { scale: 0.97 } : undefined}
                                aria-disabled={!!issue || create.isPending}
                                className={cn(
                                    "shrink-0 h-7 px-3 rounded-md text-[12px] font-medium inline-flex items-center gap-1.5 transition-colors bg-slate-900 hover:bg-slate-800 text-white",
                                    (issue || create.isPending) && "opacity-50",
                                )}
                            >
                                {create.isPending ? <Loader2Icon className="w-3 h-3 animate-spin" /> : <UploadIcon className="w-3 h-3" />}
                                Import {importCount.toLocaleString()} {importCount === 1 ? "mailbox" : "mailboxes"}
                            </motion.button>
                        ) : (
                            <button
                                type="button"
                                onClick={next}
                                aria-disabled={!!issue}
                                className={cn(
                                    "shrink-0 h-7 px-3 rounded-md bg-slate-900 hover:bg-slate-800 text-white text-[12px] font-medium inline-flex items-center gap-1 transition-colors",
                                    issue && "opacity-50",
                                )}
                            >
                                {previewing && step === "source" ? <Loader2Icon className="w-3 h-3 animate-spin" /> : null}
                                Continue
                                <ChevronRightIcon className="w-3 h-3" />
                            </button>
                        )}
                    </div>
                </div>
            )}
        </div>
    );
}
