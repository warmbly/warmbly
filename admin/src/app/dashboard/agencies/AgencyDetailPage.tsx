import { useQuery } from "@tanstack/react-query";
import { Link, useParams } from "react-router-dom";
import { PageHeader } from "@/components/layout/PageHeader";
import { Panel, Property, PropertyList, Stat, StatGrid, StatusBadge } from "@/components/ui/kit";
import { ErrorState } from "@/components/ErrorState";
import { getAgency } from "@/lib/api/client/admin/agencies";
import { AGENCY_STATUS_TONE, poolLabel, usd } from "./agencyFormat";
import { AgencyClientsPanel } from "./AgencyClientsPanel";
import { AgencyCreditCard } from "./AgencyCreditCard";
import { AgencyOverridesCard } from "./AgencyOverridesCard";
import { AgencyPlanCard } from "./AgencyPlanCard";

export default function AgencyDetailPage() {
    const { id = "" } = useParams();
    const q = useQuery({ queryKey: ["admin", "agencies", id], queryFn: () => getAgency(id), enabled: !!id });
    const d = q.data;

    if (q.error) {
        return (
            <>
                <PageHeader title="Agency" breadcrumbs={[{ label: "Agencies", to: "/agencies" }]} />
                <ErrorState error={q.error} onRetry={() => void q.refetch()} />
            </>
        );
    }

    const a = d?.agency;
    return (
        <>
            <PageHeader
                title={a?.name ?? "Agency"}
                breadcrumbs={[{ label: "Agencies", to: "/agencies" }]}
                meta={a ? <StatusBadge tone={AGENCY_STATUS_TONE[a.status] ?? "neutral"}>{a.status.replace(/_/g, " ")}</StatusBadge> : undefined}
            />
            <div className="space-y-4">
                <StatGrid>
                    <Stat
                        label="Plan"
                        value={a?.plan?.name ?? "None"}
                        sub={a?.granted ? "Given by an operator" : undefined}
                        loading={q.isLoading}
                    />
                    <Stat label="Clients" value={a?.client_count.toLocaleString() ?? "0"} loading={q.isLoading} />
                    <Stat label="Pool this period" value={a ? poolLabel(a) : "0"} loading={q.isLoading} />
                    <Stat
                        label="Projected"
                        value={d ? (d.usage.unlimited ? "No cap" : d.usage.projected.toLocaleString()) : "0"}
                        sub={d ? `${d.usage.days_left} days left` : undefined}
                        tone={d && !d.usage.unlimited && d.usage.projected > d.usage.total ? "warning" : undefined}
                        loading={q.isLoading}
                    />
                </StatGrid>
                {d && (
                    <>
                        <AgencyPlanCard agency={d.agency} />
                        <AgencyOverridesCard agency={d.agency} />
                        <AgencyCreditCard detail={d} />
                    </>
                )}
                {a && (
                    <PropertyList title="Details">
                        <Property label="Owner">
                            <Link to={`/users/${a.owner_user_id}`} className="underline underline-offset-2">
                                {a.owner_email}
                            </Link>
                        </Property>
                        <Property label="Billing account">{a.has_billing ? "Yes" : "None"}</Property>
                        <Property label="Period">
                            {a.current_period_start && a.current_period_end
                                ? `${new Date(a.current_period_start).toLocaleDateString()} to ${new Date(a.current_period_end).toLocaleDateString()}`
                                : "None"}
                        </Property>
                        <Property label="Cancels at period end">{a.cancel_at_period_end ? "Yes" : "No"}</Property>
                        <Property label="Top-ups this period">{d?.usage.topups.toLocaleString() ?? "0"}</Property>
                        {d?.ai_balance && !d.ai_balance.unmetered && (
                            <>
                                <Property label="AI balance">
                                    {usd(d.ai_balance.balance_usd)} left ({usd(d.ai_balance.allowance_left_usd)} of {usd(d.ai_balance.allowance_usd)} plan,{" "}
                                    {usd(d.ai_balance.purchased_usd)} bought or added)
                                </Property>
                                <Property label="AI spent this period">
                                    {usd(d.ai_balance.spent_usd)} by clients, {usd(d.ai_balance.moved_usd)} moved to clients
                                </Property>
                            </>
                        )}
                        <Property label="Created">{new Date(a.created_at).toLocaleString()}</Property>
                    </PropertyList>
                )}
                {d && <AgencyClientsPanel detail={d} />}
                <Panel title="Staff">
                    <ul className="text-sm divide-y divide-border">
                        {d?.members.map((m) => (
                            <li key={m.user_id} className="py-1.5 flex items-center gap-3">
                                <Link to={`/users/${m.user_id}`} className="flex-1 truncate underline underline-offset-2">
                                    {m.email}
                                </Link>
                                <span className="text-muted-foreground capitalize">{m.role}</span>
                            </li>
                        ))}
                    </ul>
                </Panel>
            </div>
        </>
    );
}
