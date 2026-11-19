// Store economics: GraphKit's wholesale price against the retail price this
// instance charges, per mailbox provider and per domain TLD, and the month's totals.

import { useQuery } from "@tanstack/react-query";
import { Wallet } from "lucide-react";
import { EmptyState, Section, Stat, StatGrid } from "@/components/ui/kit";
import { Skeleton } from "@/components/ui/skeleton";
import { ErrorState } from "@/components/ErrorState";
import { APIError } from "@/lib/api/client";
import { getStoreEconomics } from "@/lib/api/client/admin/store";
import { relative } from "@/app/dashboard/jobs/format";
import { TONE_TEXT } from "@/lib/tones";
import { cn } from "@/lib/utils";
import { providerLabel, usd } from "./format";

const TH = "h-9 px-3 text-left text-xs font-medium text-muted-foreground whitespace-nowrap";
const TD = "px-3 py-2 tabular-nums whitespace-nowrap";

function marginPct(retail: number, wholesale: number): string {
    if (retail <= 0) return "—";
    return `${Math.round(((retail - wholesale) / retail) * 100)}%`;
}

function Margin({ micros }: { micros: number }) {
    return <span className={cn(micros < 0 && TONE_TEXT.danger)}>{usd(micros)}</span>;
}

export function EconomicsTab() {
    const q = useQuery({ queryKey: ["admin", "store", "economics"], queryFn: getStoreEconomics, staleTime: 5 * 60_000, retry: false });
    if (q.isLoading) return <Skeleton className="h-64 w-full rounded-xl" />;
    if (q.error instanceof APIError && q.error.code === "store_disabled") {
        return <EmptyState icon={Wallet} title="The store is off" hint="There are no prices to compare until GRAPHKIT_API_KEY is set." />;
    }
    if (q.error || !q.data) return <ErrorState error={q.error} title="Failed to load store economics" onRetry={() => q.refetch()} />;
    const e = q.data;

    return (
        <div>
            <StatGrid>
                <Stat label="Live mailboxes" value={e.totals.live_mailboxes.toLocaleString()} />
                <Stat label="Monthly retail" value={usd(e.totals.monthly_retail_micros)} />
                <Stat label="Monthly wholesale" value={usd(e.totals.monthly_wholesale_micros)} />
                <Stat label="Monthly margin" value={usd(e.totals.monthly_margin_micros)} tone={e.totals.monthly_margin_micros < 0 ? "danger" : undefined} sub={marginPct(e.totals.monthly_retail_micros, e.totals.monthly_wholesale_micros)} />
            </StatGrid>
            <p className="mt-2 text-xs text-muted-foreground">
                Prices in {e.currency.toUpperCase()}, read from GraphKit {relative(e.updated_at)}. Mailbox totals only; domain income lands once a year per domain.
            </p>

            <Section title="Mailboxes" description="Monthly price per mailbox." className="mt-8">
                <div className="overflow-x-auto rounded-xl border border-border">
                    <table className="w-full border-collapse text-[13px]">
                        <thead className="border-b border-border">
                            <tr>
                                <th className={TH}>Provider</th>
                                <th className={cn(TH, "text-right")}>Wholesale</th>
                                <th className={cn(TH, "text-right")}>Retail</th>
                                <th className={cn(TH, "text-right")}>Margin</th>
                                <th className={cn(TH, "text-right")}>Margin %</th>
                                <th className={TH}>Scheduled change</th>
                                <th className={cn(TH, "text-right")}>Live</th>
                                <th className={cn(TH, "text-right")}>Monthly margin</th>
                            </tr>
                        </thead>
                        <tbody className="divide-y divide-border/70">
                            {e.mailboxes.map((m) => (
                                <tr key={m.provider}>
                                    <td className={cn(TD, "font-medium")}>{providerLabel(m.provider)}</td>
                                    <td className={cn(TD, "text-right")}>{usd(m.wholesale_monthly_micros)}</td>
                                    <td className={cn(TD, "text-right")}>{usd(m.retail_monthly_micros)}</td>
                                    <td className={cn(TD, "text-right")}><Margin micros={m.margin_micros} /></td>
                                    <td className={cn(TD, "text-right text-muted-foreground")}>{marginPct(m.retail_monthly_micros, m.wholesale_monthly_micros)}</td>
                                    <td className={TD}>
                                        {m.scheduled_wholesale_monthly_micros != null ? (
                                            <span className={cn(m.scheduled_wholesale_monthly_micros > m.retail_monthly_micros && TONE_TEXT.danger)}>
                                                {usd(m.scheduled_wholesale_monthly_micros)}
                                                {m.scheduled_from && <span className="text-muted-foreground"> from {new Date(m.scheduled_from).toLocaleDateString()}</span>}
                                            </span>
                                        ) : (
                                            <span className="text-subtle-foreground">None</span>
                                        )}
                                    </td>
                                    <td className={cn(TD, "text-right")}>{m.live_count.toLocaleString()}</td>
                                    <td className={cn(TD, "text-right")}><Margin micros={m.margin_micros * m.live_count} /></td>
                                </tr>
                            ))}
                            {e.mailboxes.length === 0 && (
                                <tr>
                                    <td colSpan={8} className="px-3 py-6 text-center text-muted-foreground">No mailbox prices</td>
                                </tr>
                            )}
                        </tbody>
                    </table>
                </div>
            </Section>

            <Section title="Domains" description="Per year, at the TLD's minimum term.">
                <div className="overflow-x-auto rounded-xl border border-border">
                    <table className="w-full border-collapse text-[13px]">
                        <thead className="border-b border-border">
                            <tr>
                                <th className={TH}>TLD</th>
                                <th className={cn(TH, "text-right")}>Min years</th>
                                <th className={cn(TH, "text-right")}>Register wholesale</th>
                                <th className={cn(TH, "text-right")}>Register retail</th>
                                <th className={cn(TH, "text-right")}>Register margin</th>
                                <th className={cn(TH, "text-right")}>Renew wholesale</th>
                                <th className={cn(TH, "text-right")}>Renew retail</th>
                                <th className={cn(TH, "text-right")}>Renew margin</th>
                            </tr>
                        </thead>
                        <tbody className="divide-y divide-border/70">
                            {e.domains.map((d) => (
                                <tr key={d.tld}>
                                    <td className={cn(TD, "font-mono")}>.{d.tld.replace(/^\./, "")}</td>
                                    <td className={cn(TD, "text-right")}>{d.min_years}</td>
                                    <td className={cn(TD, "text-right")}>{usd(d.wholesale_register_micros)}</td>
                                    <td className={cn(TD, "text-right")}>{usd(d.retail_register_micros)}</td>
                                    <td className={cn(TD, "text-right")}><Margin micros={d.retail_register_micros - d.wholesale_register_micros} /></td>
                                    <td className={cn(TD, "text-right")}>{usd(d.wholesale_renew_micros)}</td>
                                    <td className={cn(TD, "text-right")}>{usd(d.retail_renew_micros)}</td>
                                    <td className={cn(TD, "text-right")}><Margin micros={d.retail_renew_micros - d.wholesale_renew_micros} /></td>
                                </tr>
                            ))}
                            {e.domains.length === 0 && (
                                <tr>
                                    <td colSpan={8} className="px-3 py-6 text-center text-muted-foreground">No domain prices</td>
                                </tr>
                            )}
                        </tbody>
                    </table>
                </div>
            </Section>
        </div>
    );
}
