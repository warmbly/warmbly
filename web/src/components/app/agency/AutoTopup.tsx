// Agency > Billing: the two automatic top-ups, one for the send pool and one for the shared AI
// balance. Each says in a sentence what it will do, what one top-up costs, and the most it can
// charge in a month, before anyone turns it on.

import React from "react";
import { AlertTriangleIcon, CreditCardIcon, Loader2Icon } from "lucide-react";
import toast from "react-hot-toast/headless";

import { Toggle } from "@/components/app/campaigns/preferences/components/CampaignPreferenceBoolBox";
import { Label, NumberInput } from "@/components/ui/field";
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

/** One setting: its question, naming the field with the given id, the control, and a line under it. */
function Setting({ label, htmlFor, children, hint }: { label: string; htmlFor: string; children: React.ReactNode; hint?: string }) {
    return (
        <div>
            <Label htmlFor={htmlFor} className="mb-0 text-[12px] normal-case tracking-normal text-slate-700">
                {label}
            </Label>
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
    dirty,
    saving,
    problem,
    onSave,
    onDiscard,
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
    /** The numbers differ from what is saved. Nothing is sent, and nothing can be charged, until Save. */
    dirty: boolean;
    saving: boolean;
    /** Why the numbers cannot be saved as they stand. */
    problem?: string | null;
    onSave: () => void;
    onDiscard: () => void;
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
                {problem && <p className="text-[11.5px] text-red-600">{problem}</p>}
                {dirty && (
                    <div className="flex flex-wrap items-center gap-2 rounded-md bg-slate-50 px-3 py-2">
                        <p className="min-w-[200px] flex-1 text-[11.5px] leading-snug text-slate-600">
                            {enabled
                                ? "Not saved yet. If you are already below the new level when you save, one top-up is charged right away."
                                : "Not saved yet. Nothing is charged while automatic top-up is off."}
                        </p>
                        <button
                            type="button"
                            onClick={onDiscard}
                            disabled={saving}
                            className="h-7 px-2.5 rounded-md text-[12px] text-slate-600 hover:bg-slate-100 disabled:opacity-60"
                        >
                            Discard
                        </button>
                        <button
                            type="button"
                            onClick={onSave}
                            disabled={saving || !!problem}
                            className="h-7 px-2.5 rounded-md bg-sky-600 hover:bg-sky-700 text-white text-[12px] font-medium inline-flex items-center gap-1.5 disabled:opacity-60"
                        >
                            {saving && <Loader2Icon className="w-3 h-3 animate-spin" />}
                            Save changes
                        </button>
                    </div>
                )}
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
    // Edited freely and sent only by Save or the switch, so a half-made change is never acted on.
    const [threshold, setThreshold] = React.useState<number | null>(null);
    const [packs, setPacks] = React.useState<number | null>(null);
    if (query.isPending) return <Panel name="Automatic top-up of sends" summary="Loading…" enabled={false} loading canToggle={false} onToggle={() => {}} dirty={false} saving={false} onSave={() => {}} onDiscard={() => {}}>{null}</Panel>;
    if (!data) return null;

    const at = threshold ?? data.threshold_sends;
    const max = packs ?? data.max_packs;
    const price = data.pack_price_cents / 100;
    const pack = formatNumber(data.pack_sends);
    const dirty = at !== data.threshold_sends || max !== data.max_packs;
    const discard = () => {
        setThreshold(null);
        setPacks(null);
    };

    // The numbers travel with the switch, so both are saved as one setting.
    const apply = async (enabled: boolean) => {
        try {
            const before = data.bought_this_period;
            const res = await save.mutateAsync({ enabled, threshold_sends: at, max_packs: max });
            discard();
            if (enabled && !res.enabled) toast.error(res.failure ? FAILURES[res.failure] : "Automatic top-up could not be turned on.");
            else if (res.bought_this_period > before) toast.success(`${pack} sends were added and ${fmtUsdTrim(price)} charged, because fewer than ${formatNumber(res.threshold_sends)} were left`);
            else if (enabled !== data.enabled) toast.success(enabled ? "Automatic top-up is on" : "Automatic top-up is off");
            else toast.success("Saved");
        } catch (e) {
            toast.error(buildError(e as AppError));
        }
    };

    return (
        <Panel
            name="Automatic top-up of sends"
            summary={
                data.enabled
                    ? `When fewer than ${formatNumber(data.threshold_sends)} sends are left, we add ${pack} more and charge ${fmtUsdTrim(price)}. This can happen up to ${data.max_packs} ${data.max_packs === 1 ? "time" : "times"} a month.`
                    : `Turn this on and we add ${pack} sends for ${fmtUsdTrim(price)} whenever you run low, so campaigns never pause. Until then you buy packs yourself.`
            }
            enabled={data.enabled}
            loading={false}
            // Turning it off always works; turning it on needs a pool to top up.
            canToggle={canManage && !save.isPending && (data.enabled || data.available)}
            onToggle={(enabled) => void apply(enabled)}
            failure={data.failure}
            card={data.card}
            used={`This month: ${data.bought_this_period} of ${data.max_packs} used.`}
            dirty={dirty}
            saving={save.isPending}
            onSave={() => void apply(data.enabled)}
            onDiscard={discard}
        >
            <div className="grid grid-cols-1 sm:grid-cols-2 gap-x-6 gap-y-3.5">
                <Setting label="Top up when this few sends are left" htmlFor="sends-topup-threshold" hint="Counted across all your clients for the month.">
                    <span className="block w-44">
                        <NumberInput id="sends-topup-threshold" value={at} onChange={setThreshold} min={0} max={10_000_000} step={1000} suffix="sends" disabled={!canManage || save.isPending} />
                    </span>
                </Setting>
                <Setting
                    label="Stop after this many top-ups in a month"
                    htmlFor="sends-topup-max"
                    hint="A safety limit. After it, you are told and campaigns pause when sends run out."
                >
                    <span className="block w-44">
                        <NumberInput id="sends-topup-max" value={max} onChange={setPacks} min={1} max={20} suffix={max === 1 ? "top-up" : "top-ups"} disabled={!canManage || save.isPending} />
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
    if (query.isPending) return <Panel name="Automatic top-up of AI balance" summary="Loading…" enabled={false} loading canToggle={false} onToggle={() => {}} dirty={false} saving={false} onSave={() => {}} onDiscard={() => {}}>{null}</Panel>;
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

    const dirty = at !== data.threshold_usd || each !== data.amount_usd || most !== data.limit_usd;
    const discard = () => {
        setThreshold(null);
        setAmount(null);
        setLimit(null);
    };

    // All three numbers are saved together, so a top-up is never charged from a half-made change.
    const apply = async (enabled: boolean) => {
        try {
            const before = data.added_this_period_usd;
            const res = await save.mutateAsync({ enabled, threshold_usd: at, amount_usd: each, limit_usd: most });
            discard();
            if (enabled && !res.enabled) toast.error(res.failure ? FAILURES[res.failure] : "Automatic top-up could not be turned on.");
            else if (res.added_this_period_usd > before) toast.success(`${fmtUsdTrim(res.amount_usd)} was added and charged, because less than ${fmtUsdTrim(res.threshold_usd)} was left`);
            else if (enabled !== data.enabled) toast.success(enabled ? "Automatic top-up is on" : "Automatic top-up is off");
            else toast.success("Saved");
        } catch (e) {
            toast.error(buildError(e as AppError));
        }
    };

    return (
        <Panel
            name="Automatic top-up of AI balance"
            summary={
                data.enabled
                    ? `When less than ${fmtUsdTrim(data.threshold_usd)} is left in the shared AI balance, we add ${fmtUsdTrim(data.amount_usd)} and charge your card. We stop once ${fmtUsdTrim(data.limit_usd)} has been added in a month.`
                    : `Turn this on and we add ${fmtUsdTrim(each)} of AI balance whenever it runs low, so AI features never stop for your clients. Until then you add balance yourself.`
            }
            enabled={data.enabled}
            loading={false}
            // Turning it off always works; turning it on needs a balance to add to and numbers that can be saved.
            canToggle={canManage && !save.isPending && (data.enabled || (data.available && !problem))}
            onToggle={(enabled) => void apply(enabled)}
            failure={data.failure}
            card={data.card}
            used={`This month: ${fmtUsdTrim(data.added_this_period_usd)} of ${fmtUsdTrim(data.limit_usd)} used.`}
            dirty={dirty}
            saving={save.isPending}
            problem={problem}
            onSave={() => void apply(data.enabled)}
            onDiscard={discard}
        >
            <div className="grid grid-cols-1 sm:grid-cols-3 gap-x-6 gap-y-3.5">
                <Setting label="Top up when less than this is left" htmlFor="ai-topup-threshold">
                    <span className="block w-36">
                        <NumberInput id="ai-topup-threshold" value={at} onChange={setThreshold} min={0} max={data.max_usd} step={5} suffix="USD" disabled={!canManage || save.isPending} />
                    </span>
                </Setting>
                <Setting label="Add this much each time" htmlFor="ai-topup-amount">
                    <span className="block w-36">
                        <NumberInput id="ai-topup-amount" value={each} onChange={setAmount} min={data.min_usd} max={data.max_usd} step={5} suffix="USD" disabled={!canManage || save.isPending} />
                    </span>
                </Setting>
                <Setting label="Never add more than this in a month" htmlFor="ai-topup-limit">
                    <span className="block w-36">
                        <NumberInput id="ai-topup-limit" value={most} onChange={setLimit} min={data.min_usd} max={50_000} step={25} suffix="USD" disabled={!canManage || save.isPending} />
                    </span>
                </Setting>
            </div>
            <Costs
                each={fmtUsdTrim(each)}
                eachNote="added to the shared AI balance, dollar for dollar"
                most={fmtUsdTrim(times * each)}
                mostNote={`up to ${times} ${times === 1 ? "top-up" : "top-ups"} of ${fmtUsdTrim(each)}, plus tax where it applies`}
            />
        </Panel>
    );
}
