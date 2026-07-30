// The Advisor's only surface: a strip that renders on the page where the
// problem is. Scope it to an entity (this campaign, this mailbox) or to a whole
// dashboard area (everything wrong with deliverability).
//
// It renders nothing at all when there is nothing to say. That is the most
// important property here: a permanent "0 issues" panel trains people to stop
// looking at the space, so when something does appear it is already invisible.

import { useMemo, useState } from "react";
import { AnimatePresence, motion } from "framer-motion";
import { SparklesIcon } from "lucide-react";
import type {
    AdvisorCategory,
    AdvisorFinding,
    AdvisorSurface,
} from "@/lib/api/models/app/advisor/Advisor";
import { SEVERITY_RANK, groupFindings } from "@/lib/api/models/app/advisor/Advisor";
import { SURFACE_FETCH_LIMIT, useAdvisorFindings } from "@/lib/api/hooks/app/advisor/useAdvisor";
import AdvisorCard from "./AdvisorCard";
import AdvisorFixDrawer from "./AdvisorFixDrawer";
import AdvisorGroupCard from "./AdvisorGroupCard";

interface Props {
    surface?: AdvisorSurface;
    category?: AdvisorCategory;
    entityType?: string;
    entityId?: string;
    // limit caps how many cards show at once. On an entity page there are
    // rarely more than two or three; on a surface page the tail is noise.
    limit?: number;
    compact?: boolean;
    className?: string;
    // title overrides the default heading. Empty string hides the heading
    // entirely, for tight spaces like a detail drawer tab.
    title?: string;
}

export default function AdvisorStrip({
    surface,
    category,
    entityType,
    entityId,
    limit = 4,
    compact = false,
    className = "",
    title,
}: Props) {
    const [fixing, setFixing] = useState<AdvisorFinding | null>(null);

    // An entity-scoped strip must not fire before its id exists, or it renders
    // the whole org's findings for a beat while the page hydrates.
    const enabled = !entityType || Boolean(entityId);

    // A surface strip has to see the whole run before it can say "19
    // mailboxes"; fetching only what fits on screen would undercount every
    // group. Entity strips are already narrow, so they stay small.
    const fetchLimit = entityId ? limit + 4 : SURFACE_FETCH_LIMIT;

    const { data } = useAdvisorFindings(
        { surface, category, entityType, entityId, limit: fetchLimit },
        enabled,
    );

    const groups = useMemo(() => {
        const list = [...(data ?? [])].sort((a, b) => {
            // Applied cards stay visible until the next evaluation confirms
            // them, so "Fix" gives immediate, undoable feedback rather than a
            // card that just disappears. They sort last.
            if ((a.status === "applied") !== (b.status === "applied")) {
                return a.status === "applied" ? 1 : -1;
            }
            const rank = SEVERITY_RANK[b.severity] - SEVERITY_RANK[a.severity];
            return rank !== 0 ? rank : b.impact - a.impact;
        });

        // On an entity page every finding is already about that one thing, so
        // there is nothing to collapse and a "3 mailboxes" heading would be a
        // lie. Collapsing only makes sense across a whole surface.
        if (entityId) return list.map((f) => ({ key: f.id, lead: f, members: [f] }));
        return groupFindings(list).slice(0, limit);
    }, [data, limit, entityId]);

    if (groups.length === 0) return null;

    const heading = title ?? "Suggestions";

    return (
        <>
            <section className={className} aria-label="Advisor suggestions">
                {heading ? (
                    <div className="mb-1.5 flex items-center gap-1.5">
                        <SparklesIcon className="h-3 w-3 text-slate-400" />
                        <h2 className="text-[10px] font-medium uppercase tracking-[0.14em] text-slate-400">
                            {heading}
                        </h2>
                    </div>
                ) : null}

                <div className="space-y-1.5">
                    <AnimatePresence initial={false} mode="popLayout">
                        {groups.map((group) => (
                            <motion.div
                                key={group.key}
                                layout
                                initial={{ opacity: 0, y: -4 }}
                                animate={{ opacity: 1, y: 0 }}
                                exit={{ opacity: 0, height: 0, marginBottom: 0 }}
                                transition={{ duration: 0.16, ease: "easeOut" }}
                            >
                                {group.members.length > 1 ? (
                                    <AdvisorGroupCard group={group} onFix={setFixing} />
                                ) : (
                                    <AdvisorCard
                                        finding={group.lead}
                                        onFix={setFixing}
                                        compact={compact}
                                        defaultOpen={groups.length === 1 && group.lead.severity === "critical"}
                                    />
                                )}
                            </motion.div>
                        ))}
                    </AnimatePresence>
                </div>
            </section>

            <AnimatePresence>
                {fixing ? (
                    <AdvisorFixDrawer finding={fixing} onClose={() => setFixing(null)} />
                ) : null}
            </AnimatePresence>
        </>
    );
}
