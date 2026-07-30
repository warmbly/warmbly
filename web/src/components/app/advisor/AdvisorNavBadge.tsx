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
            // A translucent wash of the severity colour with the colour carried
            // by the text, not a filled pill. A solid badge in a light sidebar
            // shouts louder than a nav item should, and legibility comes from
            // the dark text rather than from the fill, so the background can
            // stay soft without the number getting hard to read.
            className={`inline-flex h-4 min-w-[16px] items-center justify-center rounded-full px-1 text-[10px] font-semibold tabular-nums ring-1 ${
                counts.critical > 0
                    ? "bg-rose-500/15 text-rose-600 ring-rose-500/25"
                    : "bg-orange-500/15 text-orange-600 ring-orange-500/25"
            }`}
        >
            {counts.urgent > 99 ? "99+" : counts.urgent}
        </motion.span>
    );
}
