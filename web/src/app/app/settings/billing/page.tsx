// Billing — owner-only, organization-scoped, flat layout.
//
// Plan data lives in `lib/plans` so the dashboard mirrors warmbly-web
// exactly — every value here (limits, descriptions, bullets) comes
// from the marketing site's pricing.astro.

import React from "react";
import {
    ArrowUpRightIcon,
    CheckIcon,
    CreditCardIcon,
    ExternalLinkIcon,
    FileTextIcon,
    Loader2Icon,
    LockIcon,
    SparklesIcon,
} from "lucide-react";
import { Link } from "react-router-dom";
import toast from "react-hot-toast";
import { TopbarAction } from "@/components/layout/Page";
import useFeatureAccess from "@/hooks/useFeatureAccess";
import useSubscription from "@/lib/api/hooks/app/subscription/useSubscription";
import useCreatePortalSession from "@/lib/api/hooks/app/subscription/useCreatePortalSession";
import { useAppStore } from "@/stores";
import type { AppError } from "@/lib/api/client/normalizeError";
import buildError from "@/lib/helper/buildError";
import { Row, Section, SectionShell } from "../_components/SectionShell";
import { PLAN_ACCENT_CLASSES, PAID_PLANS, getPlan, type PlanID } from "@/lib/plans";

export default function BillingSettingsPage() {
    const access = useFeatureAccess();
    const sub = useSubscription();
    const portal = useCreatePortalSession();
    const currentOrg = useAppStore((s) => s.currentOrganization);

    if (!access.loading && !access.isOwner) {
        return (
            <SectionShell title="Billing" description="Owner only.">
                <Section eyebrow="Permission denied">
                    <div className="flex items-start gap-3">
                        <div className="size-9 rounded-md bg-amber-50 border border-amber-200 text-amber-700 flex items-center justify-center shrink-0">
                            <LockIcon className="w-4 h-4" />
                        </div>
                        <div>
                            <div className="text-[13px] font-semibold text-slate-900">
                                Only the workspace owner can view billing
                            </div>
                            <p className="text-[12px] text-slate-500 leading-relaxed mt-1 max-w-md">
                                Plan changes, invoices and payment methods are scoped to the
                                owner role. Ask your owner to share an update if you need one.
                            </p>
                        </div>
                    </div>
                </Section>
            </SectionShell>
        );
    }

    const currentPlan = getPlan(access.plan);
    const currentAccent = PLAN_ACCENT_CLASSES[currentPlan.accent];
    const status = sub.data?.status;
    const periodEnd = sub.data?.current_period_end
        ? new Date(sub.data.current_period_end as unknown as string)
        : null;
    const cancelAtEnd = sub.data?.cancel_at_period_end;

    async function openPortal() {
        try {
            const { url } = await toast.promise(portal.mutateAsync(), {
                loading: "Opening billing portal…",
                success: "Portal ready",
                error: (e: AppError) => buildError(e),
            });
            window.location.assign(url);
        } catch {
            /* surfaced */
        }
    }

    return (
        <SectionShell
            title="Billing"
            description={`Plan, payment and invoices for ${currentOrg?.name ?? "this workspace"}.`}
            actions={
                <TopbarAction
                    icon={<ExternalLinkIcon className="w-3 h-3" />}
                    onClick={openPortal}
                >
                    {portal.isPending ? "Opening…" : "Manage billing"}
                </TopbarAction>
            }
        >
            <Section
                eyebrow="Current plan"
                description="Your active subscription. Limits below come from the marketing site — same plans, same numbers."
            >
                {sub.isPending ? (
                    <div className="h-20 rounded bg-slate-100 animate-pulse" />
                ) : (
                    <div className="flex items-start gap-4">
                        <div className={`size-9 rounded-md flex items-center justify-center shrink-0 border ${currentAccent.pill}`}>
                            <SparklesIcon className="w-4 h-4" />
                        </div>
                        <div className="min-w-0 flex-1">
                            <div className="flex items-center gap-2 flex-wrap">
                                <span className="text-[15px] font-semibold text-slate-900">
                                    {currentPlan.label}
                                </span>
                                <StatusPill status={status} cancelAtEnd={cancelAtEnd} />
                            </div>
                            <p className="text-[11.5px] text-slate-500 mt-1 leading-relaxed max-w-md">
                                {currentPlan.description}
                            </p>
                            {periodEnd && status !== "canceled" && (
                                <div className="mt-2 text-[11px] text-slate-500">
                                    {cancelAtEnd ? "Ends on " : "Renews on "}
                                    <span className="font-mono tabular-nums text-slate-700">
                                        {periodEnd.toLocaleDateString("en-US", {
                                            month: "long",
                                            day: "numeric",
                                            year: "numeric",
                                        })}
                                    </span>
                                </div>
                            )}
                        </div>
                        <button
                            type="button"
                            onClick={openPortal}
                            className="h-7 px-3 rounded-md bg-slate-900 hover:bg-slate-800 text-white text-[12px] font-medium inline-flex items-center gap-1.5 transition-colors shrink-0"
                        >
                            <SparklesIcon className="w-3 h-3" />
                            {currentPlan.id === "free" ? "Subscribe" : "Change plan"}
                        </button>
                    </div>
                )}
                <ul className="grid grid-cols-2 gap-x-6 gap-y-1.5 mt-1 text-[11.5px] max-w-md">
                    {currentPlan.bullets.map((b) => (
                        <li key={b} className="flex items-start gap-1.5">
                            <CheckIcon className="w-3 h-3 text-emerald-600 mt-0.5 shrink-0" />
                            <span className="text-slate-700">{b}</span>
                        </li>
                    ))}
                </ul>
            </Section>

            <Section
                eyebrow="Compare plans"
                description="Same lineup as the public pricing page."
            >
                <div className="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-4 gap-2">
                    {PAID_PLANS.map((id) => (
                        <PlanCard key={id} id={id} active={currentPlan.id === id} onUpgrade={openPortal} />
                    ))}
                </div>
                <Link
                    to="/#pricing"
                    className="inline-flex items-center gap-1 text-[11.5px] text-slate-500 hover:text-slate-900 transition-colors"
                >
                    <ArrowUpRightIcon className="w-3 h-3" />
                    Open full pricing page
                </Link>
            </Section>

            <Section
                eyebrow="Usage"
                description="What this workspace is consuming this period."
            >
                <UsageRow label="Mailboxes" current={0} max={"Unlimited"} />
                <UsageRow
                    label="Sends / day"
                    current={0}
                    max={
                        currentPlan.sendsPerDay === Number.POSITIVE_INFINITY
                            ? "Custom"
                            : currentPlan.sendsPerDay
                    }
                />
                <UsageRow label="Warmup" current={0} max={"Unlimited"} />
                <UsageRow
                    label="Dedicated IPs"
                    current={currentPlan.id === "business" ? 1 : 0}
                    max={currentPlan.id === "enterprise" ? "Custom" : currentPlan.id === "business" ? 1 : 0}
                />
            </Section>

            <Section
                eyebrow="Payment"
                description="Card used for renewals and add-ons. Managed through Stripe."
            >
                <Row
                    label="Payment method"
                    description="Card is managed through the billing portal."
                >
                    <div className="flex items-center gap-2">
                        <span className="inline-flex items-center gap-1.5 text-[12px] text-slate-500">
                            <CreditCardIcon className="w-3 h-3" />
                            No card on file
                        </span>
                        <button
                            type="button"
                            onClick={openPortal}
                            className="h-7 px-2.5 rounded-md border border-slate-200 hover:border-slate-300 text-[12px] text-slate-700 hover:text-slate-900 transition-colors"
                        >
                            Manage
                        </button>
                    </div>
                </Row>
                <Row
                    label="Billing email"
                    description="Where invoices and renewal notices are sent."
                >
                    <div className="flex items-center gap-2">
                        <span className="text-[12px] text-slate-500 font-mono">Owner's account email</span>
                        <button
                            type="button"
                            onClick={openPortal}
                            className="h-7 px-2.5 rounded-md border border-slate-200 hover:border-slate-300 text-[12px] text-slate-700 hover:text-slate-900 transition-colors"
                        >
                            Change
                        </button>
                    </div>
                </Row>
            </Section>

            <Section
                eyebrow="Invoices"
                description="Receipts from your billing portal."
            >
                <p className="text-[12px] text-slate-500 leading-relaxed">
                    Invoices live in Stripe's billing portal. Open the portal to download
                    PDF receipts.
                </p>
                <button
                    type="button"
                    onClick={openPortal}
                    disabled={portal.isPending}
                    className="inline-flex items-center gap-1.5 h-7 px-2.5 rounded-md border border-slate-200 hover:border-slate-300 text-[12px] text-slate-700 hover:text-slate-900 transition-colors disabled:opacity-60"
                >
                    {portal.isPending ? (
                        <Loader2Icon className="w-3 h-3 animate-spin" />
                    ) : (
                        <FileTextIcon className="w-3 h-3" />
                    )}
                    Open invoices
                </button>
            </Section>
        </SectionShell>
    );
}

function PlanCard({
    id,
    active,
    onUpgrade,
}: {
    id: PlanID;
    active: boolean;
    onUpgrade: () => void;
}) {
    const plan = getPlan(id);
    const accent = PLAN_ACCENT_CLASSES[plan.accent];
    const priceLabel = plan.priceMonthly == null
        ? "Custom"
        : `$${plan.priceMonthly}`;

    return (
        <div
            className={`rounded-md border bg-white p-3 flex flex-col ${
                active ? "border-slate-900 shadow-sm" : "border-slate-200"
            } ${plan.featured && !active ? "ring-1 ring-indigo-200" : ""}`}
        >
            <div className="flex items-center gap-1.5 mb-1">
                <span className={`size-1.5 rounded-full ${accent.dot}`} />
                <span className="text-[11px] uppercase tracking-[0.1em] font-semibold text-slate-700">
                    {plan.label}
                </span>
                {plan.featured && !active && (
                    <span className="ml-auto text-[9px] uppercase tracking-[0.08em] font-semibold text-indigo-700 bg-indigo-50 border border-indigo-100 rounded px-1">
                        Popular
                    </span>
                )}
                {active && (
                    <span className="ml-auto text-[9px] uppercase tracking-[0.08em] font-semibold text-slate-700 bg-slate-100 border border-slate-200 rounded px-1">
                        Current
                    </span>
                )}
            </div>
            <div className="flex items-baseline gap-1 mb-2">
                <span className="text-[18px] font-semibold text-slate-900 tabular-nums">
                    {priceLabel}
                </span>
                {plan.priceMonthly != null && (
                    <span className="text-[10.5px] text-slate-500">/ mo</span>
                )}
            </div>
            <ul className="space-y-1 mb-3 flex-1">
                {plan.bullets.map((b) => (
                    <li key={b} className="flex items-start gap-1.5 text-[11px] text-slate-700 leading-snug">
                        <CheckIcon className="w-3 h-3 text-emerald-600 mt-0.5 shrink-0" />
                        <span>{b}</span>
                    </li>
                ))}
            </ul>
            <button
                type="button"
                onClick={onUpgrade}
                disabled={active}
                className={`h-7 px-2.5 rounded-md text-[11.5px] font-medium transition-colors ${
                    active
                        ? "bg-slate-100 text-slate-400 cursor-default"
                        : "bg-slate-900 hover:bg-slate-800 text-white"
                }`}
            >
                {active ? "Current plan" : id === "enterprise" ? "Contact sales" : "Switch to " + plan.label}
            </button>
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
    if (!status) {
        return (
            <span className="inline-flex items-center text-[10px] rounded px-1.5 h-4 bg-slate-100 text-slate-500 uppercase tracking-[0.1em] font-medium">
                Free
            </span>
        );
    }
    if (status === "trialing") {
        return (
            <span className="inline-flex items-center text-[10px] rounded px-1.5 h-4 bg-emerald-50 text-emerald-700 uppercase tracking-[0.1em] font-medium border border-emerald-100">
                Trialing
            </span>
        );
    }
    if (status === "past_due") {
        return (
            <span className="inline-flex items-center text-[10px] rounded px-1.5 h-4 bg-red-50 text-red-700 uppercase tracking-[0.1em] font-medium border border-red-100">
                Past due
            </span>
        );
    }
    if (status === "canceled") {
        return (
            <span className="inline-flex items-center text-[10px] rounded px-1.5 h-4 bg-slate-100 text-slate-500 uppercase tracking-[0.1em] font-medium">
                Canceled
            </span>
        );
    }
    if (cancelAtEnd) {
        return (
            <span className="inline-flex items-center gap-1 text-[10px] rounded px-1.5 h-4 bg-amber-50 text-amber-700 uppercase tracking-[0.1em] font-medium border border-amber-100">
                <CheckIcon className="w-2 h-2" />
                Ending soon
            </span>
        );
    }
    return (
        <span className="inline-flex items-center gap-1 text-[10px] rounded px-1.5 h-4 bg-emerald-50 text-emerald-700 uppercase tracking-[0.1em] font-medium border border-emerald-100">
            <CheckIcon className="w-2 h-2" />
            Active
        </span>
    );
}

function UsageRow({
    label,
    current,
    max,
}: {
    label: string;
    current: number;
    max: number | string;
}) {
    const pct =
        typeof max === "number" && max > 0
            ? Math.min(100, Math.round((current / max) * 100))
            : 0;
    return (
        <div>
            <div className="flex items-center justify-between text-[11px] mb-1">
                <span className="text-slate-500">{label}</span>
                <span className="font-mono tabular-nums text-slate-700">
                    {current.toLocaleString()}
                    <span className="text-slate-400"> / {typeof max === "number" ? max.toLocaleString() : max}</span>
                </span>
            </div>
            <div className="h-1 rounded-full bg-slate-100 overflow-hidden">
                <div
                    className={`h-full ${
                        pct >= 90 ? "bg-red-500" : pct >= 70 ? "bg-amber-500" : "bg-slate-900"
                    } transition-all`}
                    style={{ width: `${pct}%` }}
                />
            </div>
        </div>
    );
}
