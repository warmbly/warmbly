// Agency > Billing: the two automatic top-ups, one for the send pool and one for the shared AI
// balance. Each says in a sentence what it will do, what one top-up costs, and the most it can
// charge in a month, before anyone turns it on.

import React from "react";
import { AlertTriangleIcon, CreditCardIcon, Loader2Icon } from "lucide-react";
import toast from "react-hot-toast/headless";

import { Toggle } from "@/components/app/campaigns/preferences/components/CampaignPreferenceBoolBox";
import { NumberInput } from "@/components/ui/field";
import {
    useAgencyAIAutoTopup,
    useAgencyAutoTopup,
    useSetAgencyAIAutoTopup,
    useSetAgencyAutoTopup,
} from "@/lib/api/hooks/app/agency/useAgency";
import type { AgencyAutoTopup } from "@/lib/api/models/app/agency/Agency";
import type { AppError } from "@/lib/api/client/normalizeError";
import buildError from "@/lib/helper/buildError";
import { fmtUsdTrim } from "@/lib/money";
import { cn } from "@/lib/utils";
import { formatNumber } from "./styles";

type Failure = NonNullable<AgencyAutoTopup["failure"]>;
type Card = NonNullable<AgencyAutoTopup["card"]>;

const FAILURES: Record<Failure, string> = {
    card_declined: "Your card was declined, so automatic top-up turned itself off and nothing was charged. Update the card under Invoices and payment, then turn it on again.",
    no_payment_method: "There was no saved card to charge, so automatic top-up turned itself off. Add a card under Invoices and payment, then turn it on again.",
};

function cardName(card: Card) {
    const brand = card.brand ? card.brand.charAt(0).toUpperCase() + card.brand.slice(1) : "Card";
    return card.last4 ? `${brand} ending ${card.last4}` : brand;
}

/** One setting: its question, the control, and a line under it. */
function Setting({ label, children, hint }: { label: string; children: React.ReactNode; hint?: string }) {
    return (
        <div>
            <div className="text-[12px] font-medium text-slate-700">{label}</div>
            <div className="mt-1.5 flex items-center gap-2">{children}</div>
            {hint && <p className="mt-1.5 text-[11.5px] leading-snug text-slate-500">{hint}</p>}
        </div>
    );
}

/** The two numbers that matter, side by side: what one top-up costs and the most a month can cost. */
function Costs({ each, eachNote, most, mostNote }: { each: string; eachNote: string; most: string; mostNote: string }) {
    return (
        <div className="grid grid-cols-1 sm:grid-cols-2 gap-px overflow-hidden rounded-md bg-slate-200 ring-1 ring-slate-200">
            {[
                { k: "Each top-up charges", v: each, n: eachNote },
                { k: "The most you can be charged in a month", v: most, n: mostNote },
            ].map((c) => (
                <div key={c.k} className="bg-slate-50 px-3 py-2.5">
                    <div className="text-[11px] text-slate-500">{c.k}</div>
                    <div className="mt-0.5 text-[16px] font-semibold tracking-tight tabular-nums text-slate-900">{c.v}</div>
                    <div className="text-[11.5px] text-slate-500">{c.n}</div>
                </div>
            ))}
        </div>
    );
}

function Panel({
    name,
    summary,
    enabled,
    loading,
    canToggle,
    onToggle,
    failure,
    card,
    used,
    children,
}: {
    /** Names the switch for assistive technology. */
    name: string;
    /** One sentence saying what happens now, on or off. */
    summary: string;
    enabled: boolean;
    loading: boolean;
    canToggle: boolean;
    onToggle: (on: boolean) => void;
    failure?: Failure;
    card?: Card;
    /** What this month has used of its limit, shown while on. */
    used?: string;
    children: React.ReactNode;
}) {
    return (
        <div className={cn("rounded-lg border", enabled ? "border-sky-200" : "border-slate-200")}>
            <div className="px-4 py-3 flex items-start gap-3">
                <div className="min-w-0 flex-1">
                    <div className="flex items-center gap-2">
                        <span className="text-[12.5px] font-medium text-slate-900">Automatic top-up</span>
                        <span
                            className={cn(
                                "h-[18px] px-1.5 rounded text-[10.5px] font-medium inline-flex items-center",
                                enabled ? "bg-sky-50 text-sky-700" : "bg-slate-100 text-slate-500",
                            )}
                        >
                            {enabled ? "On" : "Off"}
                        </span>
                    </div>
                    <p className="mt-0.5 text-[12px] leading-relaxed text-slate-600">{summary}</p>
                </div>
                {loading ? (
                    <Loader2Icon className="mt-0.5 w-3.5 h-3.5 animate-spin text-slate-300" />
                ) : (
                    <Toggle value={enabled} onChange={onToggle} disabled={!canToggle} ariaLabel={name} />
                )}
            </div>

            {failure && !enabled && (
                <p className="mx-4 mb-3 flex gap-2 rounded-md bg-amber-50 px-3 py-2 text-[11.5px] leading-relaxed text-amber-800">
                    <AlertTriangleIcon className="mt-px w-3.5 h-3.5 shrink-0" />
                    {FAILURES[failure]}
                </p>
            )}

            <div className="border-t border-slate-200 px-4 py-3.5 space-y-3.5">
                {children}
                <p className="flex items-start gap-1.5 text-[11.5px] leading-relaxed text-slate-500">
                    <CreditCardIcon className="mt-px w-3.5 h-3.5 text-slate-400 shrink-0" />
                    <span>
                        {card
                            ? `Charged to ${cardName(card)}, the card your plan is paid with. You get an invoice and a notification for each top-up.`
                            : "You have no saved card yet. Automatic top-up charges the card your plan is paid with, so it can be turned on once a plan is bought with a card."}
                        {enabled && used && ` ${used}`}
                    </span>
                </p>
            </div>
        </div>
    );
}

/** Automatic top-up of the send pool: a pack bought when few sends are left. */
export function SendsAutoTopup({ agencyId, canManage }: { agencyId: string; canManage: boolean }) {
    const query = useAgencyAutoTopup(agencyId);
    const save = useSetAgencyAutoTopup(agencyId);
    const data = query.data;
    // Typed freely and saved when the field is left, so a half-typed number is never sent.
    const [threshold, setThreshold] = React.useState<number | null>(null);
    const [packs, setPacks] = React.useState<number | null>(null);
    if (query.isPending) return <Panel name="Automatic top-up of sends" summary="Loading…" enabled={false} loading canToggle={false} onToggle={() => {}}>{null}</Panel>;
    if (!data) return null;

    const at = threshold ?? data.threshold_sends;
    const max = packs ?? data.max_packs;
    const price = data.pack_price_cents / 100;
    const pack = formatNumber(data.pack_sends);

    const apply = async (input: { enabled: boolean; threshold_sends: number; max_packs: number }) => {
        try {
            const before = data.bought_this_period;
            const res = await save.mutateAsync(input);
            setThreshold(null);
            setPacks(null);
            if (res.bought_this_period > before) toast.success(`${pack} sends were added and ${fmtUsdTrim(price)} charged, because fewer than ${formatNumber(res.threshold_sends)} were left`);
            else if (input.enabled && !data.enabled) toast.success("Automatic top-up is on");
        } catch (e) {
            toast.error(buildError(e as AppError));
        }
    };

    return (
        <Panel
            name="Automatic top-up of sends"
            summary={
                data.enabled
                    ? `When fewer than ${formatNumber(at)} sends are left, we add ${pack} more and charge ${fmtUsdTrim(price)}. This can happen up to ${max} ${max === 1 ? "time" : "times"} a month.`
                    : `Turn this on and we add ${pack} sends for ${fmtUsdTrim(price)} whenever you run low, so campaigns never pause. Until then you buy packs yourself.`
            }
            enabled={data.enabled}
            loading={false}
            canToggle={canManage && !save.isPending && data.available}
            onToggle={(enabled) => void apply({ enabled, threshold_sends: at, max_packs: max })}
            failure={data.failure}
            card={data.card}
            used={`This month: ${data.bought_this_period} of ${data.max_packs} used.`}
        >
            <div className="grid grid-cols-1 sm:grid-cols-2 gap-x-6 gap-y-3.5">
                <Setting label="Top up when this few sends are left" hint="Counted across all your clients for the month.">
                    <span className="block w-44">
                        <NumberInput
                            value={at}
                            onChange={setThreshold}
                            onCommit={(v) => {
                                if (data.enabled && v !== data.threshold_sends) void apply({ enabled: true, threshold_sends: v, max_packs: max });
                            }}
                            min={0}
                            max={10_000_000}
                            step={1000}
                            suffix="sends"
                            disabled={!canManage || save.isPending}
                        />
                    </span>
                </Setting>
                <Setting label="Stop after this many top-ups in a month" hint="A safety limit. After it, you are told and campaigns pause when sends run out.">
                    <span className="block w-44">
                        <NumberInput
                            value={max}
                            onChange={setPacks}
                            onCommit={(v) => {
                                if (data.enabled && v !== data.max_packs) void apply({ enabled: true, threshold_sends: at, max_packs: v });
                            }}
                            min={1}
                            max={20}
                            suffix={max === 1 ? "top-up" : "top-ups"}
                            disabled={!canManage || save.isPending}
                        />
                    </span>
                </Setting>
            </div>
            <Costs each={fmtUsdTrim(price)} eachNote={`for ${pack} sends`} most={fmtUsdTrim(max * price)} mostNote={`${max} × ${fmtUsdTrim(price)}, plus tax where it applies`} />
        </Panel>
    );
}

/** Automatic top-up of the shared AI balance: an amount added when little is left. */
export function AIAutoTopup({ agencyId, canManage }: { agencyId: string; canManage: boolean }) {
    const query = useAgencyAIAutoTopup(agencyId);
    const save = useSetAgencyAIAutoTopup(agencyId);
    const data = query.data;
    const [threshold, setThreshold] = React.useState<number | null>(null);
    const [amount, setAmount] = React.useState<number | null>(null);
    const [limit, setLimit] = React.useState<number | null>(null);
    if (query.isPending) return <Panel name="Automatic top-up of AI balance" summary="Loading…" enabled={false} loading canToggle={false} onToggle={() => {}}>{null}</Panel>;
    if (!data) return null;

    const at = threshold ?? data.threshold_usd;
    const each = amount ?? data.amount_usd;
    const most = limit ?? data.limit_usd;
    const times = each > 0 ? Math.floor(most / each) : 0;
    const problem =
        each < data.min_usd || each > data.max_usd
            ? `Each top-up must be between ${fmtUsdTrim(data.min_usd)} and ${fmtUsdTrim(data.max_usd)}.`
            : most < each
              ? "The monthly limit must be at least one top-up."
              : null;

    const apply = async (input: { enabled: boolean; threshold_usd: number; amount_usd: number; limit_usd: number }) => {
        try {
            const before = data.added_this_period_usd;
            const res = await save.mutateAsync(input);
            setThreshold(null);
            setAmount(null);
            setLimit(null);
            if (res.added_this_period_usd > before) toast.success(`${fmtUsdTrim(res.amount_usd)} was added and charged, because less than ${fmtUsdTrim(res.threshold_usd)} was left`);
            else if (input.enabled && !data.enabled) toast.success("Automatic top-up is on");
        } catch (e) {
            toast.error(buildError(e as AppError));
        }
    };
    const commit = (patch: Partial<{ threshold_usd: number; amount_usd: number; limit_usd: number }>) => {
        const next = { enabled: true, threshold_usd: at, amount_usd: each, limit_usd: most, ...patch };
        const changed = next.threshold_usd !== data.threshold_usd || next.amount_usd !== data.amount_usd || next.limit_usd !== data.limit_usd;
        if (data.enabled && changed && next.amount_usd >= data.min_usd && next.amount_usd <= data.max_usd && next.limit_usd >= next.amount_usd) void apply(next);
    };

    return (
        <Panel
            name="Automatic top-up of AI balance"
            summary={
                data.enabled
                    ? `When less than ${fmtUsdTrim(at)} is left in the shared AI balance, we add ${fmtUsdTrim(each)} and charge your card. We stop once ${fmtUsdTrim(most)} has been added in a month.`
                    : `Turn this on and we add ${fmtUsdTrim(each)} of AI balance whenever it runs low, so AI features never stop for your clients. Until then you add balance yourself.`
            }
            enabled={data.enabled}
            loading={false}
            canToggle={canManage && !save.isPending && data.available && (data.enabled || !problem)}
            onToggle={(enabled) => void apply({ enabled, threshold_usd: at, amount_usd: each, limit_usd: most })}
            failure={data.failure}
            card={data.card}
            used={`This month: ${fmtUsdTrim(data.added_this_period_usd)} of ${fmtUsdTrim(data.limit_usd)} used.`}
        >
            <div className="grid grid-cols-1 sm:grid-cols-3 gap-x-6 gap-y-3.5">
                <Setting label="Top up when less than this is left">
                    <span className="block w-36">
                        <NumberInput value={at} onChange={setThreshold} onCommit={(v) => commit({ threshold_usd: v })} min={0} max={data.max_usd} step={5} suffix="USD" disabled={!canManage || save.isPending} />
                    </span>
                </Setting>
                <Setting label="Add this much each time">
                    <span className="block w-36">
                        <NumberInput value={each} onChange={setAmount} onCommit={(v) => commit({ amount_usd: v })} min={data.min_usd} max={data.max_usd} step={5} suffix="USD" disabled={!canManage || save.isPending} />
                    </span>
                </Setting>
                <Setting label="Never add more than this in a month">
                    <span className="block w-36">
                        <NumberInput value={most} onChange={setLimit} onCommit={(v) => commit({ limit_usd: v })} min={data.min_usd} max={50_000} step={25} suffix="USD" disabled={!canManage || save.isPending} />
                    </span>
                </Setting>
            </div>
            {problem && <p className="text-[11.5px] text-red-600">{problem}</p>}
            <Costs
                each={fmtUsdTrim(each)}
                eachNote="added to the shared AI balance, dollar for dollar"
                most={fmtUsdTrim(times * each)}
                mostNote={`up to ${times} ${times === 1 ? "top-up" : "top-ups"} of ${fmtUsdTrim(each)}, plus tax where it applies`}
            />
        </Panel>
    );
}
