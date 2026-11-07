import { useEffect, useMemo, useRef, useState } from "react";
import { AnimatePresence, motion } from "framer-motion";
import { useQueryClient } from "@tanstack/react-query";
import { CheckCircle2Icon, TriangleAlertIcon, XIcon } from "lucide-react";
import { LEADS_KEY, runActive } from "@/lib/api/hooks/app/leads/useLeads";
import type { LeadEnrichmentRun } from "@/lib/api/models/app/leads/Lead";
import { cn } from "@/lib/utils";
import { fmtUsdCost } from "@/lib/money";

// The strip on top of the leads list: a search in flight, the website runs
// going in the background, and how each one ended.
export default function EnrichmentProgress({ runs, searching }: { runs: LeadEnrichmentRun[]; searching: string | null }) {
    const qc = useQueryClient();
    const seen = useRef(new Map<string, string>());
    const [finished, setFinished] = useState<LeadEnrichmentRun[]>([]);

    // A run finishing refreshes the leads it touched and stays on screen a moment.
    useEffect(() => {
        const ended: LeadEnrichmentRun[] = [];
        for (const r of runs) {
            const before = seen.current.get(r.id);
            if (before && before !== r.status && !runActive(r)) ended.push(r);
            seen.current.set(r.id, r.status);
        }
        if (!ended.length) return;
        qc.invalidateQueries({ queryKey: LEADS_KEY });
        setFinished((cur) => [...ended, ...cur].slice(0, 2));
    }, [runs, qc]);

    useEffect(() => {
        if (!finished.length) return;
        const t = setTimeout(() => setFinished((cur) => cur.slice(0, -1)), 9_000);
        return () => clearTimeout(t);
    }, [finished]);

    const active = useMemo(() => runs.filter(runActive), [runs]);
    const total = active.reduce((n, r) => n + r.total, 0);
    const processed = active.reduce((n, r) => n + r.processed, 0);
    const emails = active.reduce((n, r) => n + r.emails_found, 0);
    const people = active.reduce((n, r) => n + r.people_found, 0);
    const pct = total ? Math.round((processed / total) * 100) : 0;

    return (
        <AnimatePresence initial={false}>
            {searching && (
                <Strip key="search">
                    <span className="campaign-grid text-sky-600 shrink-0" aria-hidden />
                    <span className="text-shimmer truncate font-medium text-sky-800">{searching}</span>
                    <Track>
                        <span className="absolute inset-y-0 w-1/3 rounded-full bg-sky-500 progress-sweep" />
                    </Track>
                </Strip>
            )}
            {active.length > 0 && (
                <Strip key="runs">
                    <span className="campaign-grid text-sky-600 shrink-0" aria-hidden />
                    <span className="text-shimmer shrink-0 font-medium text-sky-800">Finding people and emails</span>
                    <span className="truncate text-slate-500 tabular-nums">
                        {processed.toLocaleString()} of {total.toLocaleString()} businesses
                        {emails > 0 && ` · ${emails.toLocaleString()} with an email`}
                        {people > 0 && ` · ${people.toLocaleString()} ${people === 1 ? "person" : "people"}`}
                    </span>
                    <span className="ml-auto hidden sm:inline shrink-0 text-[11px] text-slate-400">Keeps going if you leave</span>
                    <span className="shrink-0 w-8 text-right tabular-nums text-[11px] font-medium text-sky-700">{pct}%</span>
                    <Track>
                        <span className="absolute inset-y-0 left-0 rounded-full bg-sky-500 transition-[width] duration-700" style={{ width: `${Math.max(3, pct)}%` }} />
                        <span className="absolute inset-y-0 w-1/4 rounded-full bg-white/60 progress-sweep" />
                    </Track>
                </Strip>
            )}
            {finished.map((r) => {
                const failed = r.status === "error";
                return (
                    <Strip key={r.id} tone={failed ? "amber" : "emerald"}>
                        {failed ? <TriangleAlertIcon className="size-3.5 shrink-0 text-amber-600" /> : <CheckCircle2Icon className="size-3.5 shrink-0 text-emerald-600" />}
                        <span className={cn("shrink-0 font-medium", failed ? "text-amber-800" : "text-emerald-800")}>{failed ? "Stopped early" : "Done"}</span>
                        <span className="truncate text-slate-600">
                            {failed && r.last_error
                                ? r.last_error
                                : `${r.emails_found.toLocaleString()} with an email${r.people ? `, ${r.people_found.toLocaleString()} ${r.people_found === 1 ? "person" : "people"}` : ""} from ${r.total.toLocaleString()} business${r.total === 1 ? "" : "es"}`}
                            {r.charged_usd > 0 && ` · ${fmtUsdCost(r.charged_usd)}`}
                        </span>
                        <button
                            type="button"
                            aria-label="Dismiss"
                            onClick={() => setFinished((cur) => cur.filter((x) => x.id !== r.id))}
                            className="ml-auto size-5 shrink-0 rounded text-slate-400 hover:text-slate-700 hover:bg-white/70 inline-flex items-center justify-center"
                        >
                            <XIcon className="size-3" />
                        </button>
                    </Strip>
                );
            })}
        </AnimatePresence>
    );
}

function Strip({ children, tone = "sky" }: { children: React.ReactNode; tone?: "sky" | "emerald" | "amber" }) {
    return (
        <motion.div
            initial={{ height: 0, opacity: 0 }}
            animate={{ height: "auto", opacity: 1 }}
            exit={{ height: 0, opacity: 0 }}
            transition={{ duration: 0.2, ease: [0.22, 1, 0.36, 1] }}
            className="shrink-0 overflow-hidden"
        >
            <div
                className={cn(
                    "relative h-9 px-3 flex items-center gap-2 border-b text-[12px]",
                    tone === "sky" && "border-sky-100 bg-sky-50/50",
                    tone === "emerald" && "border-emerald-100 bg-emerald-50/50",
                    tone === "amber" && "border-amber-100 bg-amber-50/60",
                )}
                role="status"
            >
                {children}
            </div>
        </motion.div>
    );
}

function Track({ children }: { children: React.ReactNode }) {
    return <div className="absolute inset-x-0 bottom-0 h-0.5 overflow-hidden bg-sky-100">{children}</div>;
}
