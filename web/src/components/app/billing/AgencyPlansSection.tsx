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
                <div className="grid grid-cols-1 sm:grid-cols-2 gap-4 pt-2">
                    {list.map((p) => (
                        <div key={p.id} className="rounded-lg border border-slate-200 bg-white p-4 flex flex-col">
                            <div className="text-[12.5px] font-medium text-slate-900">{p.name}</div>
                            <div className="mt-1 flex items-baseline gap-1">
                                <span className="text-[24px] font-light text-slate-900 tabular-nums">
                                    ${Math.round(monthly(p, interval)).toLocaleString()}
                                </span>
                                <span className="text-[11.5px] text-slate-500">
                                    / month{interval === "annual" && p.price_yearly ? ", billed yearly" : ""}
                                </span>
                            </div>
                            <div className="mt-1 text-[11.5px] text-slate-600">
                                {p.monthly_send_pool
                                    ? `${formatNumber(p.monthly_send_pool)} shared sends a month`
                                    : "No cap on shared sends"}
                            </div>
                            {!!p.agency_ai_allowance_usd && (
                                <div className="text-[11.5px] text-slate-600">
                                    {fmtUsdTrim(p.agency_ai_allowance_usd)} of shared AI each month
                                </div>
                            )}
                            <ul className="mt-3 space-y-1 flex-1">
                                {(AGENCY_PLAN_FEATURES[p.name ?? ""] ?? []).map((f) => (
                                    <li key={f} className="flex gap-1.5 text-[11.5px] text-slate-600">
                                        <CheckIcon className="w-3 h-3 mt-0.5 text-sky-600 shrink-0" />
                                        {f}
                                    </li>
                                ))}
                            </ul>
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
