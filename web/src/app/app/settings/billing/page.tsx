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
    TicketIcon,
    XIcon,
} from "lucide-react";
import { Link } from "react-router-dom";
import toast from "react-hot-toast";
import { TopbarAction } from "@/components/layout/Page";
import useFeatureAccess from "@/hooks/useFeatureAccess";
import useSubscription from "@/lib/api/hooks/app/subscription/useSubscription";
import useCreatePortalSession from "@/lib/api/hooks/app/subscription/useCreatePortalSession";
import useValidateDiscountCode from "@/lib/api/hooks/app/subscription/useValidateDiscountCode";
import useCreateCheckoutSession from "@/lib/api/hooks/app/subscription/useCreateCheckoutSession";
import useChangePlan from "@/lib/api/hooks/app/subscription/useChangePlan";
import usePlans from "@/lib/api/hooks/app/subscription/usePlans";
import useUsageOverview from "@/lib/api/hooks/app/analytics/useUsageOverview";
import { useAppStore } from "@/stores";
import type { AppError } from "@/lib/api/client/normalizeError";
import type DiscountPreview from "@/lib/api/models/app/subscription/DiscountPreview";
import type ServerPlan from "@/lib/api/models/app/subscription/Plan";
import buildError from "@/lib/helper/buildError";
import { TextInput } from "@/components/ui/field";
import { Row, Section, SectionShell } from "../_components/SectionShell";
import { PLAN_ACCENT_CLASSES, PAID_PLANS, getPlan, type PlanID } from "@/lib/plans";

type BillingInterval = "monthly" | "annual";

export default function BillingSettingsPage() {
    const access = useFeatureAccess();
    const sub = useSubscription();
    const portal = useCreatePortalSession();
    const currentOrg = useAppStore((s) => s.currentOrganization);
    const validateCode = useValidateDiscountCode();
    const checkout = useCreateCheckoutSession();
    const changePlan = useChangePlan();
    const plansQuery = usePlans();
    const usage = useUsageOverview().data;
    const [codeInput, setCodeInput] = React.useState("");
    const [applied, setApplied] = React.useState<DiscountPreview | null>(null);
    const [billingInterval, setBillingInterval] =
        React.useState<BillingInterval>("annual");

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

    async function applyCode() {
        const code = codeInput.trim();
        if (!code) return;
        try {
            const res = await validateCode.mutateAsync({ code });
            if (res.valid) {
                setApplied(res);
                toast.success("Promo code applied");
            } else {
                setApplied(null);
                toast.error(res.reason || "That code can't be applied");
            }
        } catch (e) {
            toast.error(buildError(e as AppError));
        }
    }

    function clearCode() {
        setApplied(null);
        setCodeInput("");
    }

    // Resolve a marketing-catalog plan (e.g. "grow") to the server Plan record
    // so we can read its Stripe price ID / UUID. Matches by name; returns
    // undefined when the server has no matching public plan configured.
    function resolveServerPlan(catalogId: PlanID): ServerPlan | undefined {
        const label = getPlan(catalogId).label.toLowerCase().trim();
        const plans = (plansQuery.data ?? []) as ServerPlan[];
        return plans.find((p) => {
            const n = (p.name ?? "").toLowerCase().trim();
            return n === label || n.startsWith(label);
        });
    }

    // Upgrade/switch to a plan. When a valid promo code is applied it rides
    // along to Stripe: new subscriptions go through in-app Checkout, existing
    // paid subscriptions change plan directly. Falls back to the Stripe portal
    // when the target plan can't be resolved to a configured Stripe price.
    async function upgrade(catalogId: PlanID) {
        if (catalogId === "enterprise") {
            openPortal();
            return;
        }
        const code = applied?.valid ? applied.code : undefined;
        const target = resolveServerPlan(catalogId);
        const onPaid = currentPlan.id !== "free";
        const annual = billingInterval === "annual";
        const priceId = annual
            ? target?.stripe_price_id_yearly
            : target?.stripe_price_id;

        if (!target || (!onPaid && !priceId)) {
            openPortal();
            return;
        }

        try {
            if (onPaid) {
                await toast.promise(
                    changePlan.mutateAsync({
                        plan_id: target.id,
                        discount_code: code,
                        interval: annual ? "year" : "month",
                    }),
                    {
                        loading: "Updating your plan…",
                        success: "Plan updated",
                        error: (e: AppError) => buildError(e),
                    },
                );
            } else {
                const base = `${window.location.origin}/app/settings/billing`;
                const { checkout_url } = await toast.promise(
                    checkout.mutateAsync({
                        price_id: priceId as string,
                        success_url: `${base}?checkout=success`,
                        cancel_url: `${base}?checkout=cancel`,
                        discount_code: code,
                    }),
                    {
                        loading: "Starting checkout…",
                        success: "Redirecting to checkout…",
                        error: (e: AppError) => buildError(e),
                    },
                );
                window.location.assign(checkout_url);
            }
        } catch {
            /* surfaced via toast */
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
                    <div className="flex flex-wrap items-start gap-4">
                        <div className={`size-9 rounded-md flex items-center justify-center shrink-0 border ${currentAccent.pill}`}>
                            <SparklesIcon className="w-4 h-4" />
                        </div>
                        <div className="min-w-0 flex-1 basis-[200px]">
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
                <ul className="grid grid-cols-1 sm:grid-cols-2 gap-x-6 gap-y-1.5 mt-1 text-[11.5px] max-w-md">
                    {currentPlan.bullets.map((b) => (
                        <li key={b} className="flex items-start gap-1.5">
                            <CheckIcon className="w-3 h-3 text-emerald-600 mt-0.5 shrink-0" />
                            <span className="text-slate-700">{b}</span>
                        </li>
                    ))}
                </ul>
            </Section>

            <Section
                eyebrow="Promo code"
                description="Have a discount code? Apply it to preview your price. It's applied at checkout."
            >
                <Row
                    label="Discount code"
                    description="We validate the code against your workspace and the plan you pick."
                    align="start"
                >
                    <div className="flex flex-col items-stretch gap-2 sm:items-end">
                        <div className="flex items-center gap-2">
                            <TextInput
                                value={codeInput}
                                onChange={(v) => setCodeInput(v.toUpperCase())}
                                placeholder="WELCOME10"
                                disabled={!!applied}
                                onKeyDown={(e) => {
                                    if (e.key === "Enter") applyCode();
                                }}
                                className="w-full sm:w-[180px] font-mono uppercase"
                            />
                            {applied ? (
                                <button
                                    type="button"
                                    onClick={clearCode}
                                    className="h-7 px-2.5 rounded-md border border-slate-200 hover:border-slate-300 text-[12px] text-slate-700 hover:text-slate-900 transition-colors inline-flex items-center gap-1 shrink-0"
                                >
                                    <XIcon className="w-3 h-3" />
                                    Clear
                                </button>
                            ) : (
                                <button
                                    type="button"
                                    onClick={applyCode}
                                    disabled={validateCode.isPending || !codeInput.trim()}
                                    className="h-7 px-3 rounded-md bg-slate-900 hover:bg-slate-800 text-white text-[12px] font-medium inline-flex items-center gap-1.5 transition-colors disabled:opacity-50 shrink-0"
                                >
                                    {validateCode.isPending ? (
                                        <Loader2Icon className="w-3 h-3 animate-spin" />
                                    ) : (
                                        <TicketIcon className="w-3 h-3" />
                                    )}
                                    Apply
                                </button>
                            )}
                        </div>
                        {applied && (
                            <div className="text-[11.5px] text-emerald-700 bg-emerald-50 border border-emerald-100 rounded-md px-2 py-1 inline-flex items-center gap-1.5">
                                <CheckIcon className="w-3 h-3 shrink-0" />
                                <span className="font-mono font-medium">{applied.code}</span>
                                <span>· {describeDiscount(applied)}</span>
                            </div>
                        )}
                    </div>
                </Row>
            </Section>

            <Section
                eyebrow="Compare plans"
                description="Same lineup as the public pricing page."
            >
                <div className="flex items-center justify-between gap-3 flex-wrap">
                    <span className="text-[11.5px] text-slate-500">
                        {billingInterval === "annual"
                            ? "Annual billing — save 20%."
                            : "Monthly billing."}
                    </span>
                    <BillingIntervalToggle
                        interval={billingInterval}
                        onChange={setBillingInterval}
                    />
                </div>
                <div className="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-4 gap-2">
                    {PAID_PLANS.map((id) => (
                        <PlanCard
                            key={id}
                            id={id}
                            active={currentPlan.id === id}
                            discount={applied}
                            interval={billingInterval}
                            onUpgrade={() => upgrade(id)}
                        />
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
                <UsageRow label="Mailboxes" current={usage?.email_accounts.total ?? 0} max={"Unlimited"} />
                <UsageRow
                    label="Sends this period"
                    current={usage?.campaigns.emails_sent ?? 0}
                    max={
                        currentPlan.sendsPerDay === Number.POSITIVE_INFINITY
                            ? "Custom"
                            : currentPlan.sendsPerDay
                    }
                />
                <UsageRow label="Warmup" current={usage?.email_accounts.in_warmup ?? 0} max={"Unlimited"} />
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

function BillingIntervalToggle({
    interval,
    onChange,
}: {
    interval: BillingInterval;
    onChange: (i: BillingInterval) => void;
}) {
    return (
        <div className="inline-flex items-center rounded-md border border-slate-200 bg-slate-50 p-0.5 text-[12px]">
            {(["monthly", "annual"] as BillingInterval[]).map((opt) => {
                const active = interval === opt;
                return (
                    <button
                        key={opt}
                        type="button"
                        onClick={() => onChange(opt)}
                        className={`h-6 px-2.5 rounded inline-flex items-center gap-1 font-medium transition-colors ${
                            active
                                ? "bg-white text-slate-900 shadow-sm"
                                : "text-slate-500 hover:text-slate-700"
                        }`}
                    >
                        {opt === "monthly" ? "Monthly" : "Annual"}
                        {opt === "annual" && (
                            <span
                                className={`text-[10px] font-semibold ${
                                    active ? "text-emerald-600" : "text-emerald-500"
                                }`}
                            >
                                −20%
                            </span>
                        )}
                    </button>
                );
            })}
        </div>
    );
}

function PlanCard({
    id,
    active,
    discount,
    interval,
    onUpgrade,
}: {
    id: PlanID;
    active: boolean;
    discount?: DiscountPreview | null;
    interval: BillingInterval;
    onUpgrade: () => void;
}) {
    const plan = getPlan(id);
    const accent = PLAN_ACCENT_CLASSES[plan.accent];
    const annual = interval === "annual";
    const base = annual ? plan.priceAnnual : plan.priceMonthly;
    const disc = discountedPrice(base, discount);

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
            <div className="flex items-baseline gap-1 mb-0.5">
                {base == null ? (
                    <span className="text-[18px] font-semibold text-slate-900 tabular-nums">
                        Custom
                    </span>
                ) : disc != null ? (
                    <>
                        <span className="text-[18px] font-semibold text-emerald-700 tabular-nums">
                            ${fmtMoney(disc)}
                        </span>
                        <span className="text-[11px] text-slate-400 line-through tabular-nums">
                            ${fmtMoney(base)}
                        </span>
                        <span className="text-[10.5px] text-slate-500">/ mo</span>
                    </>
                ) : (
                    <>
                        <span className="text-[18px] font-semibold text-slate-900 tabular-nums">
                            ${fmtMoney(base)}
                        </span>
                        <span className="text-[10.5px] text-slate-500">/ mo</span>
                    </>
                )}
            </div>
            <div className="text-[10px] text-slate-400 mb-2 h-3">
                {base == null
                    ? "contact sales"
                    : annual
                      ? "billed annually · 20% off"
                      : "billed monthly"}
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

// describeDiscount renders a short human summary of an applied code.
function describeDiscount(d: DiscountPreview): string {
    if (d.type === "trial_extension") {
        return `+${d.trial_extension_days ?? 0} trial days`;
    }
    let base: string;
    if (d.type === "percent") {
        base = `${d.percent_off ?? 0}% off`;
    } else {
        base = `${(d.currency ?? "usd").toUpperCase()} ${fmtMoney(d.amount_off ?? 0)} off`;
    }
    if (d.duration === "forever") return `${base}, forever`;
    if (d.duration === "repeating" && d.duration_in_months) {
        return `${base} for ${d.duration_in_months} months`;
    }
    return `${base} on your first invoice`;
}

// discountedPrice returns the discounted price for a money discount, or null
// when the discount doesn't change the price (trial extensions, custom plans,
// or no applied code). Works for either the monthly or annual base price.
function discountedPrice(
    price: number | null,
    d: DiscountPreview | null | undefined,
): number | null {
    if (price == null || !d || !d.valid) return null;
    if (d.type === "percent" && d.percent_off != null) {
        return roundMoney(Math.max(0, price * (1 - d.percent_off / 100)));
    }
    if (d.type === "fixed" && d.amount_off != null) {
        return roundMoney(Math.max(0, price - d.amount_off));
    }
    return null;
}

function roundMoney(n: number): number {
    return Math.round(n * 100) / 100;
}

function fmtMoney(n: number): string {
    return Number.isInteger(n) ? String(n) : n.toFixed(2);
}
