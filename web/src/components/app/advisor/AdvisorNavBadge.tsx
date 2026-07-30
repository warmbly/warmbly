// The nav badge for one dashboard tab: how many things the Advisor has found
// that are worth interrupting you for, on the tab where the fix lives.
//
// It counts critical and high findings only. Badging a tab because a subject
// line is four characters too long is how a badge becomes wallpaper, and then
// the one that actually matters gets ignored with the rest.

import { useMemo } from "react";
import { motion } from "framer-motion";
import type { AdvisorSurface } from "@/lib/api/models/app/advisor/Advisor";
import { useAdvisorSummary } from "@/lib/api/hooks/app/advisor/useAdvisor";

export default function AdvisorNavBadge({ surface }: { surface: AdvisorSurface }) {
    const { data } = useAdvisorSummary();

    const counts = useMemo(() => {
        const entry = data?.surfaces?.find((s) => s.surface === surface);
        if (!entry) return null;
        const urgent = entry.critical + entry.high;
        return urgent > 0 ? { urgent, critical: entry.critical } : null;
    }, [data, surface]);

    if (!counts) return null;

    const label =
        counts.critical > 0
            ? `${counts.urgent} ${counts.urgent === 1 ? "issue" : "issues"} needing attention, ${counts.critical} critical`
            : `${counts.urgent} ${counts.urgent === 1 ? "issue" : "issues"} needing attention`;

    return (
        <motion.span
            initial={{ scale: 0.7, opacity: 0 }}
            animate={{ scale: 1, opacity: 1 }}
            transition={{ type: "spring", stiffness: 500, damping: 26 }}
            title={label}
            aria-label={label}
            // Solid and saturated, like every other indicator in this sidebar,
            // and the same orange as the high-severity dot on the row it points
            // at. A pale chip read as a disabled control next to them.
            className={`inline-flex h-4 min-w-[16px] items-center justify-center rounded-full px-1 text-[10px] font-medium tabular-nums text-white ${
                counts.critical > 0 ? "bg-rose-500" : "bg-orange-500"
            }`}
        >
            {counts.urgent > 99 ? "99+" : counts.urgent}
        </motion.span>
    );
}
