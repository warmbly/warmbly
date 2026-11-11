// Agency > Billing: what extra sends cost, the automatic top-up that buys them before the pool
// runs out, and what happens when it does.

import { Loader2Icon, PlusIcon } from "lucide-react";

import type { AgencyUsage } from "@/lib/api/models/app/agency/Agency";
import { fmtUsdTrim } from "@/lib/money";
import { SendsAutoTopup } from "./AutoTopup";
import { formatNumber } from "./styles";

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
    const price = fmtUsdTrim(usage.topup_price_cents / 100);
    const pack = formatNumber(usage.topup_sends);
    return (
        <div className="border-b border-slate-200 px-5 py-4 space-y-4">
            <div className="flex flex-wrap items-center gap-3">
                <div className="flex-1 min-w-[220px]">
                    <div className="text-[13px] font-medium text-slate-900">
                        {pack} extra sends cost {price}
                    </div>
                    <div className="mt-0.5 text-[12px] leading-relaxed text-slate-500">
                        You pay {price} once, {pack} sends are added to this month's pool for all your clients, and any left over are gone when the
                        month renews.
                        {usage.topups > 0 && ` ${formatNumber(usage.topups)} added so far this month.`}
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
                        Buy {pack} sends for {price}
                    </button>
                )}
            </div>

            <SendsAutoTopup agencyId={agencyId} canManage={canManage} />

            <p className="text-[11.5px] leading-relaxed text-slate-500">
                <span className="font-medium text-slate-700">If sends run out:</span> campaigns across your clients pause until the month renews or
                sends are added. Warmup keeps running, and nothing is charged without a pack or automatic top-up. Owners and admins are told
                when 80% is used and again at 100%.
            </p>
        </div>
    );
}
