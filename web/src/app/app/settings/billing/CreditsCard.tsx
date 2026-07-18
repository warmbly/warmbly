// AI credits card for the billing page: a balance ring (monthly pool vs plan
// allowance), the purchased top-up pool, next reset date, top-up pack buttons
// that redirect to Stripe Checkout, and the paged transaction log.
//
// Fulfillment of a purchase is webhook-only; on return from Stripe the balance
// refreshes via the realtime spine (credit_purchase / credit_grant).

import React from "react";
import toast from "react-hot-toast";
import { Loader2Icon, SparklesIcon, PlusIcon } from "lucide-react";
import useCredits from "@/lib/api/hooks/app/subscription/useCredits";
import useCreditTransactions from "@/lib/api/hooks/app/subscription/useCreditTransactions";
import useCreateCreditCheckout from "@/lib/api/hooks/app/subscription/useCreateCreditCheckout";
import type { AppError } from "@/lib/api/client/normalizeError";
import type { CreditTransaction } from "@/lib/api/models/app/subscription/Credits";
import buildError from "@/lib/helper/buildError";
import { Section, TableSurface } from "../_components/SectionShell";

export default function CreditsCard({ isPaid }: { isPaid: boolean }) {
    const credits = useCredits();
    const checkout = useCreateCreditCheckout();
    const [buying, setBuying] = React.useState<string | null>(null);

    async function buyPack(pack: string) {
        setBuying(pack);
        try {
            const base = `${window.location.origin}/app/settings/billing`;
            const { checkout_url } = await toast.promise(
                checkout.mutateAsync({
                    pack,
                    success_url: `${base}?topup=success`,
                    cancel_url: `${base}?topup=cancel`,
                }),
                {
                    loading: "Starting checkout…",
                    success: "Redirecting to checkout…",
                    error: (e: AppError) => buildError(e),
                },
            );
            window.location.assign(checkout_url);
        } catch {
            /* surfaced via toast */
        } finally {
            setBuying(null);
        }
    }

    const data = credits.data;
    const allowance = data?.monthly_allowance ?? 0;
    const monthly = data?.monthly_balance ?? 0;
    const purchased = data?.purchased_balance ?? 0;
    const total = data?.balance ?? 0;
    const packs = data?.packs ?? [];

    return (
        <Section
            eyebrow="AI credits"
            description="Credits power AI features (writing, research, the assistant, automation steps). Your plan grants a monthly allowance that resets each cycle; top-ups never expire."
        >
            {credits.isPending ? (
                <div className="h-24 rounded bg-slate-100 animate-pulse" />
            ) : (
                <div className="flex flex-wrap items-center gap-5">
                    <BalanceRing monthly={monthly} allowance={allowance} />
                    <div className="min-w-0 flex-1 basis-[200px]">
                        <div className="flex items-baseline gap-1.5">
                            <span className="text-[22px] font-semibold text-slate-900 tabular-nums">
                                {total.toLocaleString()}
                            </span>
                            <span className="text-[12px] text-slate-500">credits available</span>
                        </div>
                        <div className="mt-1.5 grid grid-cols-1 sm:grid-cols-2 gap-x-6 gap-y-1 text-[11.5px]">
                            <Stat
                                label="Monthly allowance"
                                value={`${monthly.toLocaleString()} / ${allowance.toLocaleString()}`}
                            />
                            <Stat label="Purchased" value={purchased.toLocaleString()} />
                            <Stat label="Resets" value={formatReset(data?.next_reset_at)} />
                            <Stat
                                label="Lifetime purchased"
                                value={(data?.total_purchased ?? 0).toLocaleString()}
                            />
                        </div>
                    </div>
                </div>
            )}

            {/* Top-up packs */}
            <div className="mt-1">
                <div className="text-[10px] uppercase tracking-[0.14em] text-slate-400 mb-2">
                    Buy more credits
                </div>
                {!isPaid ? (
                    <p className="text-[11.5px] text-slate-500 leading-relaxed">
                        Credit top-ups are available on paid plans. Upgrade above to buy packs.
                    </p>
                ) : packs.length === 0 ? (
                    <p className="text-[11.5px] text-slate-500 leading-relaxed">
                        Credit packs aren't configured for this workspace yet.
                    </p>
                ) : (
                    <div className="flex flex-wrap gap-2">
                        {packs.map((p) => (
                            <button
                                key={p.key}
                                type="button"
                                onClick={() => buyPack(p.key)}
                                disabled={buying !== null}
                                className="h-8 px-3 rounded-md border border-slate-200 hover:border-sky-400 hover:bg-sky-50 text-[12px] font-medium text-slate-700 hover:text-sky-700 inline-flex items-center gap-1.5 transition-colors disabled:opacity-50"
                            >
                                {buying === p.key ? (
                                    <Loader2Icon className="w-3 h-3 animate-spin" />
                                ) : (
                                    <PlusIcon className="w-3 h-3" />
                                )}
                                {p.credits.toLocaleString()} credits
                            </button>
                        ))}
                    </div>
                )}
            </div>

            {/* Transaction log */}
            <TransactionTable />
        </Section>
    );
}

function Stat({ label, value }: { label: string; value: string }) {
    return (
        <div className="flex items-center justify-between gap-2">
            <span className="text-slate-500">{label}</span>
            <span className="font-mono tabular-nums text-slate-700">{value}</span>
        </div>
    );
}

// BalanceRing draws a donut of monthly-pool remaining vs the plan allowance.
function BalanceRing({ monthly, allowance }: { monthly: number; allowance: number }) {
    const pct = allowance > 0 ? Math.max(0, Math.min(1, monthly / allowance)) : 0;
    const r = 26;
    const c = 2 * Math.PI * r;
    const dash = c * pct;
    const low = pct <= 0.15;
    return (
        <div className="relative shrink-0">
            <svg width="72" height="72" viewBox="0 0 72 72" className="-rotate-90">
                <circle cx="36" cy="36" r={r} fill="none" stroke="#e2e8f0" strokeWidth="7" />
                <circle
                    cx="36"
                    cy="36"
                    r={r}
                    fill="none"
                    stroke={low ? "#dc2626" : "#0284c7"}
                    strokeWidth="7"
                    strokeLinecap="round"
                    strokeDasharray={`${dash} ${c - dash}`}
                />
            </svg>
            <div className="absolute inset-0 flex flex-col items-center justify-center">
                <SparklesIcon className={`w-3.5 h-3.5 ${low ? "text-red-500" : "text-sky-600"}`} />
            </div>
        </div>
    );
}

function TransactionTable() {
    const txns = useCreditTransactions(25);
    const rows: CreditTransaction[] = txns.data?.pages.flatMap((p) => p.data) ?? [];

    return (
        <div className="mt-1">
            <div className="text-[10px] uppercase tracking-[0.14em] text-slate-400 mb-2">
                Transactions
            </div>
            {txns.isPending ? (
                <div className="h-16 rounded bg-slate-100 animate-pulse" />
            ) : rows.length === 0 ? (
                <p className="text-[11.5px] text-slate-500 leading-relaxed">
                    No credit activity yet.
                </p>
            ) : (
                <>
                    <TableSurface>
                        <table className="w-full text-[12px]">
                            <thead>
                                <tr className="text-left text-[10.5px] uppercase tracking-[0.08em] text-slate-400 border-b border-slate-200">
                                    <th className="font-medium px-3 py-2">Activity</th>
                                    <th className="font-medium px-3 py-2 text-right">Amount</th>
                                    <th className="font-medium px-3 py-2 text-right">Balance</th>
                                    <th className="font-medium px-3 py-2 text-right">When</th>
                                </tr>
                            </thead>
                            <tbody className="divide-y divide-slate-100">
                                {rows.map((t) => (
                                    <TransactionRow key={t.id} row={t} />
                                ))}
                            </tbody>
                        </table>
                    </TableSurface>
                    {txns.hasNextPage && (
                        <button
                            type="button"
                            onClick={() => txns.fetchNextPage()}
                            disabled={txns.isFetchingNextPage}
                            className="mt-2 h-7 px-2.5 rounded-md border border-slate-200 hover:border-slate-300 text-[12px] text-slate-700 hover:text-slate-900 transition-colors inline-flex items-center gap-1.5 disabled:opacity-60"
                        >
                            {txns.isFetchingNextPage && (
                                <Loader2Icon className="w-3 h-3 animate-spin" />
                            )}
                            Load more
                        </button>
                    )}
                </>
            )}
        </div>
    );
}

function TransactionRow({ row }: { row: CreditTransaction }) {
    const positive = row.amount >= 0;
    return (
        <tr className="text-slate-700">
            <td className="px-3 py-2">
                <span className="text-slate-900">{describeReason(row.reason)}</span>
                {row.model_used && (
                    <span className="ml-1.5 text-[10.5px] text-slate-400 font-mono">
                        {row.model_used}
                    </span>
                )}
                {row.tokens_used > 0 && (
                    <span className="ml-1.5 text-[10.5px] text-slate-400 tabular-nums">
                        {row.tokens_used.toLocaleString()} tok
                    </span>
                )}
                {describeContext(row) && (
                    <div className="mt-0.5 text-[10.5px] text-slate-400 truncate max-w-[360px]">
                        {describeContext(row)}
                    </div>
                )}
            </td>
            <td
                className={`px-3 py-2 text-right font-mono tabular-nums ${
                    positive ? "text-emerald-700" : "text-slate-600"
                }`}
            >
                {positive ? "+" : ""}
                {row.amount.toLocaleString()}
            </td>
            <td className="px-3 py-2 text-right font-mono tabular-nums text-slate-500">
                {(row.balance_after + row.purchased_balance_after).toLocaleString()}
            </td>
            <td className="px-3 py-2 text-right text-slate-500 tabular-nums">
                {formatWhen(row.created_at)}
            </td>
        </tr>
    );
}

// describeContext renders the charge's attribution: exactly what ran and who
// triggered it, from the structured context recorded with the transaction.
function describeContext(row: CreditTransaction): string {
    const c = row.context ?? {};
    const parts: string[] = [];
    if (c.campaign_name || c.campaign_id) parts.push(`Campaign “${c.campaign_name || c.campaign_id}”`);
    if (c.contact_email) parts.push(c.contact_email);
    if (c.automation_name || c.automation_id) parts.push(`Automation “${c.automation_name || c.automation_id}”`);
    if (c.thread_id) parts.push(`thread ${c.thread_id.slice(0, 8)}`);
    if (c.session_id) parts.push("assistant session");
    if (c.detail) parts.push(c.detail);
    if (row.actor_user_id) parts.push("triggered by a teammate");
    return parts.join(" · ");
}

// describeReason maps a ledger reason code to a short human label.
function describeReason(reason: string): string {
    const map: Record<string, string> = {
        writing_assistant: "Writing assistant",
        writing_assistant_refund: "Writing assistant refund",
        agent_iteration: "AI assistant",
        reply_draft: "Reply draft",
        research_run: "Contact research",
        automation_ai: "Automation AI step",
        automation_ai_refund: "Automation AI refund",
        campaign_ai: "Campaign switch",
        campaign_ai_refund: "Campaign switch refund",
        campaign_ai_search: "Campaign switch web search",
        reply_draft_refund: "Reply draft refund",
        agent_iteration_refund: "AI assistant refund",
        inbox_agent_draft: "Inbox agent",
        credit_topup: "Top-up purchase",
        credit_auto_topup: "Auto top-up",
        monthly_reset: "Monthly allowance",
        trial_grant: "Trial credits",
    };
    return map[reason] ?? reason.replace(/_/g, " ");
}

function formatReset(value: string | null | undefined): string {
    if (!value) return "on renewal";
    const d = new Date(value);
    if (Number.isNaN(d.getTime())) return "on renewal";
    return d.toLocaleDateString("en-US", { month: "short", day: "numeric" });
}

function formatWhen(value: string): string {
    const d = new Date(value);
    if (Number.isNaN(d.getTime())) return "—";
    return d.toLocaleDateString("en-US", {
        month: "short",
        day: "numeric",
        hour: "numeric",
        minute: "2-digit",
    });
}
