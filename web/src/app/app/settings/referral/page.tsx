// Referral program — owner-only, organization-scoped.
//
// Invitees get a discount for a few months; the referrer earns account credit
// equal to one month of the invitee's plan, applied to their invoices. This
// page surfaces the share link + code, the running balance, and the two
// ledgers (referrals + earnings history).

import React from "react";
import {
    CheckIcon,
    CopyIcon,
    GiftIcon,
    Loader2Icon,
    LockIcon,
} from "lucide-react";
import toast from "react-hot-toast";
import useFeatureAccess from "@/hooks/useFeatureAccess";
import useReferral from "@/lib/api/hooks/app/subscription/useReferral";
import useReferralAttributions from "@/lib/api/hooks/app/subscription/useReferralAttributions";
import useReferralEarnings from "@/lib/api/hooks/app/subscription/useReferralEarnings";
import type {
    ReferralAttribution,
    ReferralAttributionStatus,
    ReferralEarningsTransaction,
} from "@/lib/api/models/app/subscription/Referral";
import { Row, Section, SectionShell, TableSurface } from "../_components/SectionShell";

export default function ReferralSettingsPage() {
    const access = useFeatureAccess();
    const referral = useReferral();
    const summary = referral.data;
    const currency = summary?.currency ?? "usd";

    if (!access.loading && !access.isOwner) {
        return (
            <SectionShell title="Refer & earn" description="Owner only.">
                <Section eyebrow="Permission denied">
                    <div className="flex items-start gap-3">
                        <div className="size-9 rounded-md bg-amber-50 border border-amber-200 text-amber-700 flex items-center justify-center shrink-0">
                            <LockIcon className="w-4 h-4" />
                        </div>
                        <div>
                            <div className="text-[13px] font-semibold text-slate-900">
                                Only the workspace owner can manage referrals
                            </div>
                            <p className="text-[12px] text-slate-500 leading-relaxed mt-1 max-w-md">
                                Referral credit is applied to this workspace's invoices, so the
                                program is scoped to the owner role. Ask your owner to share the
                                link if you'd like to refer someone.
                            </p>
                        </div>
                    </div>
                </Section>
            </SectionShell>
        );
    }

    return (
        <SectionShell
            title="Refer & earn"
            description="Invite other teams to Warmbly and earn account credit on your invoices."
        >
            <Section
                eyebrow="How it works"
                description="Share your link, they save, you earn."
            >
                <div className="rounded-md border border-sky-100 bg-sky-50/60 p-4">
                    <div className="flex items-start gap-3">
                        <div className="size-9 rounded-md bg-white border border-sky-200 text-sky-700 flex items-center justify-center shrink-0">
                            <GiftIcon className="w-4 h-4" />
                        </div>
                        <div className="min-w-0">
                            <div className="text-[13px] font-semibold text-slate-900">
                                Give {summary?.invitee_percent_off ?? 0}%, get a month of credit
                            </div>
                            <p className="text-[12px] text-slate-600 leading-relaxed mt-1 max-w-lg">
                                Anyone who signs up with your link gets{" "}
                                <span className="font-medium text-slate-900">
                                    {summary?.invitee_percent_off ?? 0}% off
                                </span>{" "}
                                for their first{" "}
                                <span className="font-medium text-slate-900">
                                    {summary?.invitee_months ?? 0}{" "}
                                    {(summary?.invitee_months ?? 0) === 1 ? "month" : "months"}
                                </span>
                                . Once they're a paying customer you earn account credit equal to
                                one month of their plan, applied automatically to your invoices.
                            </p>
                        </div>
                    </div>
                </div>
            </Section>

            <Section
                eyebrow="Your link"
                description="Share this link or code. New signups are attributed to you automatically."
            >
                {referral.isPending ? (
                    <div className="h-9 rounded bg-slate-100 animate-pulse" />
                ) : referral.isError || !summary ? (
                    <p className="text-[12px] text-slate-500">
                        Couldn't load your referral link. Reload the page to try again.
                    </p>
                ) : (
                    <>
                        <Row
                            label="Share link"
                            description="A direct signup link with your code attached."
                            align="start"
                        >
                            <CopyChip value={summary.share_url} label="Copy link" mono />
                        </Row>
                        <Row
                            label="Referral code"
                            description="For anyone entering a code manually at signup."
                            align="start"
                        >
                            <CopyChip value={summary.code} label="Copy code" mono uppercase />
                        </Row>
                    </>
                )}
            </Section>

            <Section
                eyebrow="Earnings"
                description="Credit you've earned. Balance is applied to your invoices automatically."
            >
                <div className="grid grid-cols-2 sm:grid-cols-4 gap-2">
                    <StatCard
                        label="Available credit"
                        value={formatMoney(summary?.balance_cents ?? 0, currency)}
                        loading={referral.isPending}
                        accent
                    />
                    <StatCard
                        label="Lifetime earned"
                        value={formatMoney(summary?.lifetime_earned_cents ?? 0, currency)}
                        loading={referral.isPending}
                    />
                    <StatCard
                        label="Total referred"
                        value={String(summary?.total_referred ?? 0)}
                        loading={referral.isPending}
                    />
                    <StatCard
                        label="Rewarded"
                        value={String(summary?.rewarded ?? 0)}
                        loading={referral.isPending}
                    />
                </div>
                <div className="flex flex-wrap items-center gap-x-4 gap-y-1.5 text-[11.5px] text-slate-500">
                    <CountPill tone="muted" label="Pending" value={summary?.pending ?? 0} />
                    <CountPill tone="sky" label="Qualified" value={summary?.qualified ?? 0} />
                    <CountPill tone="emerald" label="Rewarded" value={summary?.rewarded ?? 0} />
                </div>
            </Section>

            <Section
                eyebrow="Your referrals"
                description="Teams that signed up with your link and where they are in the reward flow."
            >
                <AttributionsTable currency={currency} />
            </Section>

            <Section
                eyebrow="Earnings history"
                description="Every credit and adjustment applied to your balance."
            >
                <EarningsTable currency={currency} />
            </Section>
        </SectionShell>
    );
}

// CopyChip renders a value in a bordered chip with a copy-to-clipboard button.
function CopyChip({
    value,
    label,
    mono,
    uppercase,
}: {
    value: string;
    label: string;
    mono?: boolean;
    uppercase?: boolean;
}) {
    const [copied, setCopied] = React.useState(false);
    function copy() {
        navigator.clipboard.writeText(value).then(
            () => {
                setCopied(true);
                toast.success("Copied to clipboard");
                setTimeout(() => setCopied(false), 2000);
            },
            () => toast.error("Couldn't copy to clipboard"),
        );
    }
    return (
        <div className="flex items-center gap-2 w-full sm:w-auto">
            <div
                className={`flex-1 sm:w-[280px] min-w-0 h-7 px-2.5 rounded-md border border-slate-200 bg-slate-50 text-[12px] text-slate-700 flex items-center truncate ${
                    mono ? "font-mono" : ""
                } ${uppercase ? "uppercase" : ""}`}
                title={value}
            >
                <span className="truncate">{value}</span>
            </div>
            <button
                type="button"
                onClick={copy}
                className="h-7 px-2.5 rounded-md bg-slate-900 hover:bg-slate-800 text-white text-[12px] font-medium inline-flex items-center gap-1.5 transition-colors shrink-0"
            >
                {copied ? <CheckIcon className="w-3 h-3" /> : <CopyIcon className="w-3 h-3" />}
                {copied ? "Copied" : label}
            </button>
        </div>
    );
}

function StatCard({
    label,
    value,
    loading,
    accent,
}: {
    label: string;
    value: string;
    loading?: boolean;
    accent?: boolean;
}) {
    return (
        <div
            className={`rounded-md border p-3 ${
                accent ? "border-sky-200 bg-sky-50/60" : "border-slate-200 bg-white"
            }`}
        >
            <div className="text-[10px] uppercase tracking-[0.1em] font-medium text-slate-400">
                {label}
            </div>
            {loading ? (
                <div className="h-5 mt-1.5 w-16 rounded bg-slate-100 animate-pulse" />
            ) : (
                <div
                    className={`text-[16px] font-semibold tabular-nums mt-0.5 ${
                        accent ? "text-sky-700" : "text-slate-900"
                    }`}
                >
                    {value}
                </div>
            )}
        </div>
    );
}

function CountPill({
    label,
    value,
    tone,
}: {
    label: string;
    value: number;
    tone: "muted" | "sky" | "emerald";
}) {
    const dot =
        tone === "emerald"
            ? "bg-emerald-500"
            : tone === "sky"
              ? "bg-sky-500"
              : "bg-slate-300";
    return (
        <span className="inline-flex items-center gap-1.5">
            <span className={`size-1.5 rounded-full ${dot}`} />
            <span className="text-slate-500">{label}</span>
            <span className="font-mono tabular-nums text-slate-700">{value}</span>
        </span>
    );
}

function AttributionsTable({ currency }: { currency: string }) {
    const query = useReferralAttributions();
    const rows = query.attributions;

    if (query.isPending) {
        return <div className="h-16 rounded bg-slate-100 animate-pulse" />;
    }
    if (rows.length === 0) {
        return (
            <p className="text-[12px] text-slate-500 leading-relaxed">
                No referrals yet. Share your link to get started.
            </p>
        );
    }

    return (
        <>
            <TableSurface>
                <table className="w-full text-[12px]">
                    <thead>
                        <tr className="text-left text-[10.5px] uppercase tracking-[0.08em] text-slate-400 border-b border-slate-200">
                            <th className="font-medium px-3 py-2">Referred org</th>
                            <th className="font-medium px-3 py-2">Status</th>
                            <th className="font-medium px-3 py-2 text-right">Reward</th>
                            <th className="font-medium px-3 py-2 text-right">Date</th>
                        </tr>
                    </thead>
                    <tbody className="divide-y divide-slate-100">
                        {rows.map((r) => (
                            <AttributionRow key={r.id} row={r} fallbackCurrency={currency} />
                        ))}
                    </tbody>
                </table>
            </TableSurface>
            <LoadMore query={query} />
        </>
    );
}

function AttributionRow({
    row,
    fallbackCurrency,
}: {
    row: ReferralAttribution;
    fallbackCurrency: string;
}) {
    return (
        <tr className="text-slate-700">
            <td className="px-3 py-2">
                <span className="inline-flex items-center gap-1.5">
                    <span className="text-slate-500">Referred org</span>
                    <span className="font-mono text-[11px] text-slate-400">
                        {shortId(row.invitee_org_id)}
                    </span>
                </span>
            </td>
            <td className="px-3 py-2">
                <AttributionStatusBadge status={row.status} />
            </td>
            <td className="px-3 py-2 text-right font-mono tabular-nums">
                {row.reward_cents > 0
                    ? formatMoney(row.reward_cents, row.reward_currency || fallbackCurrency)
                    : "—"}
            </td>
            <td className="px-3 py-2 text-right text-slate-500 tabular-nums">
                {formatDate(row.rewarded_at ?? row.qualified_at ?? row.created_at)}
            </td>
        </tr>
    );
}

function AttributionStatusBadge({ status }: { status: ReferralAttributionStatus }) {
    const map: Record<ReferralAttributionStatus, { label: string; cls: string }> = {
        pending: { label: "Pending", cls: "bg-slate-100 text-slate-500 border-slate-200" },
        qualified: { label: "Qualified", cls: "bg-sky-50 text-sky-700 border-sky-100" },
        rewarded: { label: "Rewarded", cls: "bg-emerald-50 text-emerald-700 border-emerald-100" },
        void: { label: "Void", cls: "bg-slate-100 text-slate-400 border-slate-200" },
    };
    const { label, cls } = map[status];
    return (
        <span
            className={`inline-flex items-center text-[10px] uppercase tracking-[0.08em] font-semibold rounded-sm px-1.5 py-0.5 border ${cls}`}
        >
            {label}
        </span>
    );
}

function EarningsTable({ currency }: { currency: string }) {
    const query = useReferralEarnings();
    const rows = query.earnings;

    if (query.isPending) {
        return <div className="h-16 rounded bg-slate-100 animate-pulse" />;
    }
    if (rows.length === 0) {
        return (
            <p className="text-[12px] text-slate-500 leading-relaxed">
                No earnings yet. Credit shows up here once a referral is rewarded.
            </p>
        );
    }

    return (
        <>
            <TableSurface>
                <table className="w-full text-[12px]">
                    <thead>
                        <tr className="text-left text-[10.5px] uppercase tracking-[0.08em] text-slate-400 border-b border-slate-200">
                            <th className="font-medium px-3 py-2">Reason</th>
                            <th className="font-medium px-3 py-2 text-right">Amount</th>
                            <th className="font-medium px-3 py-2 text-right">Balance</th>
                            <th className="font-medium px-3 py-2 text-right">Date</th>
                        </tr>
                    </thead>
                    <tbody className="divide-y divide-slate-100">
                        {rows.map((t) => (
                            <EarningsRow key={t.id} row={t} fallbackCurrency={currency} />
                        ))}
                    </tbody>
                </table>
            </TableSurface>
            <LoadMore query={query} />
        </>
    );
}

function EarningsRow({
    row,
    fallbackCurrency,
}: {
    row: ReferralEarningsTransaction;
    fallbackCurrency: string;
}) {
    const positive = row.amount_cents >= 0;
    const cur = row.currency || fallbackCurrency;
    return (
        <tr className="text-slate-700">
            <td className="px-3 py-2">{humanizeReason(row.reason)}</td>
            <td
                className={`px-3 py-2 text-right font-mono tabular-nums ${
                    positive ? "text-emerald-700" : "text-rose-700"
                }`}
            >
                {positive ? "+" : "−"}
                {formatMoney(Math.abs(row.amount_cents), cur)}
            </td>
            <td className="px-3 py-2 text-right font-mono tabular-nums text-slate-500">
                {formatMoney(row.balance_after_cents, cur)}
            </td>
            <td className="px-3 py-2 text-right text-slate-500 tabular-nums">
                {formatDate(row.created_at)}
            </td>
        </tr>
    );
}

function LoadMore({
    query,
}: {
    query: {
        hasNextPage: boolean;
        isFetchingNextPage: boolean;
        fetchNextPage: () => void;
    };
}) {
    if (!query.hasNextPage) return null;
    return (
        <button
            type="button"
            onClick={() => query.fetchNextPage()}
            disabled={query.isFetchingNextPage}
            className="mt-2 inline-flex items-center gap-1.5 h-7 px-2.5 rounded-md border border-slate-200 hover:border-slate-300 text-[12px] text-slate-700 hover:text-slate-900 transition-colors disabled:opacity-60"
        >
            {query.isFetchingNextPage && <Loader2Icon className="w-3 h-3 animate-spin" />}
            Load more
        </button>
    );
}

// shortId masks an org id to a short, non-PII reference.
function shortId(id: string): string {
    if (!id) return "—";
    const trimmed = id.replace(/-/g, "");
    return `#${trimmed.slice(0, 6)}`;
}

function humanizeReason(reason: string): string {
    if (!reason) return "Adjustment";
    return reason
        .replace(/[_-]+/g, " ")
        .replace(/\b\w/g, (c) => c.toUpperCase());
}

// formatMoney renders minor units (cents) as a localized currency string.
function formatMoney(cents: number, currency: string): string {
    try {
        return new Intl.NumberFormat("en-US", {
            style: "currency",
            currency: (currency || "usd").toUpperCase(),
        }).format(cents / 100);
    } catch {
        return `${(cents / 100).toFixed(2)} ${(currency || "usd").toUpperCase()}`;
    }
}

function formatDate(value?: string): string {
    if (!value) return "—";
    const d = new Date(value);
    if (Number.isNaN(d.getTime())) return "—";
    return d.toLocaleDateString("en-US", {
        month: "short",
        day: "numeric",
        year: "numeric",
    });
}
