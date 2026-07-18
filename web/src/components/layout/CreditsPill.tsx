// CreditsPill — the org's remaining AI credit balance in the top header,
// beside the plan badge. Amber under the org's low-balance threshold, red at
// zero; clicking opens Settings → Billing where the usage overview and spend
// controls live. Rendered only for members who can see billing (the balance
// endpoint is manage_billing-gated), and refreshed by the realtime spine on
// purchases, resets, and the low-credit alert.

import { Link } from "react-router-dom";
import { SparklesIcon } from "lucide-react";
import useCredits from "@/lib/api/hooks/app/subscription/useCredits";
import { useCreditSettings } from "@/lib/api/hooks/app/subscription/useCreditSettings";
import { usePermission } from "@/hooks/usePermission";
import { cn } from "@/lib/utils";

export function CreditsPill() {
    const canSee = usePermission("MANAGE_BILLING");
    const credits = useCredits();
    const settings = useCreditSettings();
    if (!canSee || credits.isPending || !credits.data) return null;

    const total = credits.data.balance;
    const threshold = settings.data?.low_balance_threshold ?? 25;
    const empty = total <= 0;
    const low = !empty && total <= threshold;

    return (
        <Link
            to="/app/settings/billing"
            title={`${total.toLocaleString()} AI credits left — open usage & billing`}
            className={cn(
                "inline-flex items-center gap-1.5 h-6 px-2 rounded border text-[11px] font-semibold tabular-nums transition-colors hover:brightness-105",
                empty
                    ? "bg-red-50 text-red-700 border-red-100"
                    : low
                      ? "bg-amber-50 text-amber-700 border-amber-100"
                      : "bg-sky-50 text-sky-700 border-sky-100",
            )}
        >
            <SparklesIcon className="w-2.5 h-2.5" />
            {formatCredits(total)}
        </Link>
    );
}

// formatCredits keeps the pill compact: 843, 12.4k, 1.2M.
function formatCredits(n: number): string {
    if (n >= 1_000_000) return `${(n / 1_000_000).toFixed(1).replace(/\.0$/, "")}M`;
    if (n >= 10_000) return `${(n / 1_000).toFixed(1).replace(/\.0$/, "")}k`;
    return n.toLocaleString();
}
