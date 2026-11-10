import { AgencyPage } from "@/components/app/agency/AgencyPage";
import React from "react";
import { CheckIcon, ExternalLinkIcon, Loader2Icon, PlusIcon, SparklesIcon } from "lucide-react";
import toast from "react-hot-toast/headless";
import { EmptyBlock, PageBody, SectionBar } from "@/components/layout/Page";
import { NumberInput } from "@/components/ui/field";
import {
    useAgencyAIBalance,
    useAgencyAITopup,
    useAgencyCheckout,
    useAgencyPlans,
    useAgencyPortal,
    useAgencyTopup,
    useAgencyUsage,
    useCurrentAgency,
} from "@/lib/api/hooks/app/agency/useAgency";
import type { AgencyAIBalance, AgencyPlan } from "@/lib/api/models/app/agency/Agency";
import type { AppError } from "@/lib/api/client/normalizeError";
import buildError from "@/lib/helper/buildError";
import { cn } from "@/lib/utils";
import { formatNumber } from "@/components/app/agency/styles";
import { fmtUsd, fmtUsdTrim, roundCents } from "@/lib/money";

const STATUS_LABEL: Record<string, string> = {
    active: "Active",
    trialing: "Trial",
    past_due: "Payment overdue",
    canceled: "Canceled",
    unpaid: "Unpaid",
    incomplete: "No plan yet",
    incomplete_expired: "Checkout expired",
    paused: "Paused",
};

const FEATURES: Record<string, string[]> = {
    Agency: [
        "Unlimited clients, no per-client fee",
        "The API and webhooks in every client workspace",
        "Your logo, colour and portal domain on client pages",
        "Shared send pool and AI balance across clients",
    ],
    "Agency Pro": [
        "Everything in Agency",
        "No \"Powered by Warmbly\" on client pages",
        "Sending kept apart from other customers",
    ],
};

function money(n: number) {
    return `$${Math.round(n).toLocaleString()}`;
}

function BuyAIBalance({ agencyId, balance, canManage }: { agencyId: string; balance: AgencyAIBalance; canManage: boolean }) {
    const topup = useAgencyAITopup(agencyId);
    const presets = balance.top_up.presets_usd?.length ? balance.top_up.presets_usd : [10, 25, 50, 100, 250];
    const [amount, setAmount] = React.useState(balance.top_up.default_usd || 25);
    const usd = roundCents(amount);
    const valid = usd >= balance.top_up.min_usd && usd <= balance.top_up.max_usd;

    const buy = async () => {
        if (!valid) return;
        try {
            const res = await topup.mutateAsync(usd);
            if (res.checkout_url) window.location.assign(res.checkout_url);
        } catch (e) {
            toast.error(buildError(e as AppError));
        }
    };

    return (
        <div className="px-5 py-4 border-b border-slate-200 flex flex-wrap items-center gap-3">
            <div className="flex-1 min-w-[220px]">
                <div className="text-[12.5px] text-slate-900">{fmtUsd(balance.balance_usd)} left in the shared AI balance</div>
                <div className="text-[11.5px] text-slate-500">
                    Dollar for dollar. Bought balance never expires and is spent after this period's {fmtUsd(balance.allowance_usd)}{" "}
                    included with the plan.
                </div>
            </div>
            {canManage && (
                <div className="flex flex-wrap items-center gap-1.5">
                    {presets.map((v) => (
                        <button
                            key={v}
                            type="button"
                            aria-pressed={usd === v}
                            onClick={() => setAmount(v)}
                            className={cn(
                                "h-7 px-2.5 rounded-md border text-[12px] tabular-nums transition-colors",
                                usd === v
                                    ? "border-sky-400 bg-sky-50 text-sky-700 font-medium"
                                    : "border-slate-200 bg-white text-slate-600 hover:border-slate-300 hover:text-slate-900",
                            )}
                        >
                            {fmtUsdTrim(v)}
                        </button>
                    ))}
                    <NumberInput
                        value={amount}
                        onChange={setAmount}
                        min={0}
                        max={balance.top_up.max_usd}
                        step={5}
                        suffix="USD"
                        className="w-28"
                    />
                    <button
                        type="button"
                        onClick={() => void buy()}
                        disabled={!valid || topup.isPending}
                        className="h-7 px-2.5 rounded-md bg-sky-600 hover:bg-sky-700 text-white text-[12px] font-medium inline-flex items-center gap-1.5 disabled:opacity-60"
                    >
                        {topup.isPending ? <Loader2Icon className="w-3 h-3 animate-spin" /> : <SparklesIcon className="w-3 h-3" />}
                        Buy AI balance
                    </button>
                </div>
            )}
            {canManage && !valid && (
                <p className="basis-full text-[11.5px] text-red-600">
                    Enter an amount from {fmtUsdTrim(balance.top_up.min_usd)} to {fmtUsdTrim(balance.top_up.max_usd)}.
                </p>
            )}
        </div>
    );
}

function AgencyBilling() {
    const { agency, billingEnabled } = useCurrentAgency();
    const agencyId = agency?.id ?? "";
    const plans = useAgencyPlans();
    const usage = useAgencyUsage(agency?.id);
    const ai = useAgencyAIBalance(agency?.id);
    const checkout = useAgencyCheckout(agencyId);
    const topup = useAgencyTopup(agencyId);
    const portal = useAgencyPortal(agencyId);
    const [yearly, setYearly] = React.useState(true);
    const canManage = agency?.role === "owner" || agency?.role === "admin";

    if (!agency) return null;

    if (!billingEnabled) {
        return (
            <PageBody>
                <EmptyBlock
                    title="No billing on this instance"
                    body="This self-hosted instance does not bill agencies. Every agency feature is available and the send pool has no cap."
                />
            </PageBody>
        );
    }

    const go = async (p: Promise<{ checkout_url?: string; url?: string }>) => {
        try {
            const res = await p;
            const url = res.checkout_url ?? res.url;
            if (url) window.location.assign(url);
        } catch (e) {
            toast.error(buildError(e as AppError));
        }
    };

    // A plan arranged directly is in force whatever the subscription says.
    const granted = agency.granted;
    const active = !!granted || agency.status === "active" || agency.status === "trialing" || agency.status === "past_due";
    const current = agency.plan;
    const list = plans.data?.data ?? [];
    const u = usage.data;

    const priceOf = (p: AgencyPlan) => (yearly && p.price_yearly ? p.price_yearly / 12 : p.price);
    const buyable = (p: AgencyPlan) => (yearly ? !!p.stripe_price_id_yearly : !!p.stripe_price_id);

    return (
        <PageBody>
            <SectionBar label="Current plan">
                {agency.has_billing && canManage && (
                    <button
                        type="button"
                        onClick={() => void go(portal.mutateAsync())}
                        disabled={portal.isPending}
                        className="h-7 px-2.5 rounded-md border border-slate-200 hover:border-slate-300 text-[12px] text-slate-700 bg-white inline-flex items-center gap-1.5"
                    >
                        {portal.isPending ? <Loader2Icon className="w-3 h-3 animate-spin" /> : <ExternalLinkIcon className="w-3 h-3" />}
                        Invoices and payment
                    </button>
                )}
            </SectionBar>
            <div className="px-5 py-4 border-b border-slate-200 flex flex-wrap items-baseline gap-x-4 gap-y-1">
                <span className="text-[16px] font-medium text-slate-900">{active && current?.name ? current.name : "No plan"}</span>
                {granted ? (
                    <>
                        <span className="text-[12px] text-slate-500">Active</span>
                        <span className="text-[12px] text-slate-500">
                            Arranged directly, not billed here
                            {granted.until ? `. Ends ${new Date(granted.until).toLocaleDateString()}` : ""}
                        </span>
                    </>
                ) : (
                    <>
                        <span className={cn("text-[12px]", agency.status === "past_due" ? "text-amber-700" : "text-slate-500")}>
                            {STATUS_LABEL[agency.status] ?? agency.status}
                        </span>
                        {active && agency.current_period_end && (
                            <span className="text-[12px] text-slate-500">
                                {agency.cancel_at_period_end ? "Ends" : "Renews"} {new Date(agency.current_period_end).toLocaleDateString()}
                            </span>
                        )}
                    </>
                )}
                {!active && (
                    <p className="basis-full text-[12px] text-slate-500 mt-1">
                        Client workspaces get the agency's plan once you choose one. Until then each workspace stays on whatever
                        it had.
                    </p>
                )}
            </div>

            {active && u && !u.unlimited && (
                <>
                    <SectionBar label="Top-up packs" />
                    <div className="px-5 py-4 border-b border-slate-200 flex flex-wrap items-center gap-3">
                        <div className="flex-1 min-w-[220px]">
                            <div className="text-[12.5px] text-slate-900">
                                {formatNumber(u.topup_sends)} more sends for {money(u.topup_price_cents / 100)}
                            </div>
                            <div className="text-[11.5px] text-slate-500">
                                One-off, added to this period's pool and gone when the period renews.
                                {u.topups > 0 && ` ${formatNumber(u.topups)} added this period.`}
                            </div>
                        </div>
                        {canManage && (
                            <button
                                type="button"
                                onClick={() => void go(topup.mutateAsync())}
                                disabled={topup.isPending}
                                className="h-7 px-2.5 rounded-md bg-sky-600 hover:bg-sky-700 text-white text-[12px] font-medium inline-flex items-center gap-1.5 disabled:opacity-60"
                            >
                                {topup.isPending ? <Loader2Icon className="w-3 h-3 animate-spin" /> : <PlusIcon className="w-3 h-3" />}
                                Buy a pack
                            </button>
                        )}
                    </div>
                </>
            )}

            {active && ai.data && !ai.data.unmetered && (
                <>
                    <SectionBar label="AI balance" />
                    <BuyAIBalance agencyId={agencyId} balance={ai.data} canManage={canManage} />
                </>
            )}

            {!active && (
                <>
                    <SectionBar label="Plans">
                        <div className="inline-flex rounded-md border border-slate-200 p-0.5 bg-slate-50">
                            {[
                                { v: false, l: "Monthly" },
                                { v: true, l: "Annual" },
                            ].map((o) => (
                                <button
                                    key={o.l}
                                    type="button"
                                    onClick={() => setYearly(o.v)}
                                    className={cn(
                                        "h-6 px-2.5 rounded text-[12px]",
                                        yearly === o.v ? "bg-white text-slate-900 shadow-sm font-medium" : "text-slate-500",
                                    )}
                                >
                                    {o.l}
                                </button>
                            ))}
                        </div>
                    </SectionBar>
                    <div className="grid md:grid-cols-3 gap-3 p-5">
                        {list.map((p) => (
                            <div key={p.id} className="rounded-lg border border-slate-200 p-4 flex flex-col">
                                <div className="text-[12.5px] font-medium text-slate-900">{p.name}</div>
                                <div className="mt-1 flex items-baseline gap-1">
                                    <span className="text-[24px] font-light text-slate-900 tabular-nums">{money(priceOf(p))}</span>
                                    <span className="text-[11.5px] text-slate-500">/ month{yearly ? ", billed yearly" : ""}</span>
                                </div>
                                <div className="mt-1 text-[11.5px] text-slate-600">
                                    {formatNumber(p.monthly_send_pool ?? 0)} shared sends a month
                                </div>
                                {!!p.agency_ai_allowance_usd && (
                                    <div className="text-[11.5px] text-slate-600">{fmtUsdTrim(p.agency_ai_allowance_usd)} of shared AI each month</div>
                                )}
                                <ul className="mt-3 space-y-1 flex-1">
                                    {(FEATURES[p.name ?? ""] ?? []).map((f) => (
                                        <li key={f} className="flex gap-1.5 text-[11.5px] text-slate-600">
                                            <CheckIcon className="w-3 h-3 mt-0.5 text-sky-600 shrink-0" />
                                            {f}
                                        </li>
                                    ))}
                                </ul>
                                {canManage && (
                                    <button
                                        type="button"
                                        disabled={!buyable(p) || checkout.isPending}
                                        title={buyable(p) ? undefined : "Not available for purchase yet"}
                                        onClick={() => void go(checkout.mutateAsync({ planId: p.id, interval: yearly ? "year" : "month" }))}
                                        className="mt-4 h-7 px-2.5 rounded-md bg-slate-900 hover:bg-slate-800 text-white text-[12px] font-medium disabled:opacity-50"
                                    >
                                        {buyable(p) ? `Choose ${p.name}` : "Not available yet"}
                                    </button>
                                )}
                            </div>
                        ))}
                        <div className="rounded-lg border border-dashed border-slate-300 p-4 flex flex-col">
                            <div className="text-[12.5px] font-medium text-slate-900">Agency Scale</div>
                            <div className="mt-1 text-[24px] font-light text-slate-900">Custom</div>
                            <div className="mt-1 text-[11.5px] text-slate-600">1,000,000+ shared sends a month</div>
                            <p className="mt-3 flex-1 text-[11.5px] text-slate-600">Everything in Agency Pro, sized to your volume.</p>
                            <a
                                href="https://warmbly.com/contact"
                                target="_blank"
                                rel="noreferrer"
                                className="mt-4 h-7 px-2.5 rounded-md border border-slate-200 text-[12px] text-slate-700 inline-flex items-center justify-center hover:border-slate-300"
                            >
                                Talk to us
                            </a>
                        </div>
                    </div>
                </>
            )}
        </PageBody>
    );
}

export default function AgencyBillingPage() {
    return (
        <AgencyPage title="Billing">
            <AgencyBilling />
        </AgencyPage>
    );
}
