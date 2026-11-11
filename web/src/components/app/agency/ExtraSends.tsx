// Agency > Billing: what extra sends cost, what happens when the pool runs out, and the
// automatic top-up that buys a pack on the saved card before it does.

import React from "react";
import { AlertTriangleIcon, CreditCardIcon, Loader2Icon, PlusIcon } from "lucide-react";
import toast from "react-hot-toast/headless";

import { Toggle } from "@/components/app/campaigns/preferences/components/CampaignPreferenceBoolBox";
import { NumberInput } from "@/components/ui/field";
import { useAgencyAutoTopup, useSetAgencyAutoTopup } from "@/lib/api/hooks/app/agency/useAgency";
import type { AgencyAutoTopup, AgencyUsage } from "@/lib/api/models/app/agency/Agency";
import type { AppError } from "@/lib/api/client/normalizeError";
import buildError from "@/lib/helper/buildError";
import { fmtUsdTrim } from "@/lib/money";
import { formatNumber } from "./styles";

const FAILURES: Record<NonNullable<AgencyAutoTopup["failure"]>, string> = {
    card_declined: "The card on file was declined, so automatic top-up turned itself off. Update the card under Invoices and payment, then turn it on again.",
    no_payment_method: "There was no saved card to charge, so automatic top-up turned itself off. Add a card under Invoices and payment, then turn it on again.",
};

function cardName(card: NonNullable<AgencyAutoTopup["card"]>) {
    const brand = card.brand ? card.brand.charAt(0).toUpperCase() + card.brand.slice(1) : "Card";
    return card.last4 ? `${brand} ending ${card.last4}` : brand;
}

export default function ExtraSends({
    agencyId,
    usage,
    canManage,
    buying,
    onBuy,
}: {
    agencyId: string;
    usage: AgencyUsage;
    canManage: boolean;
    /** A pack's checkout is opening. */
    buying: boolean;
    onBuy: () => void;
}) {
    const auto = useAgencyAutoTopup(agencyId);
    const save = useSetAgencyAutoTopup(agencyId);
    const data = auto.data;
    const price = usage.topup_price_cents / 100;

    // The two limits are typed freely and saved when the field is left, so a half-typed number is never sent.
    const [threshold, setThreshold] = React.useState<number | null>(null);
    const [maxPacks, setMaxPacks] = React.useState<number | null>(null);
    const shownThreshold = threshold ?? data?.threshold_sends ?? 10_000;
    const shownMax = maxPacks ?? data?.max_packs ?? 2;

    const apply = async (input: { enabled: boolean; threshold_sends: number; max_packs: number }) => {
        try {
            const before = data?.bought_this_period ?? 0;
            const res = await save.mutateAsync(input);
            setThreshold(null);
            setMaxPacks(null);
            if (res.bought_this_period > before) toast.success(`${formatNumber(res.pack_sends)} sends were added now, because the pool was already low`);
            else if (input.enabled && !data?.enabled) toast.success("Automatic top-up is on");
        } catch (e) {
            toast.error(buildError(e as AppError));
        }
    };

    return (
        <div className="border-b border-slate-200">
            <div className="px-5 py-4 flex flex-wrap items-center gap-3">
                <div className="flex-1 min-w-[220px]">
                    <div className="text-[12.5px] font-medium text-slate-900">
                        {formatNumber(usage.topup_sends)} more sends for {fmtUsdTrim(price)}
                    </div>
                    <div className="mt-0.5 text-[11.5px] leading-relaxed text-slate-500">
                        A one-off pack, added to this period's pool and gone when the period renews.
                        {usage.topups > 0 && ` ${formatNumber(usage.topups)} added this period.`}
                    </div>
                </div>
                {canManage && (
                    <button
                        type="button"
                        onClick={onBuy}
                        disabled={buying}
                        className="h-7 px-2.5 rounded-md bg-sky-600 hover:bg-sky-700 text-white text-[12px] font-medium inline-flex items-center gap-1.5 disabled:opacity-60"
                    >
                        {buying ? <Loader2Icon className="w-3 h-3 animate-spin" /> : <PlusIcon className="w-3 h-3" />}
                        Buy a pack
                    </button>
                )}
            </div>

            <div className="mx-5 mb-4 rounded-lg border border-slate-200">
                <div className="px-4 py-3 flex items-start gap-3">
                    <div className="min-w-0 flex-1">
                        <div className="text-[12.5px] font-medium text-slate-900">Automatic top-up</div>
                        <p className="mt-0.5 text-[11.5px] leading-relaxed text-slate-500">
                            Buys a pack on your saved card when the pool runs low, so campaigns never pause for lack of sends.
                        </p>
                    </div>
                    {auto.isPending ? (
                        <Loader2Icon className="mt-0.5 w-3.5 h-3.5 animate-spin text-slate-300" />
                    ) : (
                        data && (
                            <Toggle
                                value={data.enabled}
                                onChange={(enabled) => void apply({ enabled, threshold_sends: shownThreshold, max_packs: shownMax })}
                                disabled={!canManage || save.isPending || !data.available}
                                ariaLabel="Automatic top-up"
                            />
                        )
                    )}
                </div>

                {data?.failure && !data.enabled && (
                    <p className="mx-4 mb-3 flex gap-2 rounded-md bg-amber-50 px-3 py-2 text-[11.5px] leading-relaxed text-amber-800">
                        <AlertTriangleIcon className="mt-px w-3.5 h-3.5 shrink-0" />
                        {FAILURES[data.failure]}
                    </p>
                )}

                {data && (
                    <div className="border-t border-slate-200 px-4 py-3 grid grid-cols-1 sm:grid-cols-2 gap-x-6 gap-y-3">
                        <label className="block">
                            <span className="block text-[12px] font-medium text-slate-700">Buy a pack when this many sends are left</span>
                            <span className="mt-1.5 block w-40">
                                <NumberInput
                                    value={shownThreshold}
                                    onChange={setThreshold}
                                    onCommit={(v) => {
                                        if (data.enabled && v !== data.threshold_sends) void apply({ enabled: true, threshold_sends: v, max_packs: shownMax });
                                    }}
                                    min={0}
                                    max={10_000_000}
                                    step={1000}
                                    disabled={!canManage || save.isPending}
                                />
                            </span>
                        </label>
                        <label className="block">
                            <span className="block text-[12px] font-medium text-slate-700">At most this many packs a period</span>
                            <span className="mt-1.5 flex items-center gap-2">
                                <span className="block w-24">
                                    <NumberInput
                                        value={shownMax}
                                        onChange={setMaxPacks}
                                        onCommit={(v) => {
                                            if (data.enabled && v !== data.max_packs) void apply({ enabled: true, threshold_sends: shownThreshold, max_packs: v });
                                        }}
                                        min={1}
                                        max={20}
                                        disabled={!canManage || save.isPending}
                                    />
                                </span>
                                <span className="text-[11.5px] text-slate-500 tabular-nums">up to {fmtUsdTrim(shownMax * price)} a period</span>
                            </span>
                        </label>
                        <p className="sm:col-span-2 flex items-center gap-1.5 text-[11.5px] text-slate-500">
                            <CreditCardIcon className="w-3.5 h-3.5 text-slate-400 shrink-0" />
                            {data.card
                                ? `Charged to ${cardName(data.card)}, with an invoice for each pack.`
                                : "No saved card yet. The card your subscription is paid with is the one charged."}
                            {data.enabled && ` ${data.bought_this_period} of ${data.max_packs} bought this period.`}
                        </p>
                    </div>
                )}
            </div>

            <p className="px-5 pb-4 text-[11.5px] leading-relaxed text-slate-500">
                When the pool is used up, campaigns across your clients pause until the period renews or sends are added. Warmup keeps
                running. Owners and admins are told at 80% and at 100%.
            </p>
        </div>
    );
}
