// The new-campaign flow: Leads, Emails, Schedule, Review, with a live launch
// plan beside it that simulates the send against the real mailbox pool
// (warmup graduation, the warmup mail running alongside, other campaigns,
// health bands, spacing).
//
// A campaign that is not launched yet is a draft on the server. Closing the
// flow with something in it saves the draft, a draft clicked in the campaigns
// list reopens here, and a launch saves it and hands over to the campaign
// page's launch dialog, so the pre-send checks and the list-risk gate run
// exactly as they do for every other start.

import React from "react";
import { AnimatePresence, motion } from "framer-motion";
import {
    AlertCircleIcon,
    CalendarClockIcon,
    CheckIcon,
    ChevronLeftIcon,
    ChevronRightIcon,
    ExternalLinkIcon,
    ListChecksIcon,
    Loader2Icon,
    MegaphoneIcon,
    RocketIcon,
    XIcon,
} from "lucide-react";
import toast from "react-hot-toast/headless";
import { useLocation, useNavigate } from "@tanstack/react-router";
import { useQueryClient } from "@tanstack/react-query";
import useCampaignEstimate from "@/lib/api/hooks/app/campaigns/useCampaignEstimate";
import useDeleteCampaign from "@/lib/api/hooks/app/campaigns/useDeleteCampaign";
import { useSegments } from "@/lib/api/hooks/app/segments";
import useCurrentOrganization from "@/lib/api/hooks/app/organizations/useCurrentOrganization";
import { defaultScheduleTimezone } from "@/lib/timezone";
import { useConfirm } from "@/hooks/context/confirm";
import { usePermission } from "@/hooks/usePermission";
import type { AppError } from "@/lib/api/client/normalizeError";
import buildError from "@/lib/helper/buildError";
import { cn } from "@/lib/utils";
import {
    NAME_MAX,
    NAME_MIN,
    STEPS,
    autoName,
    firstIssue,
    initialDraft,
    scheduledDate,
    stepIssue,
    stepWaits,
    writtenEmails,
    type Draft,
    type StepKey,
} from "./new/draft";
import { draftSignature, freshMeta, loadServerDraft, saveServerDraft, type DraftMeta } from "./new/serverDraft";
import { EmailsStep, LeadsStep, LaunchPlanRail, ReviewStep, ScheduleStep, type EstimateState } from "./new/steps";
import type { Patch } from "./new/fields";

interface Props {
    open: boolean;
    onClose: () => void;
    // A draft campaign to reopen; absent starts a new one.
    draftId?: string | null;
}

type Busy = "draft" | "launch" | "close" | "leave" | "delete";

// Where a reopened draft picks up: the first part that still needs work.
function resumeStep(d: Draft, meta: DraftMeta): number {
    if (d.segmentIds.length === 0 && meta.leadCount === 0) return 0;
    if (!meta.stepsLocked && writtenEmails(d).length === 0) return 1;
    return STEPS.length - 1;
}

export function NewCampaignDialog({ open, onClose, draftId = null }: Props) {
    const navigate = useNavigate();
    const pathname = useLocation({ select: (l) => l.pathname });
    const confirm = useConfirm();
    const queryClient = useQueryClient();
    const deleteCampaign = useDeleteCampaign();
    const org = useCurrentOrganization();
    const canSend = usePermission("SEND_CAMPAIGNS");
    // Follow the workspace when it has a timezone, else start from this
    // browser's. The API's list is sorted by offset, so its first entry is
    // never a sensible default.
    const defaultTimezone = org.data?.timezone ? "" : defaultScheduleTimezone("");

    const [step, setStep] = React.useState(0);
    const [direction, setDirection] = React.useState<1 | -1>(1);
    const [draft, setDraft] = React.useState<Draft>(() => initialDraft(defaultTimezone));
    const [emailIndex, setEmailIndex] = React.useState(0);
    // Set when the user tries to leave a step that is not ready; shows the reason.
    const [nudged, setNudged] = React.useState(false);
    const [busyWith, setBusyWith] = React.useState<Busy | null>(null);
    // The saved campaign this flow edits, and what it held when opened.
    const [existing, setExisting] = React.useState<{ id: string; meta: DraftMeta } | null>(null);
    const [baseline, setBaseline] = React.useState("");
    const [loading, setLoading] = React.useState(false);
    // Set once the user picks a zone, so a workspace default that loads late never overrides it.
    const tzTouched = React.useRef(false);

    const current = STEPS[step];
    const lastStep = STEPS.length - 1;

    // Each opening starts fresh, or from the saved draft it was opened on.
    React.useEffect(() => {
        if (!open) return;
        setEmailIndex(0);
        setDirection(1);
        setNudged(false);
        setBusyWith(null);
        if (!draftId) {
            const fresh = initialDraft(defaultTimezone);
            setDraft(fresh);
            setExisting(null);
            setBaseline(draftSignature(fresh));
            setStep(0);
            setLoading(false);
            tzTouched.current = false;
            return;
        }
        let cancelled = false;
        setLoading(true);
        tzTouched.current = true;
        loadServerDraft(draftId)
            .then(({ draft: d, meta }) => {
                if (cancelled) return;
                setDraft(d);
                setExisting({ id: draftId, meta });
                setBaseline(draftSignature(d));
                setStep(resumeStep(d, meta));
                setLoading(false);
            })
            .catch((err) => {
                if (cancelled) return;
                toast.error(buildError(err as AppError));
                onClose();
            });
        return () => {
            cancelled = true;
        };
        // eslint-disable-next-line react-hooks/exhaustive-deps -- keyed on the opening, not on every render's callbacks
    }, [open, draftId]);

    React.useEffect(() => {
        if (open && !draftId && !tzTouched.current) {
            setDraft((d) => (d.timezone === defaultTimezone ? d : { ...d, timezone: defaultTimezone }));
        }
    }, [open, draftId, defaultTimezone]);

    const patch = React.useCallback<Patch>((p) => {
        setDraft((d) => {
            const update = typeof p === "function" ? p(d) : p;
            if (update.timezone !== undefined) tzTouched.current = true;
            return { ...d, ...update };
        });
    }, []);

    // Names for the auto name and the review.
    const segments = useSegments(open);
    const segmentNames = React.useMemo(() => {
        const byId = new Map((segments.data ?? []).map((s) => [s.id, s.name]));
        return draft.segmentIds.map((id) => byId.get(id)).filter((n): n is string => !!n);
    }, [segments.data, draft.segmentIds]);
    const placeholderName = autoName(segmentNames);
    const meta = existing?.meta;
    // Leads can come from the lists picked here or be on a saved draft already.
    const hasLeads = draft.segmentIds.length > 0 || (meta?.leadCount ?? 0) > 0;
    const typedName = draft.name.trim();
    const finalName = typedName || placeholderName;
    // A save never fails on the name: one that is not valid yet falls back.
    const savedName =
        typedName.length >= NAME_MIN && typedName.length <= NAME_MAX ? typedName : typedName.length > NAME_MAX ? typedName.slice(0, NAME_MAX) : placeholderName;

    const lockedEmails = meta?.stepsLocked ? meta.steps.filter((s) => (s.kind ?? "email") === "email") : null;

    // The live projection behind the rail and the review.
    const at = scheduledDate(draft);
    const estimateQuery = useCampaignEstimate(
        {
            segment_ids: draft.segmentIds,
            email_tag_ids: draft.emailTagIds,
            daily_limit: Math.min(5000, Math.max(3, draft.dailyLimit || 3)),
            days: draft.days || undefined,
            timezone: draft.timezone,
            // A saved draft's own per-day windows stand when it has them.
            start_time: !meta?.customWindows && draft.startTime < draft.endTime ? draft.startTime : undefined,
            end_time: !meta?.customWindows && draft.startTime < draft.endTime ? draft.endTime : undefined,
            start_date: at && at.getTime() > Date.now() ? at.toISOString() : undefined,
            campaign_id: existing?.id,
            step_waits: lockedEmails ? lockedEmails.slice(1).map((s) => Math.max(0, s.wait_after)) : stepWaits(draft),
        },
        open && !loading,
    );
    const estimate: EstimateState = {
        data: estimateQuery.data,
        loading: estimateQuery.isFetching,
        error: estimateQuery.isError,
    };
    // Projection days are midnights in the campaign's zone.
    const tz = draft.timezone || org.data?.timezone || undefined;
    const tzLabel = draft.timezone || (org.data?.timezone ? `${org.data.timezone} (workspace)` : "Workspace timezone");

    const issue = stepIssue(current.key, draft);
    React.useEffect(() => {
        if (!issue) setNudged(false);
    }, [issue]);

    // A step is reachable when every step before it is complete.
    const canReach = React.useCallback(
        (target: number) => {
            for (let i = 0; i < target; i++) if (stepIssue(STEPS[i].key, draft)) return false;
            return true;
        },
        [draft],
    );

    const goTo = React.useCallback(
        (target: number) => {
            if (target === step) return;
            if (target > step && !canReach(target)) {
                setNudged(true);
                return;
            }
            setDirection(target > step ? 1 : -1);
            setNudged(false);
            setStep(target);
        },
        [step, canReach],
    );
    const goToKey = React.useCallback((key: StepKey) => goTo(STEPS.findIndex((s) => s.key === key)), [goTo]);

    const next = React.useCallback(() => {
        if (issue) {
            setNudged(true);
            return;
        }
        if (step < lastStep) goTo(step + 1);
    }, [issue, step, lastStep, goTo]);

    // Why a launch cannot happen yet; a draft can always be saved.
    const e = estimate.data;
    const written = writtenEmails(draft);
    const emailCount = lockedEmails ? lockedEmails.length : written.length;
    const launchBlock: string | null = !canSend
        ? "Launching needs the send campaigns permission; a teammate who has it can start it."
        : emailCount === 0
          ? "There is no email to send yet."
          : !hasLeads || (e && e.recipients === 0 && draft.segmentIds.length === 0)
            ? "It has no leads yet."
            : e && e.recipients === 0
              ? "The chosen lists are empty right now."
              : e && e.mailboxes === 0
                ? "No active mailbox can send it."
                : null;

    const busy = busyWith !== null;
    // A new flow with only a zone picked is still empty.
    const changed = existing
        ? draftSignature(draft) !== baseline
        : draftSignature({ ...draft, timezone: "", nameTouched: false }) !== draftSignature(initialDraft(""));

    // Writes the draft and refreshes every view of it.
    const persist = React.useCallback(async (): Promise<string> => {
        const id = await saveServerDraft(draft, savedName, existing, (created, stepIds) => setExisting({ id: created, meta: { ...freshMeta(), knownStepIds: stepIds } }));
        await Promise.all([
            queryClient.invalidateQueries({ queryKey: ["campaigns"] }),
            queryClient.invalidateQueries({ queryKey: ["segments"] }),
        ]);
        return id;
    }, [draft, savedName, existing, queryClient]);

    // Closing saves what is there; an untouched flow leaves nothing behind.
    const requestClose = React.useCallback(async () => {
        if (busy) return;
        if (loading || !changed) {
            onClose();
            return;
        }
        setBusyWith("close");
        try {
            await persist();
            toast.success(existing ? "Draft saved." : "Saved as a draft. It is in your campaigns list.");
            onClose();
        } catch (err) {
            toast.error(`Could not save the draft: ${buildError(err as AppError)}`);
        } finally {
            setBusyWith(null);
        }
    }, [busy, loading, changed, persist, existing, onClose]);

    // Leaves for another page, saving first.
    const leaveTo = React.useCallback(
        async (to: string, message?: string) => {
            if (busy) return;
            setBusyWith("leave");
            try {
                if (changed) await persist();
                onClose();
                navigate({ to });
                if (message) toast(message);
            } catch (err) {
                toast.error(`Could not save the draft: ${buildError(err as AppError)}`);
            } finally {
                setBusyWith(null);
            }
        },
        [busy, changed, persist, onClose, navigate],
    );

    const discard = React.useCallback(() => {
        if (!existing) {
            confirm.show("Discard this campaign? Nothing of it has been saved.", async () => onClose());
            return;
        }
        const id = existing.id;
        confirm.show("Delete this draft campaign?", async () => {
            setBusyWith("delete");
            try {
                await deleteCampaign.mutateAsync(id);
                toast.success("Draft deleted.");
                onClose();
                if (pathname.startsWith(`/app/campaigns/${id}`)) navigate({ to: "/app/campaigns", replace: true });
            } catch (err) {
                toast.error(buildError(err as AppError));
            } finally {
                setBusyWith(null);
            }
        });
    }, [existing, confirm, onClose, deleteCampaign, pathname, navigate]);

    React.useEffect(() => {
        if (!open) return;
        const onKey = (ev: KeyboardEvent) => {
            if (ev.key !== "Escape") return;
            // An open dropdown, picker or the confirm owns this Escape.
            if (document.querySelector("[data-floating], [role='alertdialog']")) return;
            ev.preventDefault();
            void requestClose();
        };
        document.addEventListener("keydown", onKey);
        return () => document.removeEventListener("keydown", onKey);
    }, [open, requestClose]);

    function importLeads() {
        void leaveTo(
            "/app/contacts",
            changed || existing ? "Saved as a draft. Reopen it from Campaigns when your leads are in." : undefined,
        );
    }

    async function submit(mode: "draft" | "launch") {
        if (busy) return;
        const bad = firstIssue(draft);
        if (bad) {
            const idx = STEPS.findIndex((s) => s.key === bad.key);
            setDirection(idx > step ? 1 : -1);
            setStep(idx);
            setNudged(true);
            return;
        }
        if (mode === "launch" && launchBlock) return;
        setBusyWith(mode);
        try {
            const id = await persist();
            onClose();
            if (mode === "launch") {
                // The campaign page's launch dialog runs the pre-send checks and the start.
                navigate({ to: "/app/campaigns/$id", params: { id }, search: { launch: "1" } });
            } else {
                toast.success(
                    hasLeads ? "Draft saved. Launch it when you are ready." : "Draft saved. Add leads, then launch it.",
                );
            }
        } catch (err) {
            toast.error(buildError(err as AppError));
        } finally {
            setBusyWith(null);
        }
    }

    const launchLabel = at ? "Schedule" : "Launch";
    const LaunchIcon = at ? CalendarClockIcon : RocketIcon;
    const showRail = current.key === "leads" || current.key === "schedule";

    return (
        <AnimatePresence>
            {open && (
                <motion.div
                    key="overlay"
                    initial={{ opacity: 0 }}
                    animate={{ opacity: 1 }}
                    exit={{ opacity: 0 }}
                    transition={{ duration: 0.15 }}
                    onMouseDown={() => void requestClose()}
                    className="fixed inset-0 z-[110] flex items-center justify-center bg-slate-900/30 backdrop-blur-[2px] px-2 sm:px-4"
                >
                    <motion.div
                        key="card"
                        role="dialog"
                        aria-modal="true"
                        aria-label="New campaign"
                        initial={{ y: 8, opacity: 0, scale: 0.985 }}
                        animate={{ y: 0, opacity: 1, scale: 1 }}
                        exit={{ y: 8, opacity: 0, scale: 0.985 }}
                        transition={{ duration: 0.18, ease: [0.22, 1, 0.36, 1] }}
                        onMouseDown={(ev) => ev.stopPropagation()}
                        className="w-full max-w-[1120px] h-[min(92dvh,880px)] rounded-lg bg-white border border-slate-200 shadow-[0_24px_48px_-12px_rgba(15,23,42,0.18),0_8px_16px_-8px_rgba(15,23,42,0.1)] overflow-hidden flex flex-col"
                    >
                        <Header
                            name={loading ? "" : finalName}
                            editing={!!existing}
                            saving={busyWith === "close"}
                            onOpenPage={existing ? () => void leaveTo(`/app/campaigns/${existing.id}`) : undefined}
                            onClose={() => void requestClose()}
                        />
                        <Stepper step={step} canReach={canReach} goTo={goTo} draft={draft} />

                        <div className="flex-1 min-h-0 flex">
                            <div className="flex-1 min-w-0 overflow-y-auto overflow-x-hidden">
                                {loading ? (
                                    <div className="h-full flex items-center justify-center gap-2 text-[12.5px] text-slate-500">
                                        <Loader2Icon className="w-4 h-4 animate-spin text-slate-400" />
                                        Opening the draft…
                                    </div>
                                ) : (
                                <AnimatePresence mode="wait" initial={false} custom={direction}>
                                    <motion.div
                                        key={current.key}
                                        custom={direction}
                                        variants={paneVariants}
                                        initial="enter"
                                        animate="center"
                                        exit="exit"
                                        transition={{ duration: 0.18, ease: [0.22, 1, 0.36, 1] }}
                                        className="px-4 sm:px-6 py-5"
                                    >
                                        {current.key === "leads" && (
                                            <LeadsStep
                                                draft={draft}
                                                patch={patch}
                                                placeholderName={placeholderName}
                                                estimate={estimate}
                                                existingLeads={meta?.leadCount}
                                                onImport={importLeads}
                                                onEnter={next}
                                            />
                                        )}
                                        {current.key === "emails" && (
                                            <EmailsStep
                                                draft={draft}
                                                patch={patch}
                                                selected={emailIndex}
                                                setSelected={setEmailIndex}
                                                lockedSteps={lockedEmails}
                                                onOpenSteps={existing ? () => void leaveTo(`/app/campaigns/${existing.id}/steps`) : undefined}
                                            />
                                        )}
                                        {current.key === "schedule" && <ScheduleStep draft={draft} patch={patch} estimate={estimate} tz={tz} meta={meta} />}
                                        {current.key === "review" && (
                                            <ReviewStep
                                                draft={draft}
                                                estimate={estimate}
                                                tz={tz}
                                                tzLabel={tzLabel}
                                                finalName={finalName}
                                                segmentNames={segmentNames}
                                                launchBlock={launchBlock}
                                                lockedSteps={lockedEmails}
                                                customWindows={meta?.customWindows}
                                                existingLeads={meta?.leadCount}
                                                goTo={goToKey}
                                            />
                                        )}
                                    </motion.div>
                                </AnimatePresence>
                                )}
                            </div>
                            {showRail && !loading && <LaunchPlanRail draft={draft} estimate={estimate} tz={tz} existingLeads={meta?.leadCount} />}
                        </div>

                        <div className="px-3 min-h-12 py-1.5 sm:py-0 sm:h-12 border-t border-slate-200 flex items-center gap-1.5 shrink-0 bg-slate-50/30">
                            {step > 0 ? (
                                <button
                                    type="button"
                                    onClick={() => goTo(step - 1)}
                                    disabled={busy}
                                    className="h-7 px-2.5 rounded-md text-[12px] text-slate-700 hover:text-slate-900 hover:bg-slate-100 inline-flex items-center gap-1 transition-colors disabled:opacity-50"
                                >
                                    <ChevronLeftIcon className="w-3 h-3" />
                                    Back
                                </button>
                            ) : existing || changed ? (
                                <button
                                    type="button"
                                    onClick={discard}
                                    disabled={busy || loading}
                                    className="h-7 px-2.5 rounded-md text-[12px] text-slate-500 hover:text-rose-600 hover:bg-rose-50 inline-flex items-center gap-1.5 transition-colors disabled:opacity-50"
                                >
                                    {busyWith === "delete" && <Loader2Icon className="w-3 h-3 animate-spin" />}
                                    {existing ? "Delete draft" : "Discard"}
                                </button>
                            ) : (
                                <span className="text-[11px] text-slate-400 pl-1 hidden sm:inline">
                                    Closing saves it as a draft you can reopen from Campaigns.
                                </span>
                            )}

                            <div className="ml-auto flex items-center gap-2 min-w-0">
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
                                {step < lastStep ? (
                                    <button
                                        type="button"
                                        onClick={next}
                                        className="h-7 px-3 rounded-md bg-sky-600 hover:bg-sky-700 text-white text-[12px] font-medium inline-flex items-center gap-1.5 transition-colors shrink-0"
                                    >
                                        {current.key === "emails" && written.length === 0
                                            ? "Write it later"
                                            : current.key === "leads" && draft.segmentIds.length === 0
                                              ? "Continue without leads"
                                              : "Continue"}
                                        <ChevronRightIcon className="w-3 h-3" />
                                    </button>
                                ) : (
                                    <>
                                        <button
                                            type="button"
                                            onClick={() => submit("draft")}
                                            disabled={busy}
                                            className="h-7 px-2.5 rounded-md border border-slate-200 bg-white hover:bg-slate-50 text-slate-700 text-[12px] font-medium inline-flex items-center gap-1.5 transition-colors disabled:opacity-60 shrink-0"
                                        >
                                            {busyWith === "draft" ? <Loader2Icon className="w-3 h-3 animate-spin" /> : <ListChecksIcon className="w-3 h-3" />}
                                            Save as draft
                                        </button>
                                        <button
                                            type="button"
                                            onClick={() => submit("launch")}
                                            disabled={busy || !!launchBlock}
                                            title={launchBlock ?? undefined}
                                            className="h-7 px-3 rounded-md bg-sky-600 hover:bg-sky-700 text-white text-[12px] font-medium inline-flex items-center gap-1.5 transition-colors disabled:bg-slate-200 disabled:text-slate-500 shrink-0"
                                        >
                                            {busyWith === "launch" ? <Loader2Icon className="w-3 h-3 animate-spin" /> : <LaunchIcon className="w-3 h-3" />}
                                            {launchLabel}
                                        </button>
                                    </>
                                )}
                            </div>
                        </div>
                    </motion.div>
                </motion.div>
            )}
        </AnimatePresence>
    );
}

const paneVariants = {
    enter: (dir: 1 | -1) => ({ x: dir * 28, opacity: 0 }),
    center: { x: 0, opacity: 1 },
    exit: (dir: 1 | -1) => ({ x: dir * -28, opacity: 0 }),
};

function Header({
    name,
    editing,
    saving,
    onOpenPage,
    onClose,
}: {
    name: string;
    editing: boolean;
    saving: boolean;
    onOpenPage?: () => void;
    onClose: () => void;
}) {
    return (
        <div className="h-12 px-4 border-b border-slate-200 flex items-center gap-2.5 shrink-0">
            <div className="size-5 rounded bg-slate-100 text-slate-600 flex items-center justify-center">
                <MegaphoneIcon className="w-3 h-3" />
            </div>
            <span className="text-[10px] uppercase tracking-[0.14em] text-slate-400 font-medium shrink-0">
                {editing ? "Draft campaign" : "New campaign"}
            </span>
            <div className="h-4 w-px bg-slate-200 shrink-0" />
            <span className="text-[12.5px] text-slate-900 font-medium truncate">{name}</span>
            <div className="ml-auto flex items-center gap-1 shrink-0">
                {saving && (
                    <span className="inline-flex items-center gap-1.5 text-[11.5px] text-slate-400 pr-1">
                        <Loader2Icon className="w-3 h-3 animate-spin" />
                        Saving
                    </span>
                )}
                {onOpenPage && (
                    <button
                        type="button"
                        onClick={onOpenPage}
                        className="h-7 px-2 rounded-md text-[12px] text-slate-500 hover:text-slate-900 hover:bg-slate-100 inline-flex items-center gap-1.5 transition-colors"
                    >
                        <ExternalLinkIcon className="w-3 h-3" />
                        <span className="hidden sm:inline">Campaign page</span>
                    </button>
                )}
                <button
                    type="button"
                    onClick={onClose}
                    aria-label="Close"
                    className="size-7 rounded-md text-slate-500 hover:text-slate-900 hover:bg-slate-100 inline-flex items-center justify-center transition-colors"
                >
                    <XIcon className="w-3.5 h-3.5" />
                </button>
            </div>
        </div>
    );
}

function Stepper({
    step,
    canReach,
    goTo,
    draft,
}: {
    step: number;
    canReach: (s: number) => boolean;
    goTo: (s: number) => void;
    draft: Draft;
}) {
    return (
        <div className="px-4 sm:px-5 h-11 border-b border-slate-100 flex items-center shrink-0 bg-slate-50/40">
            {STEPS.map((s, i) => {
                const active = i === step;
                const done = i < step && !stepIssue(s.key, draft);
                const reachable = i <= step || canReach(i);
                return (
                    <React.Fragment key={s.key}>
                        <button
                            type="button"
                            onClick={() => goTo(i)}
                            disabled={!reachable}
                            aria-current={active ? "step" : undefined}
                            className={cn(
                                "group inline-flex items-center gap-2 h-7 pl-1 pr-2 rounded-md shrink-0 transition-colors outline-none focus-visible:ring-2 focus-visible:ring-sky-100",
                                reachable && !active ? "hover:bg-slate-100" : "",
                                !reachable ? "cursor-default" : "",
                            )}
                        >
                            <span
                                className={cn(
                                    "relative size-5 rounded-full inline-flex items-center justify-center text-[10.5px] font-semibold tabular-nums transition-colors",
                                    done
                                        ? "bg-sky-600 text-white"
                                        : active
                                          ? "bg-white text-sky-700 ring-1 ring-inset ring-sky-600"
                                          : "bg-white text-slate-400 ring-1 ring-inset ring-slate-200",
                                )}
                            >
                                <AnimatePresence mode="wait" initial={false}>
                                    {done ? (
                                        <motion.span
                                            key="check"
                                            initial={{ scale: 0.4, opacity: 0 }}
                                            animate={{ scale: 1, opacity: 1 }}
                                            exit={{ scale: 0.4, opacity: 0 }}
                                            transition={{ duration: 0.16 }}
                                            className="inline-flex"
                                        >
                                            <CheckIcon className="w-3 h-3" strokeWidth={3} />
                                        </motion.span>
                                    ) : (
                                        <motion.span
                                            key="num"
                                            initial={{ scale: 0.4, opacity: 0 }}
                                            animate={{ scale: 1, opacity: 1 }}
                                            exit={{ scale: 0.4, opacity: 0 }}
                                            transition={{ duration: 0.16 }}
                                        >
                                            {i + 1}
                                        </motion.span>
                                    )}
                                </AnimatePresence>
                            </span>
                            <span
                                className={cn(
                                    "text-[11.5px] font-medium whitespace-nowrap",
                                    active ? "text-slate-900" : done ? "text-slate-600" : "text-slate-400",
                                    active ? "inline" : "hidden sm:inline",
                                )}
                            >
                                {s.label}
                            </span>
                        </button>
                        {i < STEPS.length - 1 && (
                            <span className="relative flex-1 h-px mx-1 sm:mx-2 bg-slate-200 min-w-3 overflow-hidden">
                                <motion.span
                                    initial={false}
                                    animate={{ scaleX: i < step ? 1 : 0 }}
                                    transition={{ duration: 0.25, ease: [0.22, 1, 0.36, 1] }}
                                    style={{ originX: 0 }}
                                    className="absolute inset-0 bg-sky-600"
                                />
                            </span>
                        )}
                    </React.Fragment>
                );
            })}
        </div>
    );
}
