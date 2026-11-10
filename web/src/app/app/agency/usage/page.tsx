import { AgencyPage } from "@/components/app/agency/AgencyPage";
import { Link } from "@tanstack/react-router";
import { EmptyBlock, PageBody, SectionBar } from "@/components/layout/Page";
import { useAgencyAIBalance, useAgencyUsage, useCurrentAgency } from "@/lib/api/hooks/app/agency/useAgency";
import { ClientBreakdown, PoolMeter } from "@/components/app/agency/PoolMeter";
import { AIBalanceMeter, AIClientBreakdown } from "@/components/app/agency/AIBalanceMeter";
import { cn } from "@/lib/utils";

function AgencyUsage() {
    const { agency, billingEnabled } = useCurrentAgency();
    const usage = useAgencyUsage(agency?.id);
    const ai = useAgencyAIBalance(agency?.id);
    if (!agency) return null;
    const u = usage.data;
    const b = ai.data && !ai.data.unmetered ? ai.data : null;
    return (
        <PageBody>
            <div className={cn(b && "grid lg:grid-cols-2 lg:[&>*:first-child]:border-r lg:[&>*:first-child]:border-slate-200")}>
                {u ? <PoolMeter usage={u} /> : usage.isError ? <EmptyBlock title="Could not load the send pool" body="Try again in a moment." /> : <div />}
                {b && <AIBalanceMeter balance={b} />}
            </div>
            {billingEnabled && u && !u.unlimited && (u.remaining === 0 || u.projected > u.total) && (
                <div className="px-5 py-2.5 border-b border-slate-200 bg-amber-50/60 text-[12px] text-amber-800 flex items-center gap-2 flex-wrap">
                    {u.remaining === 0
                        ? "The pool is used up, so campaigns across your clients wait until the period renews."
                        : "At this pace the pool runs out before the period renews."}
                    <Link to="/app/agency/billing" className="ml-auto font-medium underline underline-offset-2">
                        Add sends
                    </Link>
                </div>
            )}
            {b && b.balance_usd <= 0 && (
                <div className="px-5 py-2.5 border-b border-slate-200 bg-amber-50/60 text-[12px] text-amber-800 flex items-center gap-2 flex-wrap">
                    The AI balance is used up, so AI in a client workspace stops once that workspace's own balance runs out.
                    <Link to="/app/agency/billing" className="ml-auto font-medium underline underline-offset-2">
                        Buy AI balance
                    </Link>
                </div>
            )}
            {u && (
                <>
                    <SectionBar label="Sends by client" count={u.clients.length} />
                    {u.clients.length === 0 ? (
                        <EmptyBlock title="No clients yet" body="Sends from every client workspace count toward this pool." />
                    ) : (
                        <ClientBreakdown usage={u} />
                    )}
                </>
            )}
            {b && b.clients.length > 0 && (
                <>
                    <SectionBar label="AI by client" count={b.clients.length} />
                    <AIClientBreakdown balance={b} />
                </>
            )}
            {u && (
                <p className="px-5 py-4 text-[11.5px] text-slate-500 leading-relaxed max-w-[72ch]">
                    The pool counts campaign emails. Warmup does not use it. Every mailbox keeps its own daily cap, minimum
                    gap, warmup ramp and health checks, so a large pool never makes one mailbox send faster than is safe.
                    {b &&
                        " AI in a client workspace spends that workspace's own balance first, then the agency balance, up to the client's AI cap."}
                </p>
            )}
        </PageBody>
    );
}

export default function AgencyUsagePage() {
    return (
        <AgencyPage title="Usage">
            <AgencyUsage />
        </AgencyPage>
    );
}
