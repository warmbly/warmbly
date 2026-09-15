// From a campaign's Leads tab: link segments as a live audience source.
// Members of linked segments are enrolled as leads automatically, now and as
// the segments grow. Replaces the one-shot "From segment" copy.
//
// The set is the audience: detaching a segment takes its leads back out again,
// apart from anyone the campaign has already emailed or a person added by hand
// (issue #510). Saving a detachment that costs leads asks first.

import React from "react";
import { AnimatePresence, motion } from "framer-motion";
import { LayersIcon, Loader2Icon, XIcon, CheckIcon } from "lucide-react";
import toast from "react-hot-toast";

import { SearchInput } from "@/components/ui/field";
import { useConfirm } from "@/hooks/context/confirm";
import { useCampaignSegments, useSegments, useSetCampaignSegments } from "@/lib/api/hooks/app/segments";
import { linkTotals, linksEmptyReason } from "./linkedSegments";
import type MiniCampaign from "@/lib/api/models/app/campaigns/MiniCampaign";
import type { AppError } from "@/lib/api/client/normalizeError";
import buildError from "@/lib/helper/buildError";
import { cn } from "@/lib/utils";

// leads renders a count with its noun, so every sentence below reads the same.
const leads = (n: number) => `${n.toLocaleString()} lead${n === 1 ? "" : "s"}`;

export default function CampaignSegmentsDialog({
    open,
    onClose,
    campaign,
}: {
    open: boolean;
    onClose: () => void;
    campaign: MiniCampaign;
}) {
    const confirm = useConfirm();
    const segments = useSegments(open);
    const linked = useCampaignSegments(campaign.id, open);
    const save = useSetCampaignSegments();
    const [query, setQuery] = React.useState("");
    const [picked, setPicked] = React.useState<Set<string>>(new Set());
    const [seeded, setSeeded] = React.useState(false);

    React.useEffect(() => {
        if (!open) {
            setQuery("");
            setPicked(new Set());
            setSeeded(false);
        }
    }, [open]);

    React.useEffect(() => {
        if (!open || seeded || !linked.data) return;
        setPicked(new Set(linked.data.map((l) => l.segment_id)));
        setSeeded(true);
    }, [open, seeded, linked.data]);

    const list = React.useMemo(() => {
        const all = segments.data ?? [];
        const q = query.trim().toLowerCase();
        return q ? all.filter((s) => s.name.toLowerCase().includes(q)) : all;
    }, [segments.data, query]);

    const dirty = React.useMemo(() => {
        if (!seeded || !linked.data) return false;
        const before = new Set(linked.data.map((l) => l.segment_id));
        if (before.size !== picked.size) return true;
        for (const id of picked) if (!before.has(id)) return true;
        return false;
    }, [seeded, linked.data, picked]);

    // The links the save would drop, so a detachment that costs leads can say
    // so before it happens.
    const detaching = React.useMemo(() => {
        if (!seeded || !linked.data) return [];
        return linked.data.filter((l) => !picked.has(l.segment_id) && l.lead_count > 0);
    }, [seeded, linked.data, picked]);

    const busy = save.isPending;
    const requestClose = React.useCallback(() => {
        if (busy) return;
        if (dirty) {
            confirm.show("Discard your segment changes?", async () => onClose());
            return;
        }
        onClose();
    }, [busy, dirty, confirm, onClose]);

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

    function toggle(id: string) {
        setPicked((prev) => {
            const next = new Set(prev);
            if (next.has(id)) next.delete(id);
            else next.add(id);
            return next;
        });
    }

    async function submit() {
        if (busy || !seeded) return;
        try {
            const res = await save.mutateAsync({ campaignId: campaign.id, segmentIds: [...picked] });
            const linked = `Linked ${picked.size} segment${picked.size === 1 ? "" : "s"}`;
            const { members, held } = linkTotals(res.data);
            const parts = [
                res.added > 0 ? `added ${res.added.toLocaleString()}` : "",
                res.withdrawn > 0 ? `removed ${res.withdrawn.toLocaleString()}` : "",
            ].filter(Boolean);
            // "added 5 and removed 3 leads": the noun goes on the end so it is
            // not repeated when the save did both.
            const moved = parts.length
                ? `${parts.join(" and ")} lead${res.added + res.withdrawn === 1 ? "" : "s"}`
                : "";
            // Leads a detached segment left behind are the surprising part, so
            // they are named rather than left as a count that does not add up.
            const stayed =
                res.contacted > 0
                    ? ` ${leads(res.contacted)} stayed: the campaign has already emailed them.`
                    : "";
            // stayed is appended to every branch: a detach where the whole
            // audience had already been emailed changes no count at all, and
            // saying nothing after confirming "removes up to N leads" reads as
            // a save that did not happen.
            if (picked.size === 0) {
                toast.success((res.withdrawn > 0 ? `Segments detached and ${leads(res.withdrawn)} removed` : "Segments detached") + stayed);
            } else if (moved) {
                toast.success(`${linked} and ${moved}.${stayed}`);
            } else if (stayed) {
                toast.success(`${linked}.${stayed}`);
            } else if (members === 0 || held > 0) {
                // Say why the list is still empty; "already a lead" here would be a lie.
                toast(`${linked}. ${linksEmptyReason(res.data)}`);
            } else {
                toast.success(`${linked}; every member is already a lead`);
            }
            onClose();
        } catch (err) {
            toast.error(buildError(err as AppError));
        }
    }

    // Saving a detachment that costs leads confirms first: nothing else in the
    // dialog removes anybody, so the Save button alone would not read as one.
    function requestSubmit() {
        if (busy || !seeded || !dirty) return;
        if (detaching.length === 0) {
            void submit();
            return;
        }
        const total = detaching.reduce((n, l) => n + l.lead_count, 0);
        const which = detaching.length === 1 ? detaching[0].name : `${detaching.length} segments`;
        confirm.show(
            `Detaching ${which} removes up to ${total.toLocaleString()} lead${total === 1 ? "" : "s"} from this campaign. Leads the campaign has already emailed, and any you added by hand, stay.`,
            submit,
        );
    }

    const loading = segments.isPending || (linked.isPending && !seeded);
    // A failed load must not strand the dialog as "loaded but Save disabled".
    const loadError = segments.isError || (linked.isError && !seeded);
    const retryLoad = () => {
        if (segments.isError) void segments.refetch();
        if (linked.isError) void linked.refetch();
    };

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
                        aria-label="Linked segments"
                        initial={{ y: 8, opacity: 0, scale: 0.985 }}
                        animate={{ y: 0, opacity: 1, scale: 1 }}
                        exit={{ y: 8, opacity: 0, scale: 0.985 }}
                        transition={{ duration: 0.18, ease: [0.22, 1, 0.36, 1] }}
                        onMouseDown={(e) => e.stopPropagation()}
                        className="w-full max-w-[520px] rounded-lg bg-white border border-slate-200 shadow-[0_24px_48px_-12px_rgba(15,23,42,0.18),0_8px_16px_-8px_rgba(15,23,42,0.1)] overflow-hidden flex flex-col max-h-[80dvh]"
                    >
                        <header className="h-12 px-4 border-b border-slate-200 flex items-center gap-2.5 shrink-0">
                            <div className="size-5 rounded bg-slate-100 text-slate-600 flex items-center justify-center">
                                <LayersIcon className="w-3 h-3" />
                            </div>
                            <span className="text-[10px] uppercase tracking-[0.14em] text-slate-400 font-medium">Audience</span>
                            <div className="h-4 w-px bg-slate-200" />
                            <span className="text-[12.5px] text-slate-900 font-medium">Linked segments</span>
                            <span className="hidden sm:inline-flex items-center h-5 px-1.5 rounded bg-sky-50 text-sky-700 text-[10px] font-medium max-w-[200px] truncate">
                                {campaign.name}
                            </span>
                            <button
                                type="button"
                                onClick={requestClose}
                                aria-label="Close"
                                className="ml-auto size-7 rounded-md text-slate-500 hover:text-slate-900 hover:bg-slate-100 inline-flex items-center justify-center transition-colors"
                            >
                                <XIcon className="w-3.5 h-3.5" />
                            </button>
                        </header>
                        <div className="px-4 py-2.5 border-b border-slate-100 shrink-0 bg-slate-50/40">
                            <p className="text-[11.5px] text-slate-500 leading-snug">
                                Contacts in a linked segment become leads automatically, now and whenever the segment grows.
                                Detaching one takes its leads back out, apart from those the campaign has already emailed
                                and any you added by hand.
                            </p>
                        </div>
                        <div className="px-4 py-3 border-b border-slate-100 shrink-0">
                            <SearchInput value={query} onChange={setQuery} placeholder="Search segments…" autoFocus className="w-full" />
                        </div>
                        <div className="flex-1 min-h-[200px] overflow-y-auto">
                            {loading ? (
                                <div className="p-3 space-y-1.5">
                                    {[...Array(4)].map((_, i) => (
                                        <div key={i} className="h-9 rounded-md bg-slate-100 animate-pulse" />
                                    ))}
                                </div>
                            ) : loadError ? (
                                <div className="px-5 py-10 text-center">
                                    <p className="text-[12.5px] text-slate-900 font-medium">Couldn't load segments</p>
                                    <p className="text-[11.5px] text-slate-400 mt-0.5">Check your connection and try again.</p>
                                    <button
                                        type="button"
                                        onClick={retryLoad}
                                        className="mt-3 h-7 px-2.5 rounded-md border border-slate-200 hover:border-slate-300 text-[12px] text-slate-700 hover:text-slate-900 transition-colors"
                                    >
                                        Retry
                                    </button>
                                </div>
                            ) : list.length === 0 ? (
                                <div className="px-5 py-10 text-center">
                                    <p className="text-[12.5px] text-slate-900 font-medium">{query ? "No segments match" : "No segments yet"}</p>
                                    <p className="text-[11.5px] text-slate-400 mt-0.5">Build one under Contacts &gt; Segments first.</p>
                                </div>
                            ) : (
                                <ul className="divide-y divide-slate-100">
                                    {list.map((s) => {
                                        const on = picked.has(s.id);
                                        return (
                                            <li key={s.id}>
                                                <button
                                                    type="button"
                                                    onClick={() => toggle(s.id)}
                                                    aria-pressed={on}
                                                    className={cn(
                                                        "w-full px-4 h-10 flex items-center gap-3 text-left transition-colors",
                                                        on ? "bg-sky-50/60 hover:bg-sky-50" : "hover:bg-slate-50",
                                                    )}
                                                >
                                                    <span
                                                        className={cn(
                                                            "size-3.5 rounded border flex items-center justify-center shrink-0",
                                                            on ? "border-sky-600 bg-sky-600" : "border-slate-300 bg-white",
                                                        )}
                                                    >
                                                        {on && <CheckIcon className="w-2.5 h-2.5 text-white" />}
                                                    </span>
                                                    <span className="size-2 rounded-full shrink-0" style={{ backgroundColor: s.color }} />
                                                    <span className="text-[12.5px] text-slate-900 font-medium truncate">{s.name}</span>
                                                    <span className="ml-auto font-mono text-[11px] text-slate-500 tabular-nums">
                                                        {s.contact_count.toLocaleString()}
                                                    </span>
                                                </button>
                                            </li>
                                        );
                                    })}
                                </ul>
                            )}
                        </div>
                        <footer className="px-3 py-2 min-h-12 border-t border-slate-200 flex flex-wrap items-center gap-x-2 gap-y-1.5 shrink-0 bg-slate-50/30">
                            <span className="text-[11px] text-slate-400 leading-snug basis-full sm:basis-0 sm:flex-1 sm:min-w-[120px]">
                                {picked.size === 0 ? "No segments linked" : `${picked.size} segment${picked.size === 1 ? "" : "s"} linked`}
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
                                onClick={requestSubmit}
                                disabled={busy || !seeded || !dirty}
                                className="shrink-0 whitespace-nowrap h-7 px-2.5 rounded-md bg-sky-600 hover:bg-sky-700 text-white text-[12px] font-medium inline-flex items-center gap-1.5 transition-colors disabled:opacity-50"
                            >
                                {busy ? <Loader2Icon className="w-3 h-3 animate-spin" /> : <LayersIcon className="w-3 h-3" />}
                                Save
                            </button>
                        </footer>
                    </motion.div>
                </motion.div>
            )}
        </AnimatePresence>
    );
}
