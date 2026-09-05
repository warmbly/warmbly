// Billing — owner-only, organization-scoped, split into tabs so the page is
// browsable: Overview (plan, usage, controls), Plans (compare/promo), AI & credits,
// and Payment. Each tab is a real path (/app/settings/billing/ai-credits,
// /plans, /payment) so tabs are linkable and Stripe returns land on the tab
// they left from.
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
    GaugeIcon,
    LayersIcon,
    Loader2Icon,
    LockIcon,
    SparklesIcon,
    TicketIcon,
    XIcon,
} from "lucide-react";
import { Link, Navigate, useNavigate, useParams } from "react-router-dom";
import { AnimatePresence, motion } from "framer-motion";
import toast from "react-hot-toast";
import { TopbarAction } from "@/components/layout/Page";
import useFeatureAccess from "@/hooks/useFeatureAccess";
import useUpgradeFlow from "@/hooks/useUpgradeFlow";
import { useUpgradeDialog } from "@/hooks/context/upgrade";
import useValidateDiscountCode from "@/lib/api/hooks/app/subscription/useValidateDiscountCode";
import useAppliedDiscounts from "@/lib/api/hooks/app/subscription/useAppliedDiscounts";
import usePreviewPlanChange from "@/lib/api/hooks/app/subscription/usePreviewPlanChange";
import { useAppStore } from "@/stores";
import type { AppError } from "@/lib/api/client/normalizeError";
import type DiscountPreview from "@/lib/api/models/app/subscription/DiscountPreview";
import type { DiscountRedemption } from "@/lib/api/models/app/subscription/DiscountRedemption";
import buildError from "@/lib/helper/buildError";
import { TextInput } from "@/components/ui/field";
import BillingIntervalToggle from "@/components/app/billing/BillingIntervalToggle";
import PlanCard from "@/components/app/billing/PlanCard";
import EnterpriseInquiryDialog from "@/components/app/billing/EnterpriseInquiryDialog";
import { Row, Section, SectionShell, TableSurface } from "../_components/SectionShell";
import { PAID_PLANS, getPlan, planOrder, type PlanID } from "@/lib/plans";
import { describeDiscount, fmtMoney, fromMinorUnits, type BillingInterval } from "@/lib/pricing";
import OverviewTab from "./OverviewTab";
import CreditsCard from "./CreditsCard";
import AIUsageCard from "./AIUsageCard";

type BillingTab = "overview" | "plans" | "ai" | "payment";

// Tab slugs are path segments under /app/settings/billing; overview is the
// bare path.
const TABS: { id: BillingTab; slug: string; label: string; icon: React.ComponentType<{ className?: string }> }[] = [
    { id: "overview", slug: "", label: "Overview", icon: GaugeIcon },
    { id: "plans", slug: "plans", label: "Plans", icon: LayersIcon },
    { id: "ai", slug: "ai-credits", label: "AI & credits", icon: SparklesIcon },
    { id: "payment", slug: "payment", label: "Payment", icon: CreditCardIcon },
];

function tabForSlug(slug: string | undefined): BillingTab | null {
    if (!slug) return "overview";
    const hit = TABS.find((t) => t.slug === slug);
    return hit ? hit.id : null;
}

function pathForTab(t: BillingTab): string {
    const slug = TABS.find((x) => x.id === t)?.slug;
    return slug ? `/app/settings/billing/${slug}` : "/app/settings/billing";
}

export default function BillingSettingsPage() {
    const access = useFeatureAccess();
    const currentOrg = useAppStore((s) => s.currentOrganization);
    const validateCode = useValidateDiscountCode();
    // Checkout / plan change / portal live in useUpgradeFlow, shared with the
    // in-app upgrade dialog.
    const flow = useUpgradeFlow();
    const upgradeDialog = useUpgradeDialog();
    const openPortal = flow.openPortal;
    const redemptions = useAppliedDiscounts();
    const { tab: tabSlug } = useParams();
    const navigate = useNavigate();
    const [codeInput, setCodeInput] = React.useState("");
    const [applied, setApplied] = React.useState<DiscountPreview | null>(null);
    const [billingInterval, setBillingInterval] =
        React.useState<BillingInterval>("annual");
    const [salesOpen, setSalesOpen] = React.useState(false);

    const resolvedTab = tabForSlug(tabSlug);
    const tab: BillingTab = resolvedTab ?? "overview";
    const setTab = (t: BillingTab) => navigate(pathForTab(t));

    // Unknown slug (stale link, typo) — normalize the URL to the overview.
    if (resolvedTab === null) {
        return <Navigate to="/app/settings/billing" replace />;
    }

    // No billing provider on this deployment: there is nothing to manage here.
    if (!access.billing) {
        return <Navigate to="/app/settings/workspace" replace />;
    }

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

    // Spotlight the best-value plan, but never one at or below what the
    // workspace already pays for: ringing Business as "Best value" for an
    // Enterprise customer reads as a downgrade pitch. Undefined means no card
    // is highlighted, which is the right answer on the top plan.
    const recommendedPlan: PlanID | undefined =
        PAID_PLANS.find(
            (id) => getPlan(id).featured && planOrder(id) > planOrder(currentPlan.id),
        ) ?? PAID_PLANS.find((id) => planOrder(id) > planOrder(currentPlan.id));

    // Overview's "Change plan" opens the same full-screen chooser the locked
    // surfaces use, so there is one upgrade experience everywhere.
    function openPlanChooser() {
        upgradeDialog.open({
            feature: "Your plan",
            minPlan: "starter",
            blurb: "Compare every plan, switch billing interval, and change your subscription in one step.",
        });
    }

    // Upgrade/switch to a plan. A valid promo code rides along to Stripe; the
    // flow picks Checkout, an in-place change, or the portal.
    function upgrade(catalogId: PlanID) {
        void flow
            .upgrade(catalogId, {
                interval: billingInterval,
                discountCode: applied?.valid ? applied.code : undefined,
                returnTo: "/app/settings/billing/plans",
            })
            .then((outcome) => {
                if (outcome === "contact") setSalesOpen(true);
            });
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
                    {flow.portalPending ? "Opening…" : "Manage billing"}
                </TopbarAction>
            }
        >
            <div>
                <div className="sticky top-0 z-20 bg-white/95 backdrop-blur px-2 md:px-6 flex items-center gap-1 border-b border-slate-200/70 overflow-x-auto">
                    {TABS.map(({ id, label, icon: Icon }) => {
                        const active = tab === id;
                        return (
                            <button
                                key={id}
                                type="button"
                                onClick={() => setTab(id)}
                                className={`relative h-10 px-2.5 inline-flex shrink-0 items-center gap-1.5 text-[12.5px] transition-colors ${
                                    active
                                        ? "text-slate-900 font-medium"
                                        : "text-slate-500 hover:text-slate-700"
                                }`}
                            >
                                <Icon className="w-3.5 h-3.5" />
                                {label}
                                {active && (
                                    <motion.span
                                        layoutId="billing-tab-underline"
                                        className="absolute left-1.5 right-1.5 -bottom-px h-0.5 rounded-full bg-sky-600"
                                    />
                                )}
                            </button>
                        );
                    })}
                </div>
                <AnimatePresence mode="wait" initial={false}>
                    <motion.div
                        key={tab}
                        initial={{ opacity: 0, y: 6 }}
                        animate={{ opacity: 1, y: 0 }}
                        exit={{ opacity: 0, y: -4 }}
                        transition={{ duration: 0.16, ease: "easeOut" }}
                        className="divide-y divide-slate-200/70"
                    >
                        {tab === "overview" && (
                            <OverviewTab onChangePlan={openPlanChooser} />
                        )}

                        {tab === "plans" && (
                            <>
                                <Section
                                    eyebrow="Compare plans"
                                    description="The same lineup as the public pricing page. Switching is prorated and takes effect immediately."
                                    actions={
                                        <BillingIntervalToggle
                                            interval={billingInterval}
                                            onChange={setBillingInterval}
                                        />
                                    }
                                >
                                    <div className="grid grid-cols-1 sm:grid-cols-2 xl:grid-cols-4 gap-4 pt-2">
                                        {PAID_PLANS.map((id, i) => (
                                            <PlanCard
                                                key={id}
                                                id={id}
                                                index={i}
                                                current={currentPlan.id}
                                                recommended={id === recommendedPlan}
                                                discount={applied}
                                                interval={billingInterval}
                                                pending={flow.pending === id}
                                                busy={flow.pending !== null || flow.portalPending}
                                                ctaVerb={currentPlan.id === "free" ? "Get" : "Switch to"}
                                                footer={
                                                    <ProrationNote
                                                        planId={flow.resolveServerPlan(id)?.id}
                                                        enabled={currentPlan.id !== "free" && currentPlan.id !== id}
                                                    />
                                                }
                                                onChoose={() => upgrade(id)}
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
                                    eyebrow="Redeemed codes"
                                    description="Promo and referral codes this workspace has redeemed."
                                >
                                    {redemptions.isPending ? (
                                        <div className="h-16 rounded bg-slate-100 animate-pulse" />
                                    ) : (redemptions.data?.data.length ?? 0) === 0 ? (
                                        <p className="text-[12px] text-slate-500 leading-relaxed">
                                            No codes redeemed yet. Apply a code above to see it here.
                                        </p>
                                    ) : (
                                        <TableSurface>
                                            <table className="w-full text-[12px]">
                                                <thead>
                                                    <tr className="text-left text-[10.5px] uppercase tracking-[0.08em] text-slate-400 border-b border-slate-200">
                                                        <th className="font-medium px-3 py-2">Code</th>
                                                        <th className="font-medium px-3 py-2">Discount</th>
                                                        <th className="font-medium px-3 py-2">Status</th>
                                                        <th className="font-medium px-3 py-2 text-right">Redeemed</th>
                                                    </tr>
                                                </thead>
                                                <tbody className="divide-y divide-slate-100">
                                                    {(redemptions.data?.data ?? []).map((d) => (
                                                        <RedemptionRow key={d.id} row={d} />
                                                    ))}
                                                </tbody>
                                            </table>
                                        </TableSurface>
                                    )}
                                </Section>
                            </>
                        )}

                        {tab === "ai" && (
                            <>
                                <CreditsCard isPaid={currentPlan.id !== "free"} />
                                <AIUsageCard />
                            </>
                        )}

                        {tab === "payment" && (
                            <>
                                <Section
                                    eyebrow="Payment"
                                    description="Card used for renewals and add-ons. Managed through Stripe."
                                >
                                    <Row
                                        label="Payment method"
                                        description="Cards are held by Stripe and never touch Warmbly, so they are read and changed in the portal."
                                    >
                                        <button
                                            type="button"
                                            onClick={openPortal}
                                            disabled={flow.portalPending}
                                            className="h-7 px-2.5 rounded-md border border-slate-200 hover:border-slate-300 text-[12px] text-slate-700 hover:text-slate-900 transition-colors inline-flex items-center gap-1.5 disabled:opacity-60"
                                        >
                                            <CreditCardIcon className="w-3 h-3" />
                                            Manage cards in Stripe
                                        </button>
                                    </Row>
                                    <Row
                                        label="Billing email"
                                        description="Invoices and renewal notices go to the billing email on the Stripe customer."
                                    >
                                        <button
                                            type="button"
                                            onClick={openPortal}
                                            disabled={flow.portalPending}
                                            className="h-7 px-2.5 rounded-md border border-slate-200 hover:border-slate-300 text-[12px] text-slate-700 hover:text-slate-900 transition-colors disabled:opacity-60"
                                        >
                                            Change in Stripe
                                        </button>
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
                                        disabled={flow.portalPending}
                                        className="inline-flex items-center gap-1.5 h-7 px-2.5 rounded-md border border-slate-200 hover:border-slate-300 text-[12px] text-slate-700 hover:text-slate-900 transition-colors disabled:opacity-60"
                                    >
                                        {flow.portalPending ? (
                                            <Loader2Icon className="w-3 h-3 animate-spin" />
                                        ) : (
                                            <FileTextIcon className="w-3 h-3" />
                                        )}
                                        Open invoices
                                    </button>
                                </Section>
                            </>
                        )}
                    </motion.div>
                </AnimatePresence>
            </div>
            <EnterpriseInquiryDialog open={salesOpen} onClose={() => setSalesOpen(false)} />
        </SectionShell>
    );
}

function RedemptionRow({ row }: { row: DiscountRedemption }) {
    return (
        <tr className="text-slate-700">
            <td className="px-3 py-2 font-mono uppercase text-slate-900">{row.code}</td>
            <td className="px-3 py-2 text-slate-600">{describeRedemption(row)}</td>
            <td className="px-3 py-2">
                <RedemptionStatusPill status={row.status} />
            </td>
            <td className="px-3 py-2 text-right text-slate-500 tabular-nums">
                {fmtDate(row.redeemed_at)}
            </td>
        </tr>
    );
}

function RedemptionStatusPill({ status }: { status: string }) {
    const s = (status ?? "").toLowerCase();
    const cls =
        s === "applied" || s === "active"
            ? "bg-emerald-50 text-emerald-700 border-emerald-100"
            : s === "expired" || s === "revoked" || s === "void"
                ? "bg-slate-100 text-slate-400 border-slate-200"
                : "bg-slate-100 text-slate-500 border-slate-200";
    return (
        <span
            className={`inline-flex items-center text-[10px] uppercase tracking-[0.08em] font-semibold rounded-sm px-1.5 py-0.5 border ${cls}`}
        >
            {status || "—"}
        </span>
    );
}

// describeRedemption renders a short human summary of a redeemed code.
function describeRedemption(d: DiscountRedemption): string {
    if (d.type === "trial_extension") {
        return `+${d.trial_extension_days ?? 0} trial days`;
    }
    if (d.type === "percent") {
        return `${d.percent_off ?? 0}% off`;
    }
    if (d.type === "fixed" && d.amount_off != null) {
        return `${(d.currency ?? "usd").toUpperCase()} ${fmtMoney(d.amount_off)} off`;
    }
    return d.type || "Discount";
}

function fmtDate(value?: string | null): string {
    if (!value) return "—";
    const d = new Date(value);
    if (Number.isNaN(d.getTime())) return "—";
    return d.toLocaleDateString("en-US", {
        month: "short",
        day: "numeric",
        year: "numeric",
    });
}

// What a switch costs today, shown on each plan card for a paying workspace.
// An empty id disables the query, so free workspaces and the current plan never
// hit /subscription/preview-change.
function ProrationNote({ planId, enabled }: { planId?: string; enabled: boolean }) {
    const preview = usePreviewPlanChange(enabled && planId ? planId : "");
    if (!enabled) return null;
    return (
        <div className="rounded-md border border-slate-200/80 bg-slate-50 px-2.5 py-1.5 text-[11px] leading-snug">
            {preview.isPending ? (
                <span className="text-slate-400">Pricing this switch…</span>
            ) : preview.data ? (
                <>
                    <div className="text-slate-700 tabular-nums font-medium">
                        {(() => {
                            // Stripe reports this in the currency's minor unit.
                            const due = fromMinorUnits(
                                preview.data.proration_amount,
                                preview.data.currency,
                            );
                            if (due > 0) return `Due today $${fmtMoney(due)}`;
                            if (due < 0) return `Credit $${fmtMoney(Math.abs(due))}`;
                            return "No charge today";
                        })()}
                    </div>
                    <div className="text-slate-400">
                        Next bill {fmtDate(preview.data.next_billing_date as unknown as string)}
                    </div>
                </>
            ) : (
                <span className="text-slate-400">Prorated at switch</span>
            )}
        </div>
    );
}
