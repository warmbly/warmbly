// What the balance was spent on and added from, newest first, paged.

import { Loader2Icon } from "lucide-react";
import useCreditTransactions from "@/lib/api/hooks/app/subscription/useCreditTransactions";
import type { CreditTransaction } from "@/lib/api/models/app/subscription/Credits";
import { fmtUsd, fmtUsdCost } from "@/lib/money";
import { reasonLabel } from "@/components/app/billing/balanceModel";
import { Section, TableSurface } from "../_components/SectionShell";

export default function BalanceActivity() {
    const txns = useCreditTransactions(25);
    const rows: CreditTransaction[] = txns.data?.pages.flatMap((p) => p.data) ?? [];

    return (
        <Section eyebrow="Activity" description="Every charge and every dollar added, with the tokens behind each AI call.">
            {txns.isPending ? (
                <div className="h-16 rounded bg-slate-100 animate-pulse" />
            ) : rows.length === 0 ? (
                <p className="text-[11.5px] text-slate-500 leading-relaxed">No activity yet.</p>
            ) : (
                <>
                    <TableSurface>
                        <table className="w-full text-[12px]">
                            <thead>
                                <tr className="text-left text-[10.5px] uppercase tracking-[0.08em] text-slate-400 border-b border-slate-200">
                                    <th className="font-medium px-3 py-2">Activity</th>
                                    <th className="font-medium px-3 py-2 text-right">Amount</th>
                                    <th className="font-medium px-3 py-2 text-right hidden sm:table-cell">Balance</th>
                                    <th className="font-medium px-3 py-2 text-right">When</th>
                                </tr>
                            </thead>
                            <tbody className="divide-y divide-slate-100">
                                {rows.map((t) => (
                                    <ActivityRow key={t.id} row={t} />
                                ))}
                            </tbody>
                        </table>
                    </TableSurface>
                    {txns.hasNextPage && (
                        <button
                            type="button"
                            onClick={() => txns.fetchNextPage()}
                            disabled={txns.isFetchingNextPage}
                            className="h-7 px-2.5 rounded-md border border-slate-200 hover:border-slate-300 text-[12px] text-slate-700 hover:text-slate-900 transition-colors inline-flex items-center gap-1.5 disabled:opacity-60"
                        >
                            {txns.isFetchingNextPage && <Loader2Icon className="w-3 h-3 animate-spin" />}
                            Load more
                        </button>
                    )}
                </>
            )}
        </Section>
    );
}

function ActivityRow({ row }: { row: CreditTransaction }) {
    const positive = row.amount_usd > 0;
    const tokens = (row.input_tokens ?? 0) + (row.output_tokens ?? 0);
    const context = describeContext(row);
    return (
        <tr className="text-slate-700 align-top">
            <td className="px-3 py-2 min-w-0">
                <span className="text-slate-900">{reasonLabel(row.reason)}</span>
                {row.model_used && <span className="ml-1.5 text-[10.5px] text-slate-400 font-mono">{row.model_used}</span>}
                {tokens > 0 && (
                    <span
                        className="ml-1.5 text-[10.5px] text-slate-400 tabular-nums"
                        title={`${(row.input_tokens ?? 0).toLocaleString()} in, ${(row.output_tokens ?? 0).toLocaleString()} out`}
                    >
                        {tokens.toLocaleString()} tok
                    </span>
                )}
                {row.quantity > 1 && <span className="ml-1.5 text-[10.5px] text-slate-400 tabular-nums">×{row.quantity}</span>}
                {context && <div className="mt-0.5 text-[10.5px] text-slate-400 truncate max-w-[360px]">{context}</div>}
            </td>
            <td className={`px-3 py-2 text-right font-mono tabular-nums ${positive ? "text-emerald-700" : "text-slate-600"}`}>
                {positive ? `+${fmtUsd(row.amount_usd)}` : `-${fmtUsdCost(Math.abs(row.amount_usd))}`}
            </td>
            <td className="px-3 py-2 text-right font-mono tabular-nums text-slate-500 hidden sm:table-cell">
                {fmtUsd(row.balance_after_usd)}
            </td>
            <td className="px-3 py-2 text-right text-slate-500 tabular-nums whitespace-nowrap">{formatWhen(row.created_at)}</td>
        </tr>
    );
}

// What ran and who triggered it, from the context recorded with the charge.
function describeContext(row: CreditTransaction): string {
    const c = row.context ?? {};
    const parts: string[] = [];
    if (c.campaign_name || c.campaign_id) parts.push(`Campaign “${c.campaign_name || c.campaign_id}”`);
    if (c.contact_email) parts.push(c.contact_email);
    if (c.automation_name || c.automation_id) parts.push(`Automation “${c.automation_name || c.automation_id}”`);
    if (c.thread_id) parts.push(`thread ${c.thread_id.slice(0, 8)}`);
    if (c.session_id) parts.push("Remie session");
    if (c.detail) parts.push(c.detail);
    if (row.actor_user_id) parts.push("by a teammate");
    return parts.join(" · ");
}

function formatWhen(value: string | Date): string {
    const d = new Date(value);
    if (Number.isNaN(d.getTime())) return "";
    return d.toLocaleDateString("en-US", { month: "short", day: "numeric", hour: "numeric", minute: "2-digit" });
}
