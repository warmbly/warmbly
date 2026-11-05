import { useEffect, useMemo, useRef } from "react";
import { AnimatePresence, motion } from "framer-motion";
import { useQueryClient } from "@tanstack/react-query";
import { CheckCircle2Icon, Loader2Icon, TriangleAlertIcon } from "lucide-react";
import { LEADS_KEY, useLeadEnrichments } from "@/lib/api/hooks/app/leads/useLeads";

// A card in the map's corner while websites are being read.
export default function EnrichmentProgress() {
    const runs = useLeadEnrichments();
    const qc = useQueryClient();
    const seen = useRef(new Map<string, string>());
    const list = useMemo(() => runs.data ?? [], [runs.data]);

    // A run finishing refreshes the leads it touched, for this tab too.
    useEffect(() => {
        let finished = false;
        for (const r of list) {
            const before = seen.current.get(r.id);
            if (before && before !== r.status && (r.status === "done" || r.status === "error")) finished = true;
            seen.current.set(r.id, r.status);
        }
        if (finished) qc.invalidateQueries({ queryKey: LEADS_KEY });
    }, [list, qc]);

    const visible = list.slice(0, 3);
    return (
        <div className="pointer-events-none absolute bottom-3 right-3 z-10 flex w-[260px] flex-col gap-1.5">
            <AnimatePresence>
                {visible.map((r) => {
                    const pct = r.total ? Math.round((r.processed / r.total) * 100) : 0;
                    const done = r.status === "done";
                    const failed = r.status === "error";
                    return (
                        <motion.div
                            key={r.id}
                            initial={{ y: 8, opacity: 0 }}
                            animate={{ y: 0, opacity: 1 }}
                            exit={{ y: 8, opacity: 0 }}
                            className="pointer-events-auto rounded-lg border border-slate-200 bg-white/95 px-3 py-2 shadow-[0_8px_24px_-12px_rgba(15,23,42,0.35)] backdrop-blur"
                        >
                            <div className="flex items-center gap-1.5 text-[12px]">
                                {done ? (
                                    <CheckCircle2Icon className="size-3.5 text-emerald-600" />
                                ) : failed ? (
                                    <TriangleAlertIcon className="size-3.5 text-amber-600" />
                                ) : (
                                    <Loader2Icon className="size-3.5 animate-spin text-sky-600" />
                                )}
                                <span className="font-medium text-slate-900">{done ? "Websites read" : failed ? "Stopped early" : "Reading websites"}</span>
                                <span className="ml-auto tabular-nums text-slate-500">
                                    {r.processed}/{r.total}
                                </span>
                            </div>
                            <div className="mt-1.5 h-1 rounded-full bg-slate-100 overflow-hidden">
                                <div className={done ? "h-full bg-emerald-500" : failed ? "h-full bg-amber-500" : "h-full bg-sky-500 transition-[width] duration-500"} style={{ width: `${pct}%` }} />
                            </div>
                            <div className="mt-1 text-[11px] text-slate-500">
                                {r.emails_found.toLocaleString()} with an email
                                {r.people && ` · ${r.people_found.toLocaleString()} ${r.people_found === 1 ? "person" : "people"} found`}
                                {r.no_website > 0 && ` · ${r.no_website} without a site`}
                                {r.credits_consumed > 0 && ` · ${r.credits_consumed} credits`}
                            </div>
                            {failed && r.last_error && <div className="mt-0.5 text-[11px] text-amber-700">{r.last_error}</div>}
                        </motion.div>
                    );
                })}
            </AnimatePresence>
        </div>
    );
}
