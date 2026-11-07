// A strip above the header when the balance is used up or running low and
// automatic reload will not refill it. Billing managers only; dismissed per
// workspace, state and cycle; hidden on the billing page, which says the same.
// Only on a paid plan, the only kind that can add money.

import React from "react";
import { Link, useRouterState } from "@tanstack/react-router";
import { WalletIcon, XIcon } from "lucide-react";
import { usePermission } from "@/hooks/usePermission";
import useFeatureAccess from "@/hooks/useFeatureAccess";
import { useBalance } from "@/lib/api/hooks/app/subscription/useBalance";
import { fmtUsd } from "@/lib/money";
import { cn } from "@/lib/utils";
import { useAppStore } from "@/stores";
import { balanceBanner } from "./balanceModel";

function wasDismissed(key: string) {
    try {
        return localStorage.getItem(key) === "1";
    } catch {
        return false;
    }
}

export default function BalanceBanner() {
    const canManage = usePermission("MANAGE_BILLING");
    // Money can be added on a paid plan only; a banner nobody can act on is noise.
    const { billing, paid } = useFeatureAccess();
    const metered = billing && paid;
    const orgId = useAppStore((s) => s.currentOrganization?.id);
    const onBilling = useRouterState({ select: (s) => s.location.pathname.startsWith("/app/settings/billing") });
    const { data: b } = useBalance(canManage && metered);
    const [dismissed, setDismissed] = React.useState<string | null>(null);

    const kind = canManage && metered && b?.can_add_money !== false ? balanceBanner(b) : null;
    if (!kind || !b || onBilling) return null;
    const period = b.next_reset_at ? new Date(b.next_reset_at).toISOString().slice(0, 10) : new Date().toISOString().slice(0, 7);
    const key = `warmbly.balance-banner:${orgId}:${kind}:${period}`;
    if (dismissed === key || wasDismissed(key)) return null;

    const ar = b.auto_reload;
    const dismiss = () => {
        setDismissed(key);
        try {
            localStorage.setItem(key, "1");
        } catch {
            /* private mode: hidden until reload */
        }
    };
    const out = kind === "out";

    return (
        <div
            role="status"
            className={cn(
                "flex flex-wrap items-center gap-x-3 gap-y-1.5 border-b px-4 py-2",
                out ? "bg-amber-50 border-amber-200" : "bg-sky-50 border-sky-200",
            )}
        >
            <WalletIcon className={cn("w-3.5 h-3.5 shrink-0", out ? "text-amber-600" : "text-sky-600")} aria-hidden />
            <p className="min-w-0 flex-1 text-[12.5px] leading-[18px] text-slate-700">
                <span className="font-medium text-slate-900">{out ? "The balance is used up." : "The balance is running low."}</span>{" "}
                {out ? "AI features are paused until money is added." : `${fmtUsd(b.balance_usd)} left. AI features pause when it runs out.`}
                {ar?.failure_code && !ar.enabled ? " Automatic reload turned off after a failed payment." : ""}
            </p>
            <div className="flex shrink-0 items-center gap-1.5">
                <Link
                    to="/app/settings/billing/{-$tab}"
                    params={{ tab: "balance" }}
                    className="h-7 px-2.5 rounded-md bg-sky-600 hover:bg-sky-700 text-white text-[12px] font-medium inline-flex items-center transition-colors"
                >
                    Add money
                </Link>
                {ar && (
                    <Link
                        to="/app/settings/billing/{-$tab}"
                        params={{ tab: "balance" }}
                        hash="auto-reload"
                        className="h-7 px-2.5 rounded-md border border-slate-200 bg-white hover:border-slate-300 text-[12px] text-slate-700 hover:text-slate-900 inline-flex items-center transition-colors"
                    >
                        {ar.enabled ? "Reload settings" : "Turn on automatic reload"}
                    </Link>
                )}
                <button
                    type="button"
                    onClick={dismiss}
                    aria-label="Dismiss"
                    className="size-7 rounded-md text-slate-400 hover:text-slate-900 hover:bg-white/70 inline-flex items-center justify-center transition-colors"
                >
                    <XIcon className="w-3.5 h-3.5" />
                </button>
            </div>
        </div>
    );
}
