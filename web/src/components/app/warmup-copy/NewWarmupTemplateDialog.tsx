// New warmup template: name, subject, body and segment, with a live analysis
// of the draft from the check endpoint. The server decides what is accepted.
import React from "react";
import { AnimatePresence, motion } from "framer-motion";
import { FileTextIcon, OctagonAlertIcon, XIcon } from "lucide-react";
import toast from "react-hot-toast/headless";
import { Label, TextInput } from "@/components/ui/field";
import { Loading } from "@/components/loader";
import { useConfirm } from "@/hooks/context/confirm";
import useDebouncedValue from "@/hooks/useDebouncedValue";
import { useCreateWarmupTemplate, useWarmupTemplateCheck } from "@/lib/api/hooks/app/warmup/useWarmupCopy";
import type { WarmupTemplateCheck } from "@/lib/api/models/app/warmup/WarmupCopy";
import type { AppError } from "@/lib/api/client/normalizeError";
import buildError from "@/lib/helper/buildError";

const MAX_SUBJECT = 200;
const MAX_FIELD = 200;

// A refusal the person can fix (blocked or duplicate copy) reads as the server wrote it.
function submitError(err: AppError): string {
    if (err.status === 400 || err.status === 409) return err.message;
    return buildError(err);
}

export default function NewWarmupTemplateDialog({ open, onClose }: { open: boolean; onClose: () => void }) {
    const create = useCreateWarmupTemplate();
    const confirm = useConfirm();
    const [name, setName] = React.useState("");
    const [subject, setSubject] = React.useState("");
    const [body, setBody] = React.useState("");
    const [segment, setSegment] = React.useState("");
    const [error, setError] = React.useState<string | null>(null);

    const busy = create.isPending;
    const dirty = name.trim() !== "" || subject.trim() !== "" || body.trim() !== "" || segment.trim() !== "";
    const ready = subject.trim() !== "" && body.trim() !== "";

    React.useEffect(() => {
        if (!open) return;
        setName("");
        setSubject("");
        setBody("");
        setSegment("");
        setError(null);
    }, [open]);

    // Memoized so the debounce sees a new value only when the text changes; a half-written draft is not checked.
    const draft = React.useMemo(
        () => (open && subject.trim() && body.trim() ? { subject, body } : null),
        [open, subject, body],
    );
    const debounced = useDebouncedValue(draft, 400);
    const check = useWarmupTemplateCheck(debounced);

    const requestClose = React.useCallback(() => {
        if (busy) return;
        if (!dirty) {
            onClose();
            return;
        }
        confirm.show("Discard this template? Nothing has been saved yet.", async () => onClose());
    }, [busy, dirty, onClose, confirm]);

    React.useEffect(() => {
        if (!open) return;
        const onKey = (e: KeyboardEvent) => {
            if (e.key !== "Escape") return;
            if (document.querySelector("[data-floating], [role='alertdialog']")) return;
            e.preventDefault();
            requestClose();
        };
        document.addEventListener("keydown", onKey);
        return () => document.removeEventListener("keydown", onKey);
    }, [open, requestClose]);

    async function submit() {
        if (!ready || busy) return;
        setError(null);
        try {
            await create.mutateAsync({ name: name.trim(), subject: subject.trim(), body: body.trim(), segment: segment.trim() });
            toast.success("Template added");
            onClose();
        } catch (err) {
            setError(submitError(err as AppError));
        }
    }

    return (
        <AnimatePresence>
            {open && (
                <>
                    <motion.div
                        initial={{ opacity: 0 }}
                        animate={{ opacity: 1 }}
                        exit={{ opacity: 0 }}
                        transition={{ duration: 0.18 }}
                        className="fixed inset-0 z-50 bg-slate-900/40"
                        onMouseDown={requestClose}
                    />
                    <div className="fixed inset-0 z-50 flex items-center justify-center p-4 pointer-events-none">
                        <motion.form
                            role="dialog"
                            aria-modal="true"
                            aria-labelledby="new-warmup-template-title"
                            initial={{ opacity: 0, scale: 0.97, y: 8 }}
                            animate={{ opacity: 1, scale: 1, y: 0 }}
                            exit={{ opacity: 0, scale: 0.97, y: 8 }}
                            transition={{ duration: 0.2 }}
                            onMouseDown={(e) => e.stopPropagation()}
                            onSubmit={(e) => {
                                e.preventDefault();
                                void submit();
                            }}
                            className="pointer-events-auto w-full max-w-[820px] max-h-[90dvh] flex flex-col rounded-lg border border-slate-200 bg-white shadow-[0_24px_60px_-12px_rgba(15,23,42,0.35)]"
                        >
                            <div className="px-5 h-14 flex items-center gap-3 border-b border-slate-200 shrink-0">
                                <div className="w-8 h-8 rounded-lg bg-sky-50 text-sky-700 flex items-center justify-center shrink-0">
                                    <FileTextIcon className="w-4 h-4" />
                                </div>
                                <div className="min-w-0 flex-1">
                                    <div id="new-warmup-template-title" className="text-[13px] font-medium text-slate-900">
                                        New warmup template
                                    </div>
                                    <div className="text-[11px] text-slate-400">Copy you plan to send, carried by your warmup pool</div>
                                </div>
                                <button
                                    type="button"
                                    onClick={requestClose}
                                    disabled={busy}
                                    aria-label="Close"
                                    className="w-7 h-7 rounded-md flex items-center justify-center text-slate-400 hover:text-slate-900 hover:bg-slate-100 transition-colors disabled:opacity-50"
                                >
                                    <XIcon className="w-4 h-4" />
                                </button>
                            </div>

                            <div className="flex-1 min-h-0 overflow-y-auto md:grid md:grid-cols-[1fr_260px]">
                                <div className="px-5 py-4 space-y-4 min-w-0">
                                    <div>
                                        <Label>Name</Label>
                                        <TextInput value={name} onChange={setName} placeholder="Q3 intro" maxLength={MAX_FIELD} className="w-full" autoFocus />
                                        <p className="mt-1 text-[11px] text-slate-500">For your team. Recipients never see it.</p>
                                    </div>
                                    <div>
                                        <Label>Subject</Label>
                                        <TextInput
                                            value={subject}
                                            onChange={setSubject}
                                            placeholder="Quick question about your onboarding"
                                            maxLength={MAX_SUBJECT}
                                            className="w-full"
                                        />
                                    </div>
                                    <div>
                                        <Label>Body</Label>
                                        <textarea
                                            value={body}
                                            onChange={(e) => setBody(e.target.value)}
                                            onKeyDown={(e) => {
                                                if (e.key === "Enter" && (e.metaKey || e.ctrlKey)) {
                                                    e.preventDefault();
                                                    void submit();
                                                }
                                            }}
                                            rows={10}
                                            placeholder="Write it the way you would send it to a prospect."
                                            className="w-full rounded-md border border-slate-200 bg-white px-3 py-2 text-[16px] md:text-[12.5px] leading-5 text-slate-900 placeholder:text-slate-400 outline-none transition-colors focus:border-sky-400 focus:ring-2 focus:ring-sky-100 resize-y"
                                        />
                                        <p className="mt-1 text-[11px] text-slate-500">Plain text. Warmup sends it exactly as written.</p>
                                    </div>
                                    <div>
                                        <Label>Segment (optional)</Label>
                                        <TextInput value={segment} onChange={setSegment} placeholder="saas" maxLength={MAX_FIELD} className="w-full" />
                                        <p className="mt-1 text-[11px] text-slate-500">
                                            Only mailboxes with this warmup tag send it. Leave blank for every mailbox.
                                        </p>
                                    </div>
                                </div>

                                <aside className="px-5 py-4 border-t md:border-t-0 md:border-l border-slate-200 bg-slate-50/50 min-w-0">
                                    <AnalysisPanel
                                        result={debounced ? check.data : undefined}
                                        loading={check.isFetching}
                                        error={check.isError ? buildError(check.error as unknown as AppError) : null}
                                    />
                                </aside>
                            </div>

                            {error && (
                                <div role="alert" className="mx-5 mb-3 flex items-start gap-2 rounded-md border border-rose-200 bg-rose-50 px-3 py-2 text-[12px] text-rose-700">
                                    <OctagonAlertIcon className="w-3.5 h-3.5 shrink-0 mt-0.5" />
                                    <span className="min-w-0 break-words">{error}</span>
                                </div>
                            )}

                            <div className="px-5 h-14 flex items-center justify-between gap-2 border-t border-slate-200 bg-slate-50/60 shrink-0">
                                <span className="text-[11px] text-slate-400 hidden sm:inline">
                                    {ready ? "Ctrl/⌘ + Enter to add" : "A subject and a body are required"}
                                </span>
                                <div className="flex items-center gap-2 ml-auto">
                                    <button
                                        type="button"
                                        onClick={requestClose}
                                        disabled={busy}
                                        className="h-8 px-3 rounded-md border border-slate-200 hover:border-slate-300 text-[12px] text-slate-700 hover:text-slate-900 transition-colors disabled:opacity-50"
                                    >
                                        Cancel
                                    </button>
                                    <button
                                        type="submit"
                                        disabled={busy || !ready}
                                        title={ready ? undefined : "Add a subject and a body first"}
                                        className="h-8 px-3.5 rounded-md bg-sky-600 hover:bg-sky-700 text-white text-[12px] font-medium inline-flex items-center gap-1.5 transition-colors disabled:opacity-60"
                                    >
                                        {busy && <Loading className="!w-3.5 h-3.5 text-white" />}
                                        Add template
                                    </button>
                                </div>
                            </div>
                        </motion.form>
                    </div>
                </>
            )}
        </AnimatePresence>
    );
}

function AnalysisPanel({ result, loading, error }: { result: WarmupTemplateCheck | undefined; loading: boolean; error: string | null }) {
    const hits = result?.spam_trigger_hits ?? [];
    return (
        <div className="space-y-3">
            <div className="flex items-center gap-2">
                <span className="text-[10px] uppercase tracking-[0.14em] text-slate-400 font-medium">Analysis</span>
                {loading && <Loading className="!w-3 h-3 text-slate-400" />}
            </div>

            {error ? (
                <p className="text-[11.5px] text-rose-600">{error}</p>
            ) : !result ? (
                <p className="text-[11.5px] text-slate-400 leading-relaxed">Write a subject and a body and the draft is checked as you go.</p>
            ) : (
                <>
                    {result.blocked_reason ? (
                        <div className="rounded-md border border-rose-200 bg-rose-50 px-2.5 py-2">
                            <div className="flex items-center gap-1.5 text-[11.5px] font-medium text-rose-700">
                                <OctagonAlertIcon className="w-3.5 h-3.5" />
                                Would be blocked
                            </div>
                            <p className="mt-1 text-[11.5px] text-rose-700 leading-relaxed break-words">{result.blocked_reason}</p>
                        </div>
                    ) : (
                        <div className="rounded-md border border-emerald-200 bg-emerald-50 px-2.5 py-2 text-[11.5px] text-emerald-700">
                            Passes every check a save runs.
                        </div>
                    )}

                    <dl className="grid grid-cols-2 gap-2">
                        <Metric label="Subject" value={`${result.subject_length}/${MAX_SUBJECT}`} />
                        <Metric label="Words" value={result.word_count.toLocaleString()} />
                        <Metric label="Reading time" value={readingTime(result.reading_time_seconds)} />
                        <Metric label="Links" value={result.link_count.toLocaleString()} />
                        <Metric label="Questions" value={result.question_count.toLocaleString()} />
                    </dl>

                    <div>
                        <div className="text-[10px] uppercase tracking-[0.14em] text-slate-400 font-medium mb-1.5">Spam trigger words</div>
                        {hits.length === 0 ? (
                            <p className="text-[11.5px] text-slate-500">None found.</p>
                        ) : (
                            <div className="flex flex-wrap gap-1">
                                {hits.map((h) => (
                                    <span key={h} className="inline-flex items-center h-5 px-1.5 rounded-md border border-amber-200 bg-amber-50 text-[11px] text-amber-700">
                                        {h}
                                    </span>
                                ))}
                            </div>
                        )}
                    </div>
                </>
            )}
        </div>
    );
}

function Metric({ label, value }: { label: string; value: string }) {
    return (
        <div className="rounded-md border border-slate-200 bg-white px-2 py-1.5 min-w-0">
            <dt className="text-[10px] text-slate-400 truncate">{label}</dt>
            <dd className="text-[12.5px] font-mono tabular-nums text-slate-900">{value}</dd>
        </div>
    );
}

function readingTime(seconds: number): string {
    if (seconds < 60) return `${Math.max(0, Math.round(seconds))}s`;
    const m = Math.floor(seconds / 60);
    const s = Math.round(seconds % 60);
    return s ? `${m}m ${s}s` : `${m}m`;
}
