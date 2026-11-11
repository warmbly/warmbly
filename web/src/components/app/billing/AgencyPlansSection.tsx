// The agency plans, shown on a workspace's Plans tab. An agency plan belongs to
// an agency rather than to one workspace, so each card leads to the agency
// instead of checking out here.
import { Link } from "@tanstack/react-router";
import { BuildingIcon, CheckIcon } from "lucide-react";
import { useAgencies, useAgencyPlans } from "@/lib/api/hooks/app/agency/useAgency";
import type { AgencyPlan } from "@/lib/api/models/app/agency/Agency";
import { AGENCY_PLAN_FEATURES } from "@/components/app/agency/planFeatures";
import { formatNumber } from "@/components/app/agency/styles";
import { fmtUsdTrim } from "@/lib/money";
import type { BillingInterval } from "@/lib/pricing";
import { Section } from "@/app/app/settings/_components/SectionShell";

function monthly(p: AgencyPlan, interval: BillingInterval): number {
    return interval === "annual" && p.price_yearly ? p.price_yearly / 12 : p.price;
}

export default function AgencyPlansSection({ interval }: { interval: BillingInterval }) {
    const agencies = useAgencies();
    const plans = useAgencyPlans();
    // Until the list is known the link stays neutral: an agency that did not load is not a missing one.
    const hasAgency = agencies.isSuccess ? agencies.data.data.length > 0 : undefined;
    const to = hasAgency ? "/app/agency/billing" : "/app/agency";
    // An agency that already holds a plan, or a member who cannot buy one, still lands on a page that fits this label.
    const cta = hasAgency === undefined ? "Agency plans" : hasAgency ? "Open agency billing" : "Set up an agency";
    const list = plans.data?.data ?? [];

    return (
        <Section
            eyebrow="Agency plans"
            description="For outreach on behalf of clients: one bill, one shared pool of sends and no fee per client. An agency plan is held by an agency and covers every client workspace in it, so it is chosen there."
        >
            {list.length > 0 && (
                <div className="grid grid-cols-1 sm:grid-cols-2 xl:grid-cols-4 gap-4 pt-2">
                    {list.map((p) => (
                        // Laid out like PlanCard, so the agency plans read as part of the same lineup.
                        <div key={p.id} className="rounded-xl border border-slate-200 bg-white p-5 flex flex-col">
                            <div className="text-[14px] font-semibold text-slate-900">{p.name}</div>
                            <div className="mt-4 flex items-baseline gap-1.5">
                                <span className="text-[32px] font-semibold tracking-[-0.03em] tabular-nums leading-none text-slate-900">
                                    ${Math.round(monthly(p, interval)).toLocaleString()}
                                </span>
                                <span className="text-[12.5px] text-slate-500">/ month</span>
                            </div>
                            <div className="mt-1.5 text-[12px] text-slate-500">
                                {interval === "annual" && p.price_yearly ? "Billed yearly" : "Billed monthly"}
                            </div>
                            <div className="mt-4 pt-4 border-t border-slate-100 flex-1">
                                <div className="text-[12.5px] font-medium text-slate-900">
                                    {p.monthly_send_pool
                                        ? `${formatNumber(p.monthly_send_pool)} shared sends a month`
                                        : "No cap on shared sends"}
                                </div>
                                <ul className="mt-2.5 space-y-1.5">
                                    {!!p.agency_ai_allowance_usd && (
                                        <li className="flex items-start gap-2 text-[12.5px] text-slate-600 leading-snug">
                                            <CheckIcon className="w-3.5 h-3.5 text-sky-600 mt-px shrink-0" />
                                            {fmtUsdTrim(p.agency_ai_allowance_usd)} of shared AI each month
                                        </li>
                                    )}
                                    {(AGENCY_PLAN_FEATURES[p.name ?? ""] ?? []).map((f) => (
                                        <li key={f} className="flex items-start gap-2 text-[12.5px] text-slate-600 leading-snug">
                                            <CheckIcon className="w-3.5 h-3.5 text-sky-600 mt-px shrink-0" />
                                            {f}
                                        </li>
                                    ))}
                                </ul>
                            </div>
                        </div>
                    ))}
                </div>
            )}
            <Link
                to={to}
                className="inline-flex items-center gap-1.5 h-7 px-2.5 rounded-md border border-slate-200 hover:border-slate-300 bg-white text-[12px] text-slate-700 transition-colors"
            >
                <BuildingIcon className="w-3 h-3" />
                {cta}
            </Link>
        </Section>
    );
}
