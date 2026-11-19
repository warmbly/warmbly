// One signature for many mailboxes: written once with placeholders, filled in
// per mailbox and saved to each with its own PATCH /emails/:id.
import React from "react";
import { AnimatePresence, motion } from "framer-motion";
import toast from "react-hot-toast/headless";
import { useQueryClient } from "@tanstack/react-query";
import {
    AlertTriangleIcon,
    CheckCircle2Icon,
    ChevronLeftIcon,
    ChevronRightIcon,
    Loader2Icon,
    PenLineIcon,
    XCircleIcon,
    XIcon,
} from "lucide-react";
import EmailEditor from "@/components/app/EmailEditor";
import EmailBody from "@/components/app/unibox/EmailBody";
import { Toggle } from "@/components/app/campaigns/preferences/components/CampaignPreferenceBoolBox";
import { SelectMenu } from "@/components/ui/select-menu";
import { useConfirm } from "@/hooks/context/confirm";
import updateEmail from "@/lib/api/client/app/emails/updateEmail";
import type Inbox from "@/lib/api/models/app/emails/Inbox";
import { errorMessage } from "@/lib/errors/message";
import { signatureToPlain } from "@/lib/email/signaturePlain";
import {
    SIGNATURE_PLACEHOLDERS,
    fillSignature,
    senderFields,
    unknownPlaceholders,
} from "@/lib/email/signaturePlaceholders";
import { cn } from "@/lib/utils";
import UnknownPlaceholders from "./UnknownPlaceholders";

// Enough to finish a large selection quickly without bursting the API rate limit.
const CONCURRENCY = 4;
const NAME_TOKEN = /\{\{\s*\.Sender\.(FirstName|LastName|Name)\s*\}\}/;

type Phase = "form" | "running" | "done";
interface Failure {
    id: string;
    email: string;
    reason: string;
}

const plural = (n: number, one: string, many: string) => `${n.toLocaleString()} ${n === 1 ? one : many}`;
const hasSignature = (m: Inbox) => !!(m.signature_html?.trim() || m.signature_plain?.trim());

export default function BulkSignatureDialog({
    open,
    mailboxes,
    onClose,
}: {
    open: boolean;
    /** The selected mailboxes. */
    mailboxes: Inbox[];
    onClose: () => void;
}) {
    return <AnimatePresence>{open && <Dialog key="bulk-signature" mailboxes={mailboxes} onClose={onClose} />}</AnimatePresence>;
}

function Dialog({ mailboxes, onClose }: { mailboxes: Inbox[]; onClose: () => void }) {
    const confirm = useConfirm();
    const queryClient = useQueryClient();
    const editorWrap = React.useRef<HTMLDivElement>(null);

    const [html, setHtml] = React.useState("");
    const [plain, setPlain] = React.useState("");
    const [plainSync, setPlainSync] = React.useState(true);
    const [code, setCode] = React.useState(false);
    const [addToSends, setAddToSends] = React.useState(true);
    const [previewPick, setPreviewPick] = React.useState<string | null>(null);

    const [phase, setPhase] = React.useState<Phase>("form");
    const [total, setTotal] = React.useState(0);
    const [done, setDone] = React.useState(0);
    const [failures, setFailures] = React.useState<Failure[]>([]);

    const n = mailboxes.length;
    const previewIndex = Math.max(0, mailboxes.findIndex((m) => m.id === previewPick));
    const preview = mailboxes[previewIndex];
    const replacing = mailboxes.filter(hasSignature).length;
    const unnamed = mailboxes.filter((m) => !senderFields(m).named).length;
    const usesName = NAME_TOKEN.test(html) || (!plainSync && NAME_TOKEN.test(plain));
    const unknown = unknownPlaceholders(plainSync ? html : `${html}\n${plain}`);
    const empty = !html.trim() && !(plainSync ? signatureToPlain(html) : plain).trim();
    const issue = n === 0 ? "No mailboxes are selected." : empty ? "Write a signature first." : null;

    const running = phase === "running";
    const dirty = phase === "form" && (html.trim() !== "" || plain.trim() !== "" || !addToSends);

    const requestClose = React.useCallback(() => {
        if (running) return;
        if (dirty) confirm.show("Discard this signature?", async () => onClose());
        else onClose();
    }, [running, dirty, confirm, onClose]);

    React.useEffect(() => {
        function onKey(e: KeyboardEvent) {
            if (e.key !== "Escape") return;
            if (document.querySelector("[data-floating], [role='alertdialog']")) return;
            requestClose();
        }
        window.addEventListener("keydown", onKey);
        return () => window.removeEventListener("keydown", onKey);
    }, [requestClose]);

    const resolved = (m: Inbox) => fillSignature({ html, plain, plainSync }, m);

    // At the caret when the editor has one, otherwise at the end of the signature.
    function insert(token: string) {
        const el = document.activeElement;
        const editable =
            el instanceof HTMLElement &&
            !!editorWrap.current?.contains(el) &&
            (el.isContentEditable || (el instanceof HTMLTextAreaElement && !el.readOnly));
        if (editable && document.execCommand("insertText", false, token)) return;
        const next = html.trim() ? `${html} ${token}` : token;
        setHtml(next);
        setPlain(plainSync ? signatureToPlain(next) : plain.trim() ? `${plain} ${token}` : token);
    }

    async function run(targets: Inbox[]) {
        setPhase("running");
        setTotal(targets.length);
        setDone(0);
        setFailures([]);
        const failed: Failure[] = [];
        let next = 0;
        const worker = async () => {
            while (next < targets.length) {
                const m = targets[next++];
                const sig = resolved(m);
                try {
                    await updateEmail(m.id, {
                        signature_html: sig.html,
                        signature_plain: sig.plain,
                        signature_sync: addToSends,
                        signature_code: code,
                    });
                } catch (e) {
                    failed.push({ id: m.id, email: m.email, reason: errorMessage(e, "The signature could not be saved.") });
                }
                setDone((d) => d + 1);
            }
        };
        await Promise.all(Array.from({ length: Math.min(CONCURRENCY, targets.length) }, worker));
        await queryClient.invalidateQueries({ queryKey: ["emails"] });
        setFailures(failed);
        setPhase("done");
        const saved = targets.length - failed.length;
        if (failed.length === 0) toast.success(`Signature saved to ${plural(saved, "mailbox", "mailboxes")}`);
        else toast.error(`${plural(failed.length, "mailbox", "mailboxes")} could not be updated`);
    }

    function apply() {
        if (issue || running) return;
        const targets = mailboxes;
        if (replacing > 0) {
            confirm.show(
                `Replace signatures on ${plural(replacing, "mailbox", "mailboxes")}? ${
                    replacing === n ? "Every selected mailbox" : `${replacing.toLocaleString()} of the ${n.toLocaleString()} selected`
                } already ${replacing === 1 ? "has" : "have"} a signature, and it is overwritten.`,
                // Not awaited, so the confirm closes and the dialog shows the progress.
                async () => void run(targets),
            );
            return;
        }
        void run(targets);
    }

    function retryFailed() {
        const ids = new Set(failures.map((f) => f.id));
        void run(mailboxes.filter((m) => ids.has(m.id)));
    }

    const step = (by: number) => {
        if (n === 0) return;
        setPreviewPick(mailboxes[(previewIndex + by + n) % n].id);
    };
    const fields = preview ? senderFields(preview) : null;
    const previewSig = preview ? resolved(preview) : null;
    const saved = total - failures.length;

    return (
        <motion.div
            initial={{ opacity: 0 }}
            animate={{ opacity: 1 }}
            exit={{ opacity: 0 }}
            transition={{ duration: 0.15 }}
            onMouseDown={requestClose}
            className="fixed inset-0 z-[110] flex items-center justify-center bg-slate-900/30 backdrop-blur-[2px] px-4"
        >
            <motion.div
                initial={{ y: 8, opacity: 0 }}
                animate={{ y: 0, opacity: 1 }}
                exit={{ y: 8, opacity: 0 }}
                transition={{ duration: 0.16 }}
                onMouseDown={(e) => e.stopPropagation()}
                role="dialog"
                aria-modal="true"
                aria-label="Set signature"
                className="w-full max-w-[920px] rounded-lg bg-white border border-slate-200 shadow-[0_24px_48px_-12px_rgba(15,23,42,0.18),0_8px_16px_-8px_rgba(15,23,42,0.1)] overflow-hidden flex flex-col max-h-[calc(100dvh-2rem)]"
            >
                <div className="h-12 px-4 border-b border-slate-200 flex items-center gap-2.5 shrink-0">
                    <div className="size-5 rounded bg-sky-50 text-sky-600 flex items-center justify-center">
                        <PenLineIcon className="w-3 h-3" />
                    </div>
                    <span className="text-[10px] uppercase tracking-[0.14em] text-slate-400 font-medium">Signature</span>
                    <div className="h-4 w-px bg-slate-200" />
                    <span className="text-[12.5px] text-slate-900 font-medium truncate">{plural(n, "mailbox", "mailboxes")}</span>
                    <button
                        type="button"
                        onClick={requestClose}
                        disabled={running}
                        aria-label="Close"
                        className="ml-auto size-7 rounded-md text-slate-500 hover:text-slate-900 hover:bg-slate-100 inline-flex items-center justify-center transition-colors disabled:opacity-40"
                    >
                        <XIcon className="w-3.5 h-3.5" />
                    </button>
                </div>

                <div className="px-4 py-4 flex-1 min-h-0 overflow-y-auto">
                    {phase === "form" ? (
                        <div className="grid grid-cols-1 lg:grid-cols-[minmax(0,1fr)_minmax(0,0.85fr)] gap-5">
                            <div className="space-y-4 min-w-0">
                                <div className="space-y-2">
                                    <SectionLabel>Template</SectionLabel>
                                    <div ref={editorWrap} className="overflow-x-auto">
                                        <EmailEditor
                                            id="bulk-signature"
                                            htmlText={html}
                                            setHtmlText={setHtml}
                                            plainText={plain}
                                            setPlainText={setPlain}
                                            sync={plainSync}
                                            setSync={setPlainSync}
                                            code={code}
                                            setCode={setCode}
                                        />
                                    </div>
                                    <div className="flex flex-wrap items-center gap-1.5">
                                        {SIGNATURE_PLACEHOLDERS.map((p) => (
                                            <button
                                                key={p.token}
                                                type="button"
                                                // Keeps the caret in the editor so the token lands where it was.
                                                onMouseDown={(e) => e.preventDefault()}
                                                onClick={() => insert(p.token)}
                                                title={`Insert ${p.token}`}
                                                className="group h-6 px-2 inline-flex items-center gap-1.5 rounded-md border border-slate-200 bg-white hover:border-sky-300 hover:bg-sky-50 text-[11px] text-slate-700 hover:text-sky-700 transition-colors"
                                            >
                                                <span className="font-medium">{p.label}</span>
                                                <code className="font-mono text-[10px] text-slate-400 group-hover:text-sky-500">{p.token}</code>
                                            </button>
                                        ))}
                                    </div>
                                    <p className="text-[11px] text-slate-400 leading-relaxed">
                                        These are the same sender fields campaign emails use. Each mailbox gets its own values when you apply,
                                        and stores the finished signature. Contact fields and spintax are not filled in inside a signature.
                                    </p>
                                    <UnknownPlaceholders unknown={unknown} />
                                </div>

                                <div className="flex items-start justify-between gap-3 rounded-md border border-slate-200 px-3 py-2.5">
                                    <div className="min-w-0">
                                        <div className="text-[12.5px] font-medium text-slate-900">Add signature to sends</div>
                                        <div className="text-[11px] text-slate-400 leading-relaxed">
                                            Appended to campaign mail and unibox replies from every selected mailbox. Off stores the signature
                                            without adding it.
                                        </div>
                                    </div>
                                    <Toggle value={addToSends} onChange={setAddToSends} ariaLabel="Add signature to sends" />
                                </div>
                            </div>

                            <div className="space-y-2 min-w-0">
                                <div className="flex items-center justify-between gap-2">
                                    <SectionLabel>Preview</SectionLabel>
                                    <span className="text-[11px] text-slate-400 tabular-nums">
                                        {n > 0 ? `${(previewIndex + 1).toLocaleString()} of ${n.toLocaleString()}` : ""}
                                    </span>
                                </div>
                                {preview && fields && previewSig ? (
                                    <>
                                        <div className="flex items-center gap-1">
                                            <button
                                                type="button"
                                                onClick={() => step(-1)}
                                                disabled={n < 2}
                                                aria-label="Previous mailbox"
                                                className="size-7 shrink-0 rounded-md text-slate-500 hover:text-slate-900 hover:bg-slate-100 inline-flex items-center justify-center transition-colors disabled:opacity-40"
                                            >
                                                <ChevronLeftIcon className="w-3.5 h-3.5" />
                                            </button>
                                            <div className="min-w-0 flex-1">
                                                <SelectMenu
                                                    value={preview.id}
                                                    onChange={setPreviewPick}
                                                    options={mailboxes.map((m) => ({
                                                        value: m.id,
                                                        label: senderFields(m).named ? `${m.name.trim()} · ${m.email}` : `${m.email} (no name)`,
                                                    }))}
                                                    fullWidth
                                                    aria-label="Mailbox to preview"
                                                />
                                            </div>
                                            <button
                                                type="button"
                                                onClick={() => step(1)}
                                                disabled={n < 2}
                                                aria-label="Next mailbox"
                                                className="size-7 shrink-0 rounded-md text-slate-500 hover:text-slate-900 hover:bg-slate-100 inline-flex items-center justify-center transition-colors disabled:opacity-40"
                                            >
                                                <ChevronRightIcon className="w-3.5 h-3.5" />
                                            </button>
                                        </div>
                                        <div className="flex flex-wrap items-center gap-1.5 text-[11px]">
                                            {SIGNATURE_PLACEHOLDERS.map((p) => (
                                                <span key={p.field} className="inline-flex items-center gap-1 h-5 px-1.5 rounded bg-slate-50 border border-slate-200 text-slate-500 max-w-full">
                                                    <span className="shrink-0">{p.label}</span>
                                                    <span className={cn("truncate", fields[p.field] ? "text-slate-800" : "italic text-slate-400")}>
                                                        {fields[p.field] || "empty"}
                                                    </span>
                                                </span>
                                            ))}
                                        </div>
                                        {!fields.named && (
                                            <p className="flex items-start gap-1.5 text-[11px] text-amber-700 leading-relaxed">
                                                <AlertTriangleIcon className="w-3 h-3 mt-0.5 shrink-0" />
                                                <span>
                                                    This mailbox has no sender name, so the name fields come out empty. Set a sender name on the
                                                    mailbox first.
                                                </span>
                                            </p>
                                        )}
                                        <div className="rounded-md border border-slate-200 min-h-[140px] px-3 py-2.5">
                                            {previewSig.html.trim() || previewSig.plain.trim() ? (
                                                <EmailBody html={previewSig.html} plain={previewSig.plain} />
                                            ) : (
                                                <p className="text-[12px] italic text-slate-400">Nothing to preview yet.</p>
                                            )}
                                        </div>
                                    </>
                                ) : (
                                    <p className="text-[12px] italic text-slate-400">No mailboxes are selected.</p>
                                )}
                                <ul className="space-y-1 pt-1 text-[11px] text-slate-500 leading-relaxed">
                                    <li>Saves to {plural(n, "mailbox", "mailboxes")}.</li>
                                    {replacing > 0 && (
                                        <li>
                                            {plural(replacing, "mailbox already has a signature", "mailboxes already have a signature")}, replaced
                                            when you apply.
                                        </li>
                                    )}
                                    {unnamed > 0 && usesName && (
                                        <li className="text-amber-700">
                                            {plural(unnamed, "mailbox has", "mailboxes have")} no sender name, so the name fields come out
                                            empty there.
                                        </li>
                                    )}
                                </ul>
                            </div>
                        </div>
                    ) : (
                        <div className="space-y-4">
                            <div>
                                <div className="flex items-center justify-between text-[11.5px] text-slate-600 mb-1.5">
                                    <span className="inline-flex items-center gap-1.5">
                                        {running ? (
                                            <Loader2Icon className="w-3 h-3 animate-spin text-sky-600" />
                                        ) : failures.length === 0 ? (
                                            <CheckCircle2Icon className="w-3 h-3 text-emerald-600" />
                                        ) : (
                                            <AlertTriangleIcon className="w-3 h-3 text-amber-600" />
                                        )}
                                        {running
                                            ? "Saving signatures…"
                                            : `Saved to ${saved.toLocaleString()} of ${plural(total, "mailbox", "mailboxes")}`}
                                    </span>
                                    <span className="tabular-nums text-slate-400">
                                        {Math.min(done, total).toLocaleString()} / {total.toLocaleString()}
                                    </span>
                                </div>
                                <div className="h-1 rounded-full bg-slate-100 overflow-hidden">
                                    <motion.div
                                        className="h-full rounded-full bg-sky-500"
                                        initial={false}
                                        animate={{ width: `${total ? Math.round((Math.min(done, total) / total) * 100) : 0}%` }}
                                        transition={{ duration: 0.3, ease: [0.22, 1, 0.36, 1] }}
                                    />
                                </div>
                            </div>
                            {!running && failures.length > 0 && (
                                <div className="space-y-1.5">
                                    <SectionLabel>Not updated</SectionLabel>
                                    <ul className="rounded-md border border-slate-200 divide-y divide-slate-100 max-h-[320px] overflow-y-auto">
                                        {failures.map((f) => (
                                            <li key={f.id} className="flex items-start gap-2 px-3 py-2">
                                                <XCircleIcon className="w-3.5 h-3.5 mt-0.5 shrink-0 text-rose-500" />
                                                <div className="min-w-0">
                                                    <div className="text-[12.5px] text-slate-900 truncate">{f.email}</div>
                                                    <div className="text-[11px] text-slate-500 break-words">{f.reason}</div>
                                                </div>
                                            </li>
                                        ))}
                                    </ul>
                                </div>
                            )}
                        </div>
                    )}
                </div>

                <div className="px-4 py-2.5 min-h-12 flex flex-wrap items-center justify-end gap-2 border-t border-slate-200 bg-slate-50/60 shrink-0">
                    {phase === "form" ? (
                        <>
                            {issue && <span className="mr-auto text-[11.5px] text-slate-500">{issue}</span>}
                            <button
                                type="button"
                                onClick={requestClose}
                                className="h-7 px-3 rounded-md border border-slate-200 hover:border-slate-300 text-[12px] text-slate-700 hover:text-slate-900 transition-colors"
                            >
                                Cancel
                            </button>
                            <button
                                type="button"
                                onClick={apply}
                                disabled={!!issue}
                                className="h-7 px-3 rounded-md bg-sky-600 hover:bg-sky-700 text-white text-[12px] font-medium transition-colors disabled:opacity-50 disabled:hover:bg-sky-600"
                            >
                                Apply to {plural(n, "mailbox", "mailboxes")}
                            </button>
                        </>
                    ) : (
                        <>
                            {!running && failures.length > 0 && (
                                <button
                                    type="button"
                                    onClick={retryFailed}
                                    className="h-7 px-3 rounded-md border border-slate-200 hover:border-slate-300 text-[12px] text-slate-700 hover:text-slate-900 transition-colors"
                                >
                                    Retry {plural(failures.length, "mailbox", "mailboxes")}
                                </button>
                            )}
                            <button
                                type="button"
                                onClick={onClose}
                                disabled={running}
                                className="h-7 px-3 rounded-md bg-slate-900 hover:bg-slate-800 text-white text-[12px] font-medium transition-colors disabled:opacity-50"
                            >
                                Done
                            </button>
                        </>
                    )}
                </div>
            </motion.div>
        </motion.div>
    );
}

function SectionLabel({ children }: { children: React.ReactNode }) {
    return <div className="text-[10px] uppercase tracking-[0.14em] text-slate-400 font-medium">{children}</div>;
}
