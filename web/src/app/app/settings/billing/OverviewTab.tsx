// Billing > Overview — what the workspace is on, what it is consuming, and
// every control that acts on the subscription.
//
// Everything here reads real server state rather than the marketing catalog:
// limits come from /subscription/limits and /organization/limits, capability
// flags from /subscription/features, the trial countdown from
// /subscription/trial. The catalog is only used for labels and colours.

import React from "react";
import { Link } from "react-router-dom";
import { AnimatePresence, motion } from "framer-motion";
import toast from "react-hot-toast";
import {
    AlertTriangleIcon,
    ArrowRightIcon,
    CheckIcon,
    CreditCardIcon,
    FileTextIcon,
    InfoIcon,
    Loader2Icon,
    PlayIcon,
    SlidersHorizontalIcon,
    SparklesIcon,
    XIcon,
} from "lucide-react";
import useFeatureAccess from "@/hooks/useFeatureAccess";
import useUpgradeFlow from "@/hooks/useUpgradeFlow";
import { useConfirm } from "@/hooks/context/confirm";
import useSubscription from "@/lib/api/hooks/app/subscription/useSubscription";
import useSubscriptionLimits from "@/lib/api/hooks/app/subscription/useSubscriptionLimits";
import useTrialStatus from "@/lib/api/hooks/app/subscription/useTrialStatus";
import useCancelSubscription from "@/lib/api/hooks/app/subscription/useCancelSubscription";
import useOrganizationLimits from "@/lib/api/hooks/app/organizations/useOrganizationLimits";
import useUsageOverview from "@/lib/api/hooks/app/analytics/useUsageOverview";
import type { AppError } from "@/lib/api/client/normalizeError";
import buildError from "@/lib/helper/buildError";
import { AnimatedNumber, DitherMeter, type DitherTone } from "@/components/ui/dither";
import { PLAN_ACCENT_CLASSES, getPlan } from "@/lib/plans";
import { Section } from "../_components/SectionShell";

export default function OverviewTab({ onChangePlan }: { onChangePlan: () => void }) {
    const access = useFeatureAccess();
    const sub = useSubscription();
    const trial = useTrialStatus();
    const subLimits = useSubscriptionLimits();
    const orgLimits = useOrganizationLimits();
    const usage = useUsageOverview().data;
    const cancel = useCancelSubscription();
    const flow = useUpgradeFlow();
    const confirm = useConfirm();

    const plan = getPlan(access.plan);
    const accent = PLAN_ACCENT_CLASSES[plan.accent];
    const status = sub.data?.status;
    const cancelAtEnd = sub.data?.cancel_at_period_end;
    const periodEnd = sub.data?.current_period_end
        ? new Date(sub.data.current_period_end as unknown as string)
        : null;
    const onFreeTier = !access.paid;

    async function scheduleCancel() {
        confirm.show(
            `Cancel the ${plan.label} plan? It stays active until ${fmtDate(periodEnd) || "the end of the current period"}, then the workspace returns to the free tier. Mailboxes, warmup and settings are kept.`,
            async () => {
                await toast.promise(cancel.mutateAsync({ cancel_at_period_end: true }), {
                    loading: "Scheduling cancellation…",
                    success: "Cancellation scheduled for the end of the period",
                    error: (e: AppError) => buildError(e),
                });
            },
        );
    }

    async function resume() {
        try {
            await toast.promise(cancel.mutateAsync({ cancel_at_period_end: false }), {
                loading: "Resuming subscription…",
                success: "Subscription resumed",
                error: (e: AppError) => buildError(e),
            });
        } catch {
            /* surfaced via toast */
        }
    }

    return (
        <>
            {/* Attention strip: only rendered when something needs a decision. */}
            <AnimatePresence initial={false}>
                {status === "past_due" && (
                    <Banner
                        key="past_due"
                        tone="red"
                        icon={AlertTriangleIcon}
                        title="Payment failed"
                        body="Stripe could not charge the card on file. Sending is at risk until the invoice clears."
                        action={{ label: "Update payment method", onClick: flow.openPortal }}
                    />
                )}
                {cancelAtEnd && status !== "canceled" && (
                    <Banner
                        key="cancel_at_end"
                        tone="amber"
                        icon={AlertTriangleIcon}
                        title={`Cancels on ${fmtDate(periodEnd) || "the end of the period"}`}
                        body="The plan stays fully active until then. Resume any time before that date and nothing changes."
                        action={{ label: cancel.isPending ? "Resuming…" : "Resume subscription", onClick: resume }}
                    />
                )}
                {trial.data?.is_trial && (trial.data.days_remaining ?? 0) >= 0 && (
                    <Banner
                        key="trial"
                        tone="sky"
                        icon={SparklesIcon}
                        title={`${trial.data.days_remaining ?? 0} ${(trial.data.days_remaining ?? 0) === 1 ? "day" : "days"} left in your trial`}
                        body={`Your trial ends on ${fmtDate(toDate(trial.data.trial_end)) || "its end date"}. Pick a plan before then to keep sending without interruption.`}
                        action={{ label: "Choose a plan", onClick: onChangePlan }}
                    />
                )}
            </AnimatePresence>

            {/* ── Subscription ─────────────────────────────────────────── */}
            <Section
                eyebrow="Subscription"
                description="What this workspace is on right now, and everything you can do to it."
            >
                {sub.isPending ? (
                    <div className="h-24 rounded bg-slate-100 animate-pulse" />
                ) : (
                    <div className="rounded-lg border border-slate-200 overflow-hidden">
                        <div className="px-4 py-4 flex flex-wrap items-start gap-4 bg-gradient-to-b from-slate-50/60 to-white">
                            <div className={`size-10 rounded-lg flex items-center justify-center shrink-0 border ${accent.pill}`}>
                                <SparklesIcon className="w-4.5 h-4.5" />
                            </div>
                            <div className="min-w-0 flex-1 basis-[220px]">
                                <div className="flex items-center gap-2 flex-wrap">
                                    <span className="text-[17px] font-semibold text-slate-900 tracking-tight">
                                        {plan.label}
                                    </span>
                                    <StatusPill status={status} cancelAtEnd={cancelAtEnd} />
                                </div>
                                <p className="text-[12px] text-slate-500 mt-1 leading-relaxed max-w-md">
                                    {plan.description}
                                </p>
                            </div>
                            <div className="flex flex-wrap gap-x-6 gap-y-2 shrink-0">
                                <Stat
                                    label="Price"
                                    value={plan.priceMonthly == null ? "Custom" : `$${plan.priceMonthly}`}
                                    sub={plan.priceMonthly == null ? "contact sales" : "per month"}
                                />
                                <Stat
                                    label={cancelAtEnd ? "Ends" : "Renews"}
                                    value={fmtDate(periodEnd) || "—"}
                                    sub={periodEnd ? relativeDays(periodEnd) : onFreeTier ? "no subscription" : ""}
                                />
                                <Stat
                                    label="Daily sends"
                                    value={
                                        subLimits.data?.max_emails_per_day != null
                                            ? subLimits.data.max_emails_per_day.toLocaleString()
                                            : plan.sendsPerDay === Number.POSITIVE_INFINITY
                                              ? "Custom"
                                              : plan.sendsPerDay.toLocaleString()
                                    }
                                    sub="per mailbox pool"
                                />
                            </div>
                        </div>

                        {/* Control cluster — every action that changes the subscription. */}
                        <div className="px-3 py-2.5 border-t border-slate-200 bg-white flex flex-wrap items-center gap-1.5">
                            <button
                                type="button"
                                onClick={onChangePlan}
                                className="h-7 px-3 rounded-md bg-slate-900 hover:bg-slate-800 text-white text-[12px] font-medium inline-flex items-center gap-1.5 transition-colors"
                            >
                                <SparklesIcon className="w-3 h-3" />
                                {onFreeTier ? "Choose a plan" : "Change plan"}
                            </button>
                            <button
                                type="button"
                                onClick={flow.openPortal}
                                disabled={flow.portalPending}
                                className="h-7 px-2.5 rounded-md border border-slate-200 hover:border-slate-300 text-[12px] text-slate-700 hover:text-slate-900 inline-flex items-center gap-1.5 transition-colors disabled:opacity-60"
                            >
                                {flow.portalPending ? (
                                    <Loader2Icon className="w-3 h-3 animate-spin" />
                                ) : (
                                    <CreditCardIcon className="w-3 h-3" />
                                )}
                                Payment method
                            </button>
                            <button
                                type="button"
                                onClick={flow.openPortal}
                                disabled={flow.portalPending}
                                className="h-7 px-2.5 rounded-md border border-slate-200 hover:border-slate-300 text-[12px] text-slate-700 hover:text-slate-900 inline-flex items-center gap-1.5 transition-colors disabled:opacity-60"
                            >
                                <FileTextIcon className="w-3 h-3" />
                                Invoices
                            </button>
                            <Link
                                to="/app/settings/limits"
                                className="h-7 px-2.5 rounded-md border border-slate-200 hover:border-slate-300 text-[12px] text-slate-700 hover:text-slate-900 inline-flex items-center gap-1.5 transition-colors"
                            >
                                <SlidersHorizontalIcon className="w-3 h-3" />
                                Request a limit increase
                            </Link>
                            {!onFreeTier && (
                                <div className="ml-auto">
                                    {cancelAtEnd ? (
                                        <button
                                            type="button"
                                            onClick={resume}
                                            disabled={cancel.isPending}
                                            className="h-7 px-2.5 rounded-md border border-emerald-200 bg-emerald-50 hover:bg-emerald-100 text-[12px] font-medium text-emerald-700 inline-flex items-center gap-1.5 transition-colors disabled:opacity-60"
                                        >
                                            {cancel.isPending ? (
                                                <Loader2Icon className="w-3 h-3 animate-spin" />
                                            ) : (
                                                <PlayIcon className="w-3 h-3" />
                                            )}
                                            Resume
                                        </button>
                                    ) : (
                                        <button
                                            type="button"
                                            onClick={scheduleCancel}
                                            disabled={cancel.isPending}
                                            className="h-7 px-2.5 rounded-md text-[12px] text-slate-500 hover:text-red-700 hover:bg-red-50 inline-flex items-center gap-1.5 transition-colors disabled:opacity-60"
                                        >
                                            <XIcon className="w-3 h-3" />
                                            Cancel plan
                                        </button>
                                    )}
                                </div>
                            )}
                        </div>
                    </div>
                )}
            </Section>

            {/* ── Usage against real limits ────────────────────────────── */}
            <Section
                eyebrow="Usage and limits"
                description="Live counts against the limits the server is actually enforcing, not the marketing numbers."
                actions={
                    <Link
                        to="/app/settings/limits"
                        className="text-[11.5px] font-medium text-slate-500 hover:text-slate-900 inline-flex items-center gap-1 transition-colors"
                    >
                        Request an increase
                        <ArrowRightIcon className="w-3 h-3" />
                    </Link>
                }
            >
                {orgLimits.isPending && subLimits.isPending ? (
                    <div className="h-32 rounded bg-slate-100 animate-pulse" />
                ) : (
                    <div className="grid grid-cols-1 sm:grid-cols-2 gap-x-8 gap-y-4">
                        <UsageMeter
                            label="Mailboxes"
                            hint="Connected sending and warmup mailboxes"
                            current={usage?.email_accounts.total ?? orgLimits.data?.current_emails ?? 0}
                            max={subLimits.data?.max_email_accounts}
                        />
                        <UsageMeter
                            label="Contacts"
                            hint="Recipient records stored in this workspace"
                            current={orgLimits.data?.current_contacts ?? usage?.contacts.total ?? 0}
                            max={orgLimits.data?.max_contacts ?? subLimits.data?.max_contacts}
                        />
                        <UsageMeter
                            label="Campaigns"
                            hint="Campaigns created in this workspace"
                            current={orgLimits.data?.current_campaigns ?? usage?.campaigns.total ?? 0}
                            max={orgLimits.data?.max_campaigns ?? subLimits.data?.max_campaigns}
                        />
                        <UsageMeter
                            label="Team members"
                            hint="Seats used on this workspace"
                            current={orgLimits.data?.current_members ?? 0}
                            max={orgLimits.data?.max_members ?? subLimits.data?.max_team_members}
                        />
                        <UsageMeter
                            label="Sends this period"
                            hint="Campaign emails delivered in the current billing period"
                            current={usage?.campaigns.emails_sent ?? 0}
                            max={undefined}
                        />
                        <UsageMeter
                            label="API calls today"
                            hint="Requests made with API keys against the daily quota"
                            current={usage?.api.total_calls ?? 0}
                            max={usage?.api.daily_limit}
                        />
                    </div>
                )}
                <p className="text-[11px] text-slate-400 leading-relaxed pt-1 inline-flex items-start gap-1.5">
                    <InfoIcon className="w-3 h-3 mt-0.5 shrink-0" />
                    A meter with no cap means the limit is unmetered on this plan. Warmup volume is
                    governed per mailbox and is not capped here.
                </p>
            </Section>

        </>
    );
}

/* ── pieces ──────────────────────────────────────────────────────── */

function Banner({
    tone,
    icon: Icon,
    title,
    body,
    action,
}: {
    tone: "red" | "amber" | "sky";
    icon: React.ComponentType<{ className?: string }>;
    title: string;
    body: string;
    action?: { label: string; onClick: () => void };
}) {
    const tones = {
        red: "bg-red-50 border-red-200 text-red-900",
        amber: "bg-amber-50 border-amber-200 text-amber-900",
        sky: "bg-sky-50 border-sky-200 text-sky-900",
    } as const;
    const btn = {
        red: "bg-red-600 hover:bg-red-700 text-white",
        amber: "bg-amber-600 hover:bg-amber-700 text-white",
        sky: "bg-sky-600 hover:bg-sky-700 text-white",
    } as const;
    return (
        <motion.div
            initial={{ opacity: 0, height: 0 }}
            animate={{ opacity: 1, height: "auto" }}
            exit={{ opacity: 0, height: 0 }}
            transition={{ duration: 0.22, ease: [0.22, 1, 0.36, 1] }}
            className="overflow-hidden"
        >
            <div className={`mx-4 md:mx-8 mt-4 rounded-lg border px-3.5 py-3 flex flex-wrap items-start gap-3 ${tones[tone]}`}>
                <Icon className="w-4 h-4 mt-0.5 shrink-0" />
                <div className="min-w-0 flex-1 basis-[200px]">
                    <div className="text-[12.5px] font-semibold">{title}</div>
                    <p className="text-[12px] opacity-80 leading-relaxed mt-0.5">{body}</p>
                </div>
                {action && (
                    <button
                        type="button"
                        onClick={action.onClick}
                        className={`h-7 px-2.5 rounded-md text-[12px] font-medium transition-colors shrink-0 ${btn[tone]}`}
                    >
                        {action.label}
                    </button>
                )}
            </div>
        </motion.div>
    );
}

function Stat({ label, value, sub }: { label: string; value: string; sub?: string }) {
    return (
        <div>
            <div className="text-[10px] uppercase tracking-[0.12em] text-slate-400 font-medium">
                {label}
            </div>
            <div className="text-[14px] font-semibold text-slate-900 tabular-nums mt-0.5">{value}</div>
            {sub && <div className="text-[10.5px] text-slate-400">{sub}</div>}
        </div>
    );
}

function UsageMeter({
    label,
    hint,
    current,
    max,
}: {
    label: string;
    hint: string;
    current: number;
    max?: number | null;
}) {
    const capped = typeof max === "number" && max > 0 && Number.isFinite(max);
    const pct = capped ? Math.min(100, Math.round((current / (max as number)) * 100)) : 0;
    const tone: DitherTone = pct >= 90 ? "rose" : pct >= 70 ? "amber" : "sky";
    return (
        <div>
            <div className="flex items-baseline justify-between gap-2 mb-1">
                <span className="text-[12px] text-slate-700 font-medium">{label}</span>
                <span className="text-[11.5px] font-mono tabular-nums text-slate-700">
                    <AnimatedNumber value={current} />
                    <span className="text-slate-400">
                        {capped ? ` / ${(max as number).toLocaleString()}` : " / unmetered"}
                    </span>
                </span>
            </div>
            <DitherMeter frac={capped ? pct / 100 : 0} tone={tone} height={4} />
            <div className="flex items-center justify-between gap-2 mt-1">
                <span className="text-[10.5px] text-slate-400 leading-snug">{hint}</span>
                {capped && pct >= 70 && (
                    <span
                        className={`text-[10px] font-semibold shrink-0 ${pct >= 90 ? "text-red-600" : "text-amber-600"}`}
                    >
                        {pct}% used
                    </span>
                )}
            </div>
        </div>
    );
}

function StatusPill({
    status,
    cancelAtEnd,
}: {
    status: string | undefined;
    cancelAtEnd: boolean | undefined;
}) {
    const base = "inline-flex items-center gap-1 text-[10px] rounded px-1.5 h-4 uppercase tracking-[0.1em] font-medium border";
    if (!status) {
        return <span className={`${base} bg-slate-100 text-slate-500 border-slate-200`}>Free</span>;
    }
    if (status === "trialing") {
        return <span className={`${base} bg-emerald-50 text-emerald-700 border-emerald-100`}>Trialing</span>;
    }
    if (status === "past_due") {
        return <span className={`${base} bg-red-50 text-red-700 border-red-100`}>Past due</span>;
    }
    if (status === "canceled") {
        return <span className={`${base} bg-slate-100 text-slate-500 border-slate-200`}>Canceled</span>;
    }
    if (cancelAtEnd) {
        return <span className={`${base} bg-amber-50 text-amber-700 border-amber-100`}>Ending soon</span>;
    }
    return (
        <span className={`${base} bg-emerald-50 text-emerald-700 border-emerald-100`}>
            <CheckIcon className="w-2 h-2" />
            Active
        </span>
    );
}

/* ── helpers ─────────────────────────────────────────────────────── */

function toDate(v: Date | string | undefined): Date | null {
    if (!v) return null;
    const d = v instanceof Date ? v : new Date(v);
    return Number.isNaN(d.getTime()) ? null : d;
}

function fmtDate(d: Date | null): string {
    if (!d || Number.isNaN(d.getTime())) return "";
    return d.toLocaleDateString("en-US", { month: "long", day: "numeric", year: "numeric" });
}

function relativeDays(d: Date): string {
    const days = Math.round((d.getTime() - Date.now()) / 86_400_000);
    if (days < 0) return "past";
    if (days === 0) return "today";
    if (days === 1) return "tomorrow";
    return `in ${days} days`;
}
