// MailboxAllowanceDialog — what opens when a workspace runs into its mailbox
// allowance, and what "Request more" links to before it does.
//
// Every paid plan holds unlimited mailboxes under a fair-use allowance: one
// mailbox for every N daily sends the plan includes (N is 1 today). This dialog explains that
// number, offers the plan that raises it, and takes a limit-increase request
// inline so nobody has to leave the connect flow to ask. A free workspace is
// pointed at a plan instead, because that is its only path to more.
//
// Layered above the connect modal: it marks itself data-floating so the modal
// underneath ignores Escape while it is up, and stops both mousedown and
// click. The portal keeps it in the modal's React tree, so without that a
// click anywhere in here would reach the modal's backdrop handler and close
// the whole connect flow.

import React from "react";
import { createPortal } from "react-dom";
import { Link } from "react-router-dom";
import { AnimatePresence, motion } from "framer-motion";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import toast from "react-hot-toast";
import {
    ArrowRightIcon,
    ArrowUpRightIcon,
    CheckIcon,
    ClockIcon,
    Loader2Icon,
    MailboxIcon,
    SparklesIcon,
    XIcon,
} from "lucide-react";
import { useUpgradeDialog } from "@/hooks/context/upgrade";
import { useConfirm } from "@/hooks/context/confirm";
import { useCurrentOrg } from "@/stores/useAppStore";
import { DitherMeter, type DitherTone } from "@/components/ui/dither";
import { NumberInput } from "@/components/ui/field";
import submitLimitRequest from "@/lib/api/client/app/organizations/submitLimitRequest";
import cancelLimitRequest from "@/lib/api/client/app/organizations/cancelLimitRequest";
import { MAILBOX_ALLOWANCE_KEY } from "@/lib/api/hooks/app/emails/useMailboxAllowance";
import type MailboxAllowance from "@/lib/api/models/app/emails/MailboxAllowance";
import type { AppError } from "@/lib/api/client/normalizeError";
import buildError from "@/lib/helper/buildError";
import { PAID_PLANS, PLAN_CATALOG, getPlan, planOrder, type PlanID } from "@/lib/plans";
import { cn } from "@/lib/utils";

const EASE = [0.22, 1, 0.36, 1] as const;

/** Mailboxes a catalogue plan holds under fair use; null for unlimited. */
function planMailboxes(id: PlanID, sendsPerMailbox: number): number | null {
    const sends = PLAN_CATALOG[id].sendsPerDay;
    if (!Number.isFinite(sends) || sends <= 0) return null;
    return Math.ceil(sends / Math.max(1, sendsPerMailbox));
}

/** The cheapest paid plan above the current one that holds more mailboxes. */
function nextPlanWithMore(a: MailboxAllowance, current: PlanID): PlanID | null {
    const have = a.allowance ?? Number.POSITIVE_INFINITY;
    for (const id of PAID_PLANS) {
        if (planOrder(id) <= planOrder(current)) continue;
        const n = planMailboxes(id, a.sends_per_mailbox);
        if (n == null || n > have) return id;
    }
    return null;
}

/** "one mailbox per daily send", or "one mailbox for every N daily sends". */
function perSend(n: number): string {
    return n <= 1 ? "one mailbox for every send a day" : `one mailbox for every ${n} sends a day`;
}

/** A sensible starting number for a request: half again, on a round figure. */
function suggestedRequest(allowance: number): number {
    const raw = Math.ceil(allowance * 1.5);
    const step = raw >= 1000 ? 500 : raw >= 100 ? 50 : 10;
    return Math.ceil(raw / step) * step;
}

export default function MailboxAllowanceDialog({
    open,
    onClose,
    allowance,
    reached,
    currentPlan,
}: {
    open: boolean;
    onClose: () => void;
    allowance: MailboxAllowance | undefined;
    /** True when a connect was just refused, which changes the title. */
    reached?: boolean;
    /** The workspace's plan id from useFeatureAccess. */
    currentPlan: PlanID;
}) {
    const cardRef = React.useRef<HTMLDivElement>(null);

    React.useEffect(() => {
        if (!open) return;
        const previous = document.activeElement as HTMLElement | null;
        cardRef.current?.focus();
        const onKey = (e: KeyboardEvent) => {
            if (e.key !== "Escape") return;
            // The confirm owns Escape while it is up.
            if (document.querySelector("[role='alertdialog']")) return;
            e.stopPropagation();
            onClose();
        };
        document.addEventListener("keydown", onKey, true);
        return () => {
            document.removeEventListener("keydown", onKey, true);
            previous?.focus?.();
        };
    }, [open, onClose]);

    return createPortal(
        <AnimatePresence>
            {open && (
                <motion.div
                    key="allowance-overlay"
                    initial={{ opacity: 0 }}
                    animate={{ opacity: 1 }}
                    exit={{ opacity: 0 }}
                    transition={{ duration: 0.15 }}
                    onMouseDown={(e) => {
                        e.stopPropagation();
                        onClose();
                    }}
                    onClick={(e) => e.stopPropagation()}
                    className="fixed inset-0 z-[140] flex items-center justify-center bg-slate-900/40 backdrop-blur-[2px] px-4"
                >
                    <motion.div
                        key="allowance-card"
                        ref={cardRef}
                        tabIndex={-1}
                        role="dialog"
                        aria-modal="true"
                        aria-label="Mailbox allowance"
                        data-floating
                        data-nested-modal
                        initial={{ y: 10, opacity: 0, scale: 0.98 }}
                        animate={{ y: 0, opacity: 1, scale: 1 }}
                        exit={{ y: 10, opacity: 0, scale: 0.98 }}
                        transition={{ duration: 0.2, ease: EASE }}
                        onMouseDown={(e) => e.stopPropagation()}
                        className="w-full max-w-[540px] rounded-lg bg-white border border-slate-200 shadow-[0_24px_48px_-12px_rgba(15,23,42,0.22),0_8px_16px_-8px_rgba(15,23,42,0.12)] overflow-hidden flex flex-col max-h-[88dvh] outline-none"
                    >
                        <div className="h-12 px-4 border-b border-slate-200 flex items-center gap-2.5 shrink-0">
                            <MailboxIcon className="w-3.5 h-3.5 text-slate-500" />
                            <span className="text-[10px] uppercase tracking-[0.14em] text-slate-400 font-medium">
                                Mailbox allowance
                            </span>
                            <button
                                type="button"
                                onClick={onClose}
                                aria-label="Close"
                                className="ml-auto size-7 rounded-md text-slate-500 hover:text-slate-900 hover:bg-slate-100 inline-flex items-center justify-center transition-colors"
                            >
                                <XIcon className="w-3.5 h-3.5" />
                            </button>
                        </div>
                        <div className="flex-1 min-h-0 overflow-y-auto">
                            {allowance ? (
                                <Body allowance={allowance} reached={!!reached} currentPlan={currentPlan} onClose={onClose} />
                            ) : (
                                <div className="p-6 flex items-center justify-center text-slate-400">
                                    <Loader2Icon className="w-4 h-4 animate-spin" />
                                </div>
                            )}
                        </div>
                    </motion.div>
                </motion.div>
            )}
        </AnimatePresence>,
        document.body,
    );
}

function Body({
    allowance: a,
    reached,
    currentPlan,
    onClose,
}: {
    allowance: MailboxAllowance;
    reached: boolean;
    currentPlan: PlanID;
    onClose: () => void;
}) {
    const upgrade = useUpgradeDialog();
    const cap = a.allowance;
    const full = cap != null && (a.remaining ?? 0) <= 0;
    const pct = cap ? Math.min(100, Math.round((a.used / cap) * 100)) : 0;
    const tone: DitherTone = pct >= 90 ? "rose" : pct >= 70 ? "amber" : "sky";
    const planLabel = a.plan_name || getPlan(currentPlan).label;
    const next = a.paid ? nextPlanWithMore(a, currentPlan) : null;

    const title = reached
        ? full
            ? "You've reached your mailbox allowance"
            : "This connect would pass your mailbox allowance"
        : cap == null
          ? "This workspace holds unlimited mailboxes"
          : "Your mailbox allowance";

    function openPlans(feature: string, minPlan: PlanID, blurb: string) {
        onClose();
        upgrade.open({
            feature,
            minPlan,
            blurb,
            bullets: PAID_PLANS.filter((id) => id !== "enterprise").map((id) => {
                const n = planMailboxes(id, a.sends_per_mailbox);
                return `${PLAN_CATALOG[id].label}: ${n == null ? "unlimited" : n.toLocaleString()} mailboxes`;
            }),
        });
    }

    return (
        <div className="p-4 space-y-4">
            <div>
                <h2 className="text-[15px] font-semibold text-slate-900 leading-snug">{title}</h2>
                <p className="text-[12.5px] text-slate-600 mt-1 leading-relaxed">
                    <Explanation a={a} planLabel={planLabel} />
                </p>
            </div>

            {cap != null && (
                <div className="rounded-md border border-slate-200 p-3">
                    <div className="flex items-baseline justify-between gap-2 mb-1.5">
                        <span className="text-[12px] text-slate-700 font-medium">Connected mailboxes</span>
                        <span className="text-[11.5px] font-mono tabular-nums text-slate-700">
                            {a.used.toLocaleString()}
                            <span className="text-slate-400"> / {cap.toLocaleString()}</span>
                        </span>
                    </div>
                    <DitherMeter frac={pct / 100} tone={tone} height={4} />
                    <div className="text-[10.5px] text-slate-400 mt-1.5">
                        {a.basis === "fair_use" && a.plan_daily_sends != null
                            ? `${a.plan_daily_sends.toLocaleString()} sends a day on ${planLabel}, ${perSend(a.sends_per_mailbox)}`
                            : a.basis === "override"
                              ? "Raised for this workspace by an approved request"
                              : a.basis === "free"
                                ? "Free workspace"
                                : `Set by ${planLabel}`}
                    </div>
                </div>
            )}

            {!a.paid ? (
                <div className="rounded-md border border-sky-200 bg-sky-50 p-3">
                    <p className="text-[12.5px] font-medium text-sky-900">A paid plan holds one mailbox for every send a day it includes</p>
                    <ul className="mt-1.5 space-y-0.5">
                        {PAID_PLANS.filter((id) => id !== "enterprise").map((id) => {
                            const n = planMailboxes(id, a.sends_per_mailbox);
                            return (
                                <li key={id} className="text-[12px] text-sky-800 flex items-center gap-1.5">
                                    <CheckIcon className="w-3 h-3 text-sky-600 shrink-0" />
                                    <span className="font-medium">{PLAN_CATALOG[id].label}</span>
                                    <span className="text-sky-700/80">
                                        {PLAN_CATALOG[id].sendsPerDay.toLocaleString()} sends a day, {n == null ? "unlimited" : n.toLocaleString()} mailboxes
                                    </span>
                                </li>
                            );
                        })}
                    </ul>
                    <button
                        type="button"
                        onClick={() =>
                            openPlans(
                                "More mailboxes",
                                "starter",
                                `A free workspace holds ${cap?.toLocaleString() ?? "10"} mailboxes. A paid plan holds one for every send a day it includes, and more on request.`,
                            )
                        }
                        className="mt-2.5 h-7 px-3 rounded-md bg-slate-900 hover:bg-slate-800 text-white text-[12px] font-medium inline-flex items-center gap-1.5 transition-colors"
                    >
                        <SparklesIcon className="w-3 h-3" />
                        Choose a plan
                    </button>
                </div>
            ) : (
                <>
                    {next && (
                        <button
                            type="button"
                            onClick={() =>
                                openPlans(
                                    "More mailboxes",
                                    next,
                                    `${PLAN_CATALOG[next].label} includes ${PLAN_CATALOG[next].sendsPerDay.toLocaleString()} sends a day, which holds ${planMailboxes(next, a.sends_per_mailbox)?.toLocaleString() ?? "unlimited"} mailboxes under fair use.`,
                                )
                            }
                            className="w-full rounded-md border border-slate-200 hover:border-slate-300 hover:bg-slate-50 p-3 text-left flex items-center gap-3 transition-colors group"
                        >
                            <div className="min-w-0 flex-1">
                                <div className="text-[12.5px] font-medium text-slate-900">
                                    Move to {PLAN_CATALOG[next].label} for{" "}
                                    {planMailboxes(next, a.sends_per_mailbox)?.toLocaleString() ?? "unlimited"} mailboxes
                                </div>
                                <div className="text-[11.5px] text-slate-500 mt-0.5">
                                    {PLAN_CATALOG[next].sendsPerDay.toLocaleString()} sends a day. Prorated, takes effect immediately.
                                </div>
                            </div>
                            <ArrowUpRightIcon className="w-4 h-4 text-slate-300 group-hover:text-slate-500 shrink-0" />
                        </button>
                    )}
                    {cap != null && <RequestSection a={a} cap={cap} />}
                </>
            )}

            <div className="flex items-center justify-between gap-2 pt-1">
                <Link
                    to="/app/settings/limits"
                    onClick={onClose}
                    className="text-[11.5px] text-slate-500 hover:text-slate-900 inline-flex items-center gap-1 transition-colors"
                >
                    All limit requests
                    <ArrowRightIcon className="w-3 h-3" />
                </Link>
                <a
                    href="https://docs.warmbly.com/guides/mailboxes/#mailbox-allowance"
                    target="_blank"
                    rel="noreferrer"
                    className="text-[11.5px] text-slate-500 hover:text-slate-900 inline-flex items-center gap-1 transition-colors"
                >
                    How fair use works
                    <ArrowUpRightIcon className="w-3 h-3" />
                </a>
            </div>
        </div>
    );
}

function Explanation({ a, planLabel }: { a: MailboxAllowance; planLabel: string }) {
    switch (a.basis) {
        case "unlimited":
            return <>There is no cap on mailboxes here. Connect as many as you need.</>;
        case "free":
            return (
                <>
                    A free workspace holds up to {a.allowance?.toLocaleString()} mailboxes for warmup. Sending, and mailboxes
                    without a fixed cap, come with a plan.
                </>
            );
        case "override":
            return (
                <>
                    This workspace's allowance was raised to {a.allowance?.toLocaleString()} mailboxes by an approved
                    request. Ask again when you need more.
                </>
            );
        case "plan":
            return <>{planLabel} holds {a.allowance?.toLocaleString()} mailboxes. Ask for more below or move to a bigger plan.</>;
        default:
            return (
                <>
                    Mailboxes are unlimited on {planLabel} under fair use: {perSend(a.sends_per_mailbox)} the plan
                    includes. {a.plan_daily_sends?.toLocaleString()} sends a day holds {a.allowance?.toLocaleString()}{" "}
                    mailboxes, far more than safe sending ever needs. Need more? Ask below and we usually answer within a
                    business day.
                </>
            );
    }
}

function RequestSection({ a, cap }: { a: MailboxAllowance; cap: number }) {
    const qc = useQueryClient();
    const confirm = useConfirm();
    const org = useCurrentOrg();
    const [requested, setRequested] = React.useState<number>(suggestedRequest(cap));
    const [reason, setReason] = React.useState("");
    const pending = a.pending_request ?? null;

    const refresh = () => {
        qc.invalidateQueries({ queryKey: MAILBOX_ALLOWANCE_KEY });
        qc.invalidateQueries({ queryKey: ["organizations"] });
        qc.invalidateQueries({ queryKey: ["app", "organizations"] });
    };

    const submit = useMutation({
        mutationFn: () =>
            submitLimitRequest(org!.id, { field: "max_email_accounts", requested, reason: reason.trim() }),
        onSuccess: () => {
            toast.success("Request sent. We usually answer within a business day.");
            setReason("");
            refresh();
        },
        onError: (e: AppError) => toast.error(buildError(e)),
    });

    const cancel = useMutation({
        mutationFn: (id: string) => cancelLimitRequest(id),
        onSuccess: () => {
            toast.success("Request withdrawn");
            refresh();
        },
        onError: (e: AppError) => toast.error(buildError(e)),
    });

    if (pending) {
        return (
            <div className="rounded-md border border-amber-200 bg-amber-50 p-3">
                <div className="flex items-start gap-2.5">
                    <ClockIcon className="w-3.5 h-3.5 text-amber-600 mt-0.5 shrink-0" />
                    <div className="min-w-0 flex-1">
                        <p className="text-[12.5px] font-medium text-amber-900">
                            You asked for {pending.requested.toLocaleString()} mailboxes on{" "}
                            {new Date(pending.submitted_at).toLocaleDateString()}
                        </p>
                        <p className="text-[11.5px] text-amber-800/90 mt-0.5 leading-relaxed">
                            It is waiting for review. Once approved the new allowance applies here straight away, and
                            anything you could not connect in the meantime goes through on a retry.
                        </p>
                        <button
                            type="button"
                            disabled={cancel.isPending}
                            onClick={() =>
                                confirm.show("Withdraw this request? You can submit a new one at any time.", async () => {
                                    await cancel.mutateAsync(pending.id);
                                })
                            }
                            className="mt-2 h-6 px-2 rounded text-[11px] text-amber-900 hover:bg-amber-100 inline-flex items-center gap-1 transition-colors disabled:opacity-50"
                        >
                            Withdraw request
                        </button>
                    </div>
                </div>
            </div>
        );
    }

    const valid = Number.isInteger(requested) && requested > cap && reason.trim().length >= 10;

    return (
        <form
            onSubmit={(e) => {
                e.preventDefault();
                if (!valid || !org?.id) return;
                submit.mutate();
            }}
            className="rounded-md border border-slate-200 p-3 space-y-2.5"
        >
            <div>
                <p className="text-[12.5px] font-medium text-slate-900">Request an increase</p>
                <p className="text-[11.5px] text-slate-500 mt-0.5 leading-relaxed">
                    Keep your plan and ask for a higher allowance. Say how many mailboxes you need and what you are
                    sending, so review takes one pass.
                </p>
            </div>
            <div className="flex items-center gap-3">
                <span className="text-[10px] uppercase tracking-[0.14em] text-slate-400 font-medium w-20 shrink-0">
                    Mailboxes
                </span>
                <NumberInput min={cap + 1} value={requested} onChange={setRequested} className="w-32" />
                <span className="text-[11px] text-slate-400">now {cap.toLocaleString()}</span>
            </div>
            <div className="flex items-start gap-3">
                <span className="text-[10px] uppercase tracking-[0.14em] text-slate-400 font-medium w-20 shrink-0 pt-1.5">
                    Why
                </span>
                <textarea
                    value={reason}
                    onChange={(e) => setReason(e.target.value)}
                    rows={2}
                    placeholder="Onboarding 40 client domains, 25 mailboxes each, at 5 to 10 sends a day per mailbox."
                    className="flex-1 min-w-0 rounded-md border border-slate-200 bg-white px-2.5 py-1.5 text-[12.5px] text-slate-900 placeholder:text-slate-400 outline-none focus:border-sky-400 focus:ring-2 focus:ring-sky-100 transition-colors resize-none"
                />
            </div>
            <div className="flex items-center justify-between gap-2">
                <span className="text-[10.5px] text-slate-400">
                    {requested <= cap
                        ? `Ask for more than ${cap.toLocaleString()}`
                        : reason.trim().length < 10
                          ? "A sentence on what you are sending helps"
                          : "Reviewed by a person, usually within a business day"}
                </span>
                <button
                    type="submit"
                    disabled={!valid || submit.isPending || !org?.id}
                    className={cn(
                        "h-7 px-3 rounded-md text-[12px] font-medium inline-flex items-center gap-1.5 transition-colors",
                        "bg-slate-900 hover:bg-slate-800 text-white disabled:opacity-50 disabled:cursor-not-allowed",
                    )}
                >
                    {submit.isPending ? <Loader2Icon className="w-3 h-3 animate-spin" /> : <ArrowRightIcon className="w-3 h-3" />}
                    Send request
                </button>
            </div>
        </form>
    );
}
