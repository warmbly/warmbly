import React from "react";
import { Link } from "@tanstack/react-router";
import { AnimatePresence, motion } from "framer-motion";
import {
    AlertTriangleIcon,
    ArrowLeftIcon,
    CheckIcon,
    ChevronDownIcon,
    ChevronUpIcon,
    CornerUpLeftIcon,
    FlaskConicalIcon,
    MailCheckIcon,
    PencilLineIcon,
    RefreshCwIcon,
    RotateCcwIcon,
    SparklesIcon,
    UndoIcon,
} from "lucide-react";
import toast from "react-hot-toast/headless";
import { useQueryClient } from "@tanstack/react-query";
import ScrollStrip from "@/components/ui/scroll-strip";
import EmailBody from "@/components/app/unibox/EmailBody";
import EmailContentEditor from "@/components/app/campaigns/sequences/EmailContentEditor";
import { useConfirm } from "@/hooks/context/confirm";
import {
    aiWriteRunning,
    useLeadsBeingWritten,
    useLeadEmails,
    usePersonalizeLeadEmail,
    useResetLeadEmail,
    writeLeadAIOnce,
} from "@/lib/api/hooks/app/campaigns/useCampaignReview";
import type { LeadEmailStep } from "@/lib/api/models/app/campaigns/Review";
import { MISSING_FIELD_LABEL, leadName } from "@/lib/api/models/app/campaigns/Review";
import type { AppError } from "@/lib/api/client/normalizeError";
import buildError from "@/lib/helper/buildError";
import { cn } from "@/lib/utils";
import { STATUS_PILL } from "./reviewStatus";
import { EmailPaneSkeleton, RunningGrid, RunningLabel } from "./reviewLook";
import { aiPreviewCss } from "./aiPreviewCss";
import { useReportDirty } from "@/components/app/flow/dirty";

// One left edge for the header, tabs, notices and email; the email keeps a readable width.
const EDGE = "w-full px-5";
const COLUMN = "w-full max-w-[820px] px-5";

type AIState = "writing" | "error" | "balance" | "cap" | "unavailable";

const fmtDate = (v?: Date | string | null) =>
    v ? new Date(v).toLocaleString(undefined, { month: "short", day: "numeric", hour: "numeric", minute: "2-digit" }) : "";

function Notice({ tone, icon: Icon, children }: { tone: "amber" | "sky" | "violet" | "slate" | "rose"; icon: React.ElementType; children: React.ReactNode }) {
    const tones = {
        amber: "bg-amber-50 text-amber-800 border-amber-200",
        sky: "bg-sky-50 text-sky-800 border-sky-200",
        violet: "bg-violet-50 text-violet-800 border-violet-200",
        slate: "bg-slate-50 text-slate-700 border-slate-200",
        rose: "bg-rose-50 text-rose-800 border-rose-200",
    };
    return (
        <motion.div
            layout
            initial={{ opacity: 0, y: -4 }}
            animate={{ opacity: 1, y: 0 }}
            exit={{ opacity: 0, y: -4 }}
            transition={{ duration: 0.18 }}
            className={cn("px-2.5 py-1.5 rounded-md border text-[12px] leading-relaxed flex items-start gap-2", tones[tone])}
        >
            <Icon className="w-3.5 h-3.5 shrink-0 mt-[3px]" />
            <div className="min-w-0 flex-1">{children}</div>
        </motion.div>
    );
}

export default function LeadEmailsPane({
    campaignId,
    contactId,
    reviewMode,
    canSend,
    canEdit,
    canAI,
    onApprove,
    onReopen,
    lockedStep,
    position,
    total,
    onPrev,
    onNext,
    onBack,
    onDirtyChange,
}: {
    campaignId: string;
    contactId: string;
    reviewMode: boolean;
    canSend: boolean;
    canEdit: boolean;
    canAI: boolean;
    onApprove: () => Promise<void>;
    onReopen: () => Promise<void>;
    // Shows only this step, for the Review tab of a step's panel.
    lockedStep?: string;
    // Where this lead sits in the list, for the "3 of 40" next to the arrows.
    position?: number;
    total?: number;
    onPrev?: () => void;
    onNext?: () => void;
    onBack: () => void;
    onDirtyChange: (dirty: boolean) => void;
}) {
    const qc = useQueryClient();
    const confirm = useConfirm();
    const emails = useLeadEmails(campaignId, contactId);
    const personalize = usePersonalizeLeadEmail(campaignId, contactId);
    const reset = useResetLeadEmail(campaignId, contactId);
    const [stepId, setStepId] = React.useState<string | null>(lockedStep ?? null);
    // A draft belongs to the step it was started on, whatever the data refreshes to.
    const [draft, setDraft] = React.useState<{ stepId: string; subject: string; html: string; plain: string } | null>(null);
    const draftRef = React.useRef(draft);
    draftRef.current = draft;
    const [dirty, setDirty] = React.useState(false);
    const [busy, setBusy] = React.useState(false);
    const [ai, setAI] = React.useState<Record<string, AIState>>({});
    const [fresh, setFresh] = React.useState<Record<string, string[]>>({});
    const attempted = React.useRef(new Set<string>());
    // Re-render when a background write for this lead starts or ends.
    useLeadsBeingWritten(campaignId);

    const data = emails.data;
    const steps = React.useMemo(() => data?.steps ?? [], [data]);
    // A locked panel or an open draft never falls back to another step.
    const pinned = draft?.stepId ?? lockedStep;
    const step: LeadEmailStep | undefined = pinned
        ? steps.find((s) => s.step_id === pinned)
        : (steps.find((s) => s.step_id === stepId) ?? steps.find((s) => !s.sent) ?? steps[0]);
    const canWrite = canAI && canEdit;
    const pending = step && !step.sent ? (step.ai_blocks ?? []).filter((b) => !b.written) : [];
    const aiState = step ? ai[step.step_id] : undefined;
    const writing = aiState === "writing" || (!!step && pending.length > 0 && aiWriteRunning(campaignId, contactId, step.step_id));

    React.useEffect(() => onDirtyChange(dirty), [dirty, onDirtyChange]);
    // Inside a step panel, closing it or opening another step asks before dropping this edit.
    useReportDirty(dirty);

    const write = React.useCallback(
        async (target: LeadEmailStep, rewrite: boolean) => {
            const ids = (target.ai_blocks ?? []).map((b) => b.id);
            setAI((s) => ({ ...s, [target.step_id]: "writing" }));
            try {
                await writeLeadAIOnce(qc, campaignId, contactId, target.step_id, rewrite);
                setFresh((f) => ({ ...f, [target.step_id]: ids }));
                setAI((s) => {
                    const next = { ...s };
                    delete next[target.step_id];
                    return next;
                });
            } catch (err) {
                const code = (err as AppError).code;
                const state: AIState =
                    code === "insufficient_credits" ? "balance" : code === "usage_cap_exceeded" ? "cap" : code === "ai_unavailable" ? "unavailable" : "error";
                setAI((s) => ({ ...s, [target.step_id]: state }));
            }
        },
        [qc, campaignId, contactId],
    );

    // AI copy is written the moment an email is opened, so a reviewer reads the real thing.
    const pendingKey = pending.map((b) => b.id).join(",");
    React.useEffect(() => {
        if (!step || !canWrite || !pendingKey) return;
        if (attempted.current.has(step.step_id)) return;
        attempted.current.add(step.step_id);
        void write(step, false);
    }, [step, canWrite, pendingKey, write]);

    const stopEditing = () => {
        setDraft(null);
        setDirty(false);
    };
    const requestStopEditing = () => {
        if (dirty) confirm.show("Discard your changes to this lead's email?", async () => stopEditing());
        else stopEditing();
    };
    const pickStep = (id: string) => {
        if (id === step?.step_id) return;
        const go = () => {
            stopEditing();
            setStepId(id);
        };
        if (dirty) confirm.show("Discard your changes to this lead's email?", async () => go());
        else go();
    };
    const startEditing = () => {
        if (!step) return;
        setStepId(step.step_id);
        setDraft({ stepId: step.step_id, subject: step.source.subject, html: step.source.body_html, plain: step.source.body_plain });
        setDirty(false);
    };
    const save = async () => {
        if (!draft) return;
        const sent = draft;
        try {
            await personalize.mutateAsync({ stepId: sent.stepId, subject: sent.subject, body_html: sent.html, body_plain: sent.plain });
            toast.success("Saved for this lead");
            // Typing during the save keeps the editor open with the newer text.
            if (draftRef.current === sent) stopEditing();
            attempted.current.delete(sent.stepId);
        } catch (err) {
            toast.error(buildError(err as AppError));
        }
    };
    const resetCopy = () => {
        if (!step) return;
        confirm.show("Go back to the step's copy for this lead? Your edits to this email are removed.", async () => {
            try {
                await reset.mutateAsync(step.step_id);
                attempted.current.delete(step.step_id);
                toast.success("Back to the step's copy");
            } catch (err) {
                toast.error(buildError(err as AppError));
            }
        });
    };
    const act = async (fn: () => Promise<void>) => {
        setBusy(true);
        try {
            await fn();
        } finally {
            setBusy(false);
        }
    };

    if (emails.isLoading) return <EmailPaneSkeleton />;
    if (emails.isError || !data) {
        return (
            <div className="h-full flex flex-col items-center justify-center gap-2 text-[12.5px] text-slate-500">
                Could not load this lead's emails.
                <button type="button" onClick={() => void emails.refetch()} className="text-sky-700 hover:text-sky-800 font-medium">
                    Try again
                </button>
                <button type="button" onClick={onBack} className="md:hidden h-7 px-1.5 rounded text-[12px] text-slate-600 hover:bg-slate-100 inline-flex items-center gap-1">
                    <ArrowLeftIcon className="w-3.5 h-3.5" />
                    Leads
                </button>
            </div>
        );
    }

    const lead = data.lead;
    const pill = STATUS_PILL[lead.status];
    const editable = !!step && !step.sent && canEdit;
    const firstName = lead.first_name?.trim() || leadName(lead);
    const blockName = (pending[0]?.name || step?.ai_blocks?.[0]?.name || "").toLowerCase();
    const written = !!step && (step.ai_blocks?.length ?? 0) > 0 && pending.length === 0;

    return (
        <div className="h-full flex flex-col min-h-0">
            <div className="shrink-0 border-b border-slate-200">
                <div className={cn(EDGE, "h-12 flex items-center gap-2.5")}>
                    <button
                        type="button"
                        onClick={onBack}
                        className="md:hidden h-7 px-1.5 -ml-1.5 rounded text-[12px] text-slate-600 hover:bg-slate-100 inline-flex items-center gap-1"
                    >
                        <ArrowLeftIcon className="w-3.5 h-3.5" />
                        Leads
                    </button>
                    <div className="min-w-0 flex-1 flex items-center gap-2">
                        <span className="shrink-0 max-w-[50%] text-[13.5px] font-semibold text-slate-900 truncate">{leadName(lead)}</span>
                        <span className={cn("shrink-0 h-5 px-1.5 rounded border text-[10.5px] font-medium inline-flex items-center", pill.className)}>{pill.label}</span>
                        <span className="hidden sm:block min-w-0 text-[12px] text-slate-500 truncate">
                            {lead.email}
                            {lead.company ? ` · ${lead.company}` : ""}
                        </span>
                    </div>
                    <div className="hidden md:flex items-center gap-0.5">
                        {position !== undefined && total !== undefined && (
                            <span className="mr-1.5 text-[11.5px] tabular-nums text-slate-400">
                                {position.toLocaleString()} of {total.toLocaleString()}
                            </span>
                        )}
                        <button type="button" onClick={onPrev} disabled={!onPrev} title="Previous lead (K)" aria-label="Previous lead" className="size-7 rounded-md border border-slate-200 text-slate-500 hover:text-slate-800 hover:border-slate-300 disabled:opacity-30 disabled:pointer-events-none flex items-center justify-center">
                            <ChevronUpIcon className="w-3.5 h-3.5" />
                        </button>
                        <button type="button" onClick={onNext} disabled={!onNext} title="Next lead (J)" aria-label="Next lead" className="size-7 rounded-md border border-slate-200 text-slate-500 hover:text-slate-800 hover:border-slate-300 disabled:opacity-30 disabled:pointer-events-none flex items-center justify-center">
                            <ChevronDownIcon className="w-3.5 h-3.5" />
                        </button>
                    </div>
                    {lead.status === "waiting" && canSend && (
                        <button
                            type="button"
                            disabled={busy || dirty}
                            title={dirty ? "Save or discard your edit first" : "Approve this lead (A)"}
                            onClick={() => void act(onApprove)}
                            className="h-7 px-2.5 rounded-md bg-sky-600 hover:bg-sky-700 text-white text-[12px] font-medium inline-flex items-center gap-1.5 shadow-sm disabled:opacity-60 transition-colors"
                        >
                            {busy ? <RunningGrid /> : <CheckIcon className="w-3.5 h-3.5" />}
                            Approve
                        </button>
                    )}
                    {lead.status === "ready" && reviewMode && canEdit && (
                        <button
                            type="button"
                            disabled={busy}
                            onClick={() => void act(onReopen)}
                            title="Hold this lead for review again"
                            className="h-7 px-2.5 rounded-md border border-slate-200 text-slate-700 hover:bg-slate-50 text-[12px] inline-flex items-center gap-1.5 disabled:opacity-60"
                        >
                            {busy ? <RunningGrid className="text-sky-600" /> : <UndoIcon className="w-3.5 h-3.5" />}
                            <span className="hidden sm:inline">Back to review</span>
                        </button>
                    )}
                </div>
            </div>

            {steps.length === 0 ? (
                <p className="p-6 text-center text-[12.5px] text-slate-500">This campaign has no email steps yet.</p>
            ) : !step && lockedStep ? (
                <p className="p-6 text-center text-[12.5px] text-slate-500">This email step is no longer in the campaign.</p>
            ) : (
                <>
                    <div className={cn("shrink-0 border-b border-slate-200", lockedStep && "hidden")}>
                        <ScrollStrip activeKey={step?.step_id} innerClassName={cn(EDGE, "px-3 flex items-center gap-1")}>
                            {steps.map((s, i) => {
                                const active = s.step_id === step?.step_id;
                                const aiPending = !s.sent && (s.ai_blocks ?? []).some((b) => !b.written);
                                return (
                                    <button
                                        key={s.step_id}
                                        type="button"
                                        data-active={active}
                                        onClick={() => pickStep(s.step_id)}
                                        className={cn(
                                            "relative h-9 px-2 inline-flex items-center gap-1.5 text-[12.5px] whitespace-nowrap transition-colors",
                                            active ? "text-slate-900 font-medium" : "text-slate-500 hover:text-slate-800",
                                        )}
                                    >
                                        {s.sent ? <MailCheckIcon className="w-3.5 h-3.5 text-emerald-600" /> : s.threaded ? <CornerUpLeftIcon className="w-3.5 h-3.5" /> : null}
                                        {s.name?.trim() || `Email ${i + 1}`}
                                        {s.personalized && <PencilLineIcon className="w-3 h-3 text-sky-600" />}
                                        {aiPending &&
                                            (ai[s.step_id] === "writing" || aiWriteRunning(campaignId, contactId, s.step_id) ? (
                                                <RunningGrid className="text-sky-600" />
                                            ) : (
                                                <SparklesIcon className="w-3 h-3 text-violet-500" />
                                            ))}
                                        {active && (
                                            <motion.span
                                                layoutId={`review-step-${contactId}`}
                                                className="absolute left-1 right-1 -bottom-px h-0.5 rounded-full bg-sky-600"
                                                transition={{ type: "spring", duration: 0.3, bounce: 0.15 }}
                                            />
                                        )}
                                    </button>
                                );
                            })}
                        </ScrollStrip>
                    </div>

                    <div className="flex-1 min-h-0 overflow-y-auto">
                        {step && draft?.stepId === step.step_id ? (
                            <div className={cn(COLUMN, "py-4 flex flex-col gap-3")}>
                                <p className="text-[12px] text-slate-500">
                                    Editing this email for {leadName(lead)} only. Merge fields and AI blocks still fill in when it sends.
                                </p>
                                <EmailContentEditor
                                    key={`${contactId}:${step.step_id}`}
                                    subject={draft.subject}
                                    onSubjectChange={(v) => {
                                        setDraft((d) => d && { ...d, subject: v });
                                        setDirty(true);
                                    }}
                                    bodyHtml={draft.html}
                                    onBodyChange={(html, plain) => {
                                        setDraft((d) => d && { ...d, html, plain });
                                        setDirty(true);
                                    }}
                                    subjectLocked={step.threaded ? { subject: step.subject, note: "Replies in the lead's thread, so it carries the conversation's subject." } : undefined}
                                    campaignId={campaignId}
                                    stepId={step.step_id}
                                />
                                <div className="flex items-center justify-end gap-2">
                                    <button type="button" onClick={requestStopEditing} className="h-8 px-3 rounded-md border border-slate-200 text-[12.5px] text-slate-700 hover:bg-slate-50">
                                        Cancel
                                    </button>
                                    <button
                                        type="button"
                                        onClick={() => void save()}
                                        disabled={personalize.isPending || !dirty}
                                        className="h-8 px-3 rounded-md bg-sky-600 hover:bg-sky-700 text-white text-[12.5px] font-medium inline-flex items-center gap-1.5 disabled:opacity-60"
                                    >
                                        {personalize.isPending && <RunningGrid />}
                                        Save for this lead
                                    </button>
                                </div>
                            </div>
                        ) : step ? (
                            <motion.div
                                key={step.step_id}
                                initial={{ opacity: 0, y: 6 }}
                                animate={{ opacity: 1, y: 0 }}
                                transition={{ duration: 0.2, ease: "easeOut" }}
                                className={cn(COLUMN, "py-3 flex flex-col gap-2")}
                            >
                                <AnimatePresence initial={false}>
                                    {step.sent && (
                                        <Notice key="sent" tone="slate" icon={MailCheckIcon}>
                                            Sent {fmtDate(step.sent_at)}. This is the current copy, which can no longer change for this lead.
                                        </Notice>
                                    )}
                                    {(step.errors ?? []).map((e) => (
                                        <Notice key={e} tone="rose" icon={AlertTriangleIcon}>
                                            {e}
                                        </Notice>
                                    ))}
                                    {(step.missing?.length ?? 0) > 0 && (
                                        <Notice key="missing" tone="amber" icon={AlertTriangleIcon}>
                                            This lead has no {step.missing!.map((m) => MISSING_FIELD_LABEL[m] ?? m).join(" or ")}, and this email prints it with nothing in its place.
                                            {editable && " Fill it in on the contact, or edit this email for them."}
                                        </Notice>
                                    )}
                                    {step.personalized && (
                                        <Notice key="personal" tone="sky" icon={PencilLineIcon}>
                                            Personalized for this lead{step.edited_at ? ` on ${fmtDate(step.edited_at)}` : ""}.
                                            {step.step_changed && " The step's copy changed since, and this lead still gets this version."}
                                        </Notice>
                                    )}
                                    {step.variant_id && !step.personalized && (
                                        <Notice key="variant" tone="slate" icon={FlaskConicalIcon}>
                                            This lead is in an A/B variant of this step.
                                        </Notice>
                                    )}
                                    {pending.length > 0 && aiState === "balance" && (
                                        <Notice key="ai-balance" tone="amber" icon={SparklesIcon}>
                                            The balance is too low to write the AI part now, so it is written when the email sends.{" "}
                                            <Link to="/app/settings/billing/{-$tab}" params={{ tab: "balance" }} className="font-medium underline underline-offset-2">
                                                Add money
                                            </Link>
                                        </Notice>
                                    )}
                                    {pending.length > 0 && aiState === "cap" && (
                                        <Notice key="ai-cap" tone="amber" icon={SparklesIcon}>
                                            The workspace reached its AI spend limit, so the AI part is written when the email sends.
                                        </Notice>
                                    )}
                                    {pending.length > 0 && aiState === "unavailable" && (
                                        <Notice key="ai-off" tone="slate" icon={SparklesIcon}>
                                            This instance has no AI provider set up, so the marked part cannot be written. An operator can connect one in the instance settings.
                                        </Notice>
                                    )}
                                    {pending.length > 0 && aiState === "error" && (
                                        <Notice key="ai-error" tone="rose" icon={SparklesIcon}>
                                            The AI part could not be written just now.{" "}
                                            <button type="button" onClick={() => void write(step, false)} className="font-medium underline underline-offset-2">
                                                Try again
                                            </button>
                                        </Notice>
                                    )}
                                    {pending.length === 0 && aiState && aiState !== "writing" && (
                                        <Notice key="ai-rewrite" tone="rose" icon={SparklesIcon}>
                                            {aiState === "balance"
                                                ? "The balance is too low to write it again."
                                                : aiState === "cap"
                                                  ? "The workspace reached its AI spend limit."
                                                  : "It could not be written again just now."}{" "}
                                            The copy below is what goes out.
                                        </Notice>
                                    )}
                                    {pending.length > 0 && !canWrite && (
                                        <Notice key="ai-later" tone="slate" icon={SparklesIcon}>
                                            AI writes the marked part for {firstName} when the email sends.
                                        </Notice>
                                    )}
                                </AnimatePresence>

                                <div className="rounded-lg border border-slate-200 bg-white shadow-[0_1px_2px_rgba(15,23,42,0.04)] overflow-hidden">
                                    <dl className="px-4 py-3 border-b border-slate-100 grid grid-cols-[60px_1fr] gap-y-1 text-[12.5px]">
                                        <dt className="text-slate-400">From</dt>
                                        <dd className="text-slate-700 truncate">
                                            {data.from ? `${data.from.name} <${data.from.email}>` : <span className="text-slate-500">The mailbox the campaign picks when it sends</span>}
                                        </dd>
                                        <dt className="text-slate-400">To</dt>
                                        <dd className="text-slate-700 truncate">{lead.email}</dd>
                                        <dt className="text-slate-400">Subject</dt>
                                        <dd className="text-slate-900 font-medium truncate">
                                            {step.subject || <span className="text-slate-400 font-normal">No subject</span>}
                                            {step.threaded && <span className="ml-1.5 text-[11px] font-normal text-slate-400">replies in the thread</span>}
                                        </dd>
                                    </dl>
                                    <div className="px-4 py-3.5">
                                        <EmailBody
                                            html={step.body_html}
                                            plain={step.body_plain}
                                            extraCss={aiPreviewCss({ writing: writing || (canWrite && pending.length > 0 && !aiState), justWritten: fresh[step.step_id] ?? [] })}
                                        />
                                    </div>
                                </div>

                                <div className="flex items-center gap-2 flex-wrap min-h-8">
                                    <AnimatePresence mode="wait" initial={false}>
                                    {writing ? (
                                        <motion.div
                                            key="writing"
                                            initial={{ opacity: 0, y: 4 }}
                                            animate={{ opacity: 1, y: 0 }}
                                            exit={{ opacity: 0, y: -4 }}
                                            transition={{ duration: 0.16 }}
                                        >
                                            <RunningLabel
                                                labels={[
                                                    `Reading about ${lead.company || firstName}`,
                                                    `Writing ${firstName}'s ${blockName || "AI part"}`,
                                                    "Matching your voice",
                                                ]}
                                            />
                                        </motion.div>
                                    ) : written && !step.sent ? (
                                        <motion.span
                                            key="written"
                                            initial={{ opacity: 0, y: 4 }}
                                            animate={{ opacity: 1, y: 0 }}
                                            exit={{ opacity: 0, y: -4 }}
                                            transition={{ duration: 0.16 }}
                                            className="text-[12px] text-slate-500 inline-flex items-center gap-1.5"
                                        >
                                            <SparklesIcon className="w-3.5 h-3.5 text-violet-500" />
                                            AI wrote the highlighted part for {firstName}.
                                            {canWrite && (
                                                <button
                                                    type="button"
                                                    onClick={() => void write(step, true)}
                                                    disabled={writing}
                                                    title="Write it again; charged like the first time"
                                                    className="ml-0.5 inline-flex items-center gap-1 font-medium text-sky-700 hover:text-sky-800 disabled:opacity-50"
                                                >
                                                    <RefreshCwIcon className="w-3 h-3" />
                                                    Rewrite
                                                </button>
                                            )}
                                        </motion.span>
                                    ) : null}
                                    </AnimatePresence>
                                    {editable && (
                                        <div className="ml-auto flex items-center gap-1.5">
                                            {step.personalized && (
                                                <button
                                                    type="button"
                                                    onClick={resetCopy}
                                                    className="h-8 px-2.5 rounded-md text-[12.5px] text-slate-600 hover:bg-slate-100 inline-flex items-center gap-1.5"
                                                >
                                                    <RotateCcwIcon className="w-3.5 h-3.5" />
                                                    Use the step's copy
                                                </button>
                                            )}
                                            <button
                                                type="button"
                                                onClick={startEditing}
                                                className="h-8 px-3 rounded-md border border-slate-200 bg-white text-[12.5px] text-slate-700 hover:bg-slate-50 inline-flex items-center gap-1.5"
                                            >
                                                <PencilLineIcon className="w-3.5 h-3.5" />
                                                Edit for this lead
                                            </button>
                                        </div>
                                    )}
                                </div>
                            </motion.div>
                        ) : null}
                    </div>
                </>
            )}
        </div>
    );
}
