// Split one segment into a live segment per world region, so each region can
// get its own campaign, schedule and mailboxes. The new segments are
// conditions, not snapshots: a contact moves between them as its location does.

import React from "react";
import { useQueries } from "@tanstack/react-query";
import { AnimatePresence, motion } from "framer-motion";
import { GlobeIcon, Loader2Icon, XIcon } from "lucide-react";
import toast from "react-hot-toast/headless";

import { Checkbox } from "@/components/ui/checkbox";
import { previewSegment } from "@/lib/api/client/app/segments";
import { useCreateSegment, useSegments } from "@/lib/api/hooks/app/segments";
import type Segment from "@/lib/api/models/app/segments/Segment";
import type { SegmentCondition } from "@/lib/api/models/app/segments/Segment";
import type { AppError } from "@/lib/api/client/normalizeError";
import buildError from "@/lib/helper/buildError";

type Part = { key: string; label: string; color: string; condition: SegmentCondition };

// The world_region values are the backend's (geo.WorldRegions plus "none", a contact with no country).
const PARTS: Part[] = [
    { key: "north_america", label: "North America", color: "#0284c7", condition: { field: "world_region", operator: "in", values: ["north_america"] } },
    { key: "latin_america", label: "Latin America", color: "#16a34a", condition: { field: "world_region", operator: "in", values: ["latin_america"] } },
    { key: "emea", label: "Europe, Middle East and Africa", color: "#7c3aed", condition: { field: "world_region", operator: "in", values: ["emea"] } },
    { key: "apac", label: "Asia-Pacific", color: "#ea580c", condition: { field: "world_region", operator: "in", values: ["apac"] } },
    { key: "none", label: "No known region", color: "#64748b", condition: { field: "world_region", operator: "in", values: ["none"] } },
];

// SegmentMaxNameLen on the backend, which counts UTF-8 bytes.
const MAX_NAME_BYTES = 120;
const utf8 = new TextEncoder();
const byteLength = (s: string) => utf8.encode(s).length;

function partName(base: string, label: string): string {
    const suffix = ` · ${label}`;
    if (byteLength(base + suffix) <= MAX_NAME_BYTES) return base + suffix;
    // Cut whole characters, never half of one.
    const chars = Array.from(base);
    while (chars.length > 0 && byteLength(chars.join("").trimEnd() + "…" + suffix) > MAX_NAME_BYTES) chars.pop();
    return chars.join("").trimEnd() + "…" + suffix;
}

export default function SplitByRegionDialog({ open, onClose, segment }: { open: boolean; onClose: () => void; segment: Segment }) {
    const create = useCreateSegment();
    const [picked, setPicked] = React.useState<Set<string> | null>(null);
    const [busy, setBusy] = React.useState(false);

    const conditionsFor = React.useCallback(
        (p: Part): SegmentCondition[] => [{ field: "segment", operator: "in", values: [segment.id] }, p.condition],
        [segment.id],
    );
    const counts = useQueries({
        queries: PARTS.map((p) => ({
            queryKey: ["segments", "preview", "split", segment.id, p.key],
            queryFn: () => previewSegment({ match: "all", conditions: conditionsFor(p) }),
            enabled: open,
            staleTime: 30_000,
        })),
    });
    // A region whose segment already exists (an earlier, partly failed split, or a
    // create whose answer was lost) is shown as done and never submitted again.
    const segments = useSegments(open);
    const existing = React.useMemo(() => new Set((segments.data ?? []).map((s) => s.name.toLowerCase())), [segments.data]);
    const exists = React.useCallback((p: Part) => existing.has(partName(segment.name, p.label).toLowerCase()), [existing, segment.name]);
    const loaded = counts.every((c) => c.data !== undefined) && segments.data !== undefined;

    // Preselect every region that has members once the counts land.
    React.useEffect(() => {
        if (!open) {
            setPicked(null);
            return;
        }
        if (picked === null && loaded) {
            setPicked(new Set(PARTS.filter((p, i) => (counts[i].data ?? 0) > 0 && !exists(p)).map((p) => p.key)));
        }
    }, [open, loaded, picked, counts, exists]);

    // Focus moves into the dialog on open and back to what opened it on close.
    const cardRef = React.useRef<HTMLDivElement>(null);
    React.useEffect(() => {
        if (!open) return;
        const back = document.activeElement instanceof HTMLElement ? document.activeElement : null;
        const t = window.setTimeout(() => cardRef.current?.focus(), 0);
        return () => {
            window.clearTimeout(t);
            back?.focus();
        };
    }, [open]);

    const requestClose = React.useCallback(() => {
        if (!busy) onClose();
    }, [busy, onClose]);

    React.useEffect(() => {
        if (!open) return;
        const onKey = (e: KeyboardEvent) => {
            if (e.key !== "Escape") return;
            if (document.querySelector("[data-floating], [role='alertdialog']")) return;
            requestClose();
        };
        document.addEventListener("keydown", onKey);
        return () => document.removeEventListener("keydown", onKey);
    }, [open, requestClose]);

    const chosen = PARTS.filter((p) => picked?.has(p.key) && !exists(p));

    async function submit() {
        if (busy || chosen.length === 0) return;
        setBusy(true);
        let made = 0;
        try {
            for (const p of chosen) {
                try {
                    await create.mutateAsync({
                        name: partName(segment.name, p.label),
                        description: p.key === "none" ? `Contacts in ${segment.name} with no known country.` : `Contacts in ${segment.name} located in ${p.label}.`,
                        color: p.color,
                        match: "all",
                        conditions: conditionsFor(p),
                    });
                } catch (err) {
                    // The name is taken: an earlier attempt created it after all.
                    if ((err as AppError).status !== 409) throw err;
                }
                made++;
                // A retry after a later failure must not create this one again.
                setPicked((prev) => {
                    const next = new Set(prev ?? []);
                    next.delete(p.key);
                    return next;
                });
            }
            toast.success(`Created ${made} segment${made === 1 ? "" : "s"} from ${segment.name}`);
            onClose();
        } catch (err) {
            toast.error(made > 0 ? `Created ${made}, then: ${buildError(err as AppError)}` : buildError(err as AppError));
        } finally {
            setBusy(false);
        }
    }

    return (
        <AnimatePresence>
            {open && (
                <motion.div
                    key="overlay"
                    initial={{ opacity: 0 }}
                    animate={{ opacity: 1 }}
                    exit={{ opacity: 0 }}
                    transition={{ duration: 0.15 }}
                    onMouseDown={requestClose}
                    className="fixed inset-0 z-[120] flex items-center justify-center bg-slate-900/30 backdrop-blur-[2px] px-4"
                >
                    <motion.div
                        key="card"
                        role="dialog"
                        aria-modal="true"
                        aria-label="Split segment by region"
                        ref={cardRef}
                        tabIndex={-1}
                        initial={{ y: 8, opacity: 0, scale: 0.985 }}
                        animate={{ y: 0, opacity: 1, scale: 1 }}
                        exit={{ y: 8, opacity: 0, scale: 0.985 }}
                        transition={{ duration: 0.18, ease: [0.22, 1, 0.36, 1] }}
                        onMouseDown={(e) => e.stopPropagation()}
                        className="w-full max-w-[480px] rounded-lg outline-none bg-white border border-slate-200 shadow-[0_24px_48px_-12px_rgba(15,23,42,0.18),0_8px_16px_-8px_rgba(15,23,42,0.1)] overflow-hidden flex flex-col max-h-[80dvh]"
                    >
                        <header className="h-12 px-4 border-b border-slate-200 flex items-center gap-2.5 shrink-0">
                            <div className="size-5 rounded bg-slate-100 text-slate-600 flex items-center justify-center">
                                <GlobeIcon className="w-3 h-3" />
                            </div>
                            <span className="text-[10px] uppercase tracking-[0.14em] text-slate-400 font-medium">Split by region</span>
                            <div className="h-4 w-px bg-slate-200" />
                            <span className="text-[12.5px] text-slate-900 font-medium truncate">{segment.name}</span>
                            <button
                                type="button"
                                onClick={requestClose}
                                aria-label="Close"
                                className="ml-auto size-7 rounded-md text-slate-500 hover:text-slate-900 hover:bg-slate-100 inline-flex items-center justify-center transition-colors"
                            >
                                <XIcon className="w-3.5 h-3.5" />
                            </button>
                        </header>
                        <p className="px-4 pt-3 text-[11.5px] leading-snug text-slate-500">
                            Each region becomes its own live segment, so it can get its own campaign, sending hours and mailboxes. Location
                            comes from the contacts' timezone, country, state, city or location fields, then their phone number and email
                            domain.
                        </p>
                        <ul className="px-2 py-2 overflow-y-auto">
                            {PARTS.map((p, i) => {
                                const n = counts[i].data;
                                return (
                                    <li key={p.key}>
                                        <label className="h-9 px-2 rounded-md flex items-center gap-2.5 cursor-pointer select-none hover:bg-slate-50">
                                            <Checkbox
                                                tone="slate"
                                                checked={exists(p) || (picked?.has(p.key) ?? false)}
                                                disabled={!picked || busy || exists(p)}
                                                onChange={(e) =>
                                                    setPicked((prev) => {
                                                        const next = new Set(prev ?? []);
                                                        if (e.target.checked) next.add(p.key);
                                                        else next.delete(p.key);
                                                        return next;
                                                    })
                                                }
                                            />
                                            <span className="size-2.5 rounded-full shrink-0" style={{ backgroundColor: p.color }} />
                                            <span className="text-[12.5px] text-slate-800 truncate">{p.label}</span>
                                            {exists(p) && <span className="text-[10.5px] text-emerald-600">Already created</span>}
                                            <span className="ml-auto font-mono text-[11px] tabular-nums text-slate-500">
                                                {n === undefined ? (
                                                    <span className="inline-block h-3 w-8 rounded bg-slate-100 animate-pulse align-middle" />
                                                ) : (
                                                    n.toLocaleString()
                                                )}
                                            </span>
                                        </label>
                                    </li>
                                );
                            })}
                        </ul>
                        <footer className="px-3 py-2 min-h-12 border-t border-slate-200 flex flex-wrap items-center gap-x-2 gap-y-1.5 shrink-0 bg-slate-50/30">
                            <span className="text-[11px] text-slate-400 leading-snug basis-full sm:basis-0 sm:flex-1 sm:min-w-[160px]">
                                {segment.name} itself is kept as it is.
                            </span>
                            <button
                                type="button"
                                onClick={requestClose}
                                disabled={busy}
                                className="ml-auto shrink-0 h-7 px-2.5 rounded-md text-[12px] text-slate-700 hover:text-slate-900 hover:bg-slate-100 transition-colors disabled:opacity-50"
                            >
                                Cancel
                            </button>
                            <button
                                type="button"
                                onClick={submit}
                                disabled={busy || chosen.length === 0}
                                className="shrink-0 whitespace-nowrap h-7 px-2.5 rounded-md bg-sky-600 hover:bg-sky-700 text-white text-[12px] font-medium inline-flex items-center gap-1.5 transition-colors disabled:opacity-50"
                            >
                                {busy ? <Loader2Icon className="w-3 h-3 animate-spin" /> : <GlobeIcon className="w-3 h-3" />}
                                {chosen.length === 0 ? "Pick a region" : `Create ${chosen.length} segment${chosen.length === 1 ? "" : "s"}`}
                            </button>
                        </footer>
                    </motion.div>
                </motion.div>
            )}
        </AnimatePresence>
    );
}
