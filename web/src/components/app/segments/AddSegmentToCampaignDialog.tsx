// Enrol a segment's current members as leads of one campaign. A snapshot:
// contacts who join the segment later are not added automatically.

import React from "react";
import { AnimatePresence, motion } from "framer-motion";
import { Loader2Icon, MegaphoneIcon, XIcon } from "lucide-react";
import toast from "react-hot-toast/headless";

import { SearchInput } from "@/components/ui/field";
import { Checkbox } from "@/components/ui/checkbox";
import { campaignStatusLabel, campaignStatusTone } from "@/components/app/campaigns/status";
import useCampaigns from "@/lib/api/hooks/app/campaigns/useCampaigns";
import { useAddSegmentToCampaign } from "@/lib/api/hooks/app/segments";
import type Segment from "@/lib/api/models/app/segments/Segment";
import type { AppError } from "@/lib/api/client/normalizeError";
import buildError from "@/lib/helper/buildError";
import { cn } from "@/lib/utils";

export default function AddSegmentToCampaignDialog({
    open,
    onClose,
    segment,
}: {
    open: boolean;
    onClose: () => void;
    segment: Segment;
}) {
    const add = useAddSegmentToCampaign();
    const [query, setQuery] = React.useState("");
    const [picked, setPicked] = React.useState<string | null>(null);
    const [skipActive, setSkipActive] = React.useState(false);
    const campaigns = useCampaigns({ query: query.trim(), folder: "", enabled: open });

    React.useEffect(() => {
        if (!open) {
            setQuery("");
            setPicked(null);
            setSkipActive(false);
        }
    }, [open]);

    const busy = add.isPending;
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

    async function submit() {
        if (!picked || busy) return;
        const target = campaigns.campaigns.find((c) => c.id === picked);
        try {
            const res = await add.mutateAsync({ id: segment.id, campaignId: picked, skipActiveInOtherCampaigns: skipActive });
            toast.success(
                res.added === 0
                    ? res.members === 0
                        ? `${segment.name} has no members to add`
                        : skipActive
                          ? `No new leads for ${target?.name ?? "the campaign"}: every member is already a lead or running elsewhere`
                          : `Every member of ${segment.name} is already a lead in ${target?.name ?? "the campaign"}`
                    : `Added ${res.added.toLocaleString()} lead${res.added === 1 ? "" : "s"} to ${target?.name ?? "the campaign"}`,
            );
            onClose();
        } catch (err) {
            toast.error(buildError(err as AppError));
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
                        aria-label="Add segment to campaign"
                        initial={{ y: 8, opacity: 0, scale: 0.985 }}
                        animate={{ y: 0, opacity: 1, scale: 1 }}
                        exit={{ y: 8, opacity: 0, scale: 0.985 }}
                        transition={{ duration: 0.18, ease: [0.22, 1, 0.36, 1] }}
                        onMouseDown={(e) => e.stopPropagation()}
                        className="w-full max-w-[520px] rounded-lg bg-white border border-slate-200 shadow-[0_24px_48px_-12px_rgba(15,23,42,0.18),0_8px_16px_-8px_rgba(15,23,42,0.1)] overflow-hidden flex flex-col max-h-[80dvh]"
                    >
                        <header className="h-12 px-4 border-b border-slate-200 flex items-center gap-2.5 shrink-0">
                            <div className="size-5 rounded bg-slate-100 text-slate-600 flex items-center justify-center">
                                <MegaphoneIcon className="w-3 h-3" />
                            </div>
                            <span className="text-[10px] uppercase tracking-[0.14em] text-slate-400 font-medium">Add to campaign</span>
                            <div className="h-4 w-px bg-slate-200" />
                            <span className="text-[12.5px] text-slate-900 font-medium truncate">{segment.name}</span>
                            <span className="hidden sm:inline-flex items-center h-5 px-1.5 rounded bg-sky-50 text-sky-700 text-[10px] font-medium">
                                {segment.contact_count.toLocaleString()} contact{segment.contact_count === 1 ? "" : "s"}
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
                        <div className="px-4 py-3 border-b border-slate-100 shrink-0">
                            <SearchInput value={query} onChange={setQuery} placeholder="Search campaigns…" autoFocus className="w-full" />
                        </div>
                        <div className="flex-1 min-h-[200px] overflow-y-auto">
                            {campaigns.isPending ? (
                                <div className="p-3 space-y-1.5">
                                    {[...Array(4)].map((_, i) => (
                                        <div key={i} className="h-9 rounded-md bg-slate-100 animate-pulse" />
                                    ))}
                                </div>
                            ) : campaigns.campaigns.length === 0 ? (
                                <div className="px-5 py-10 text-center">
                                    <p className="text-[12.5px] text-slate-900 font-medium">No campaigns found</p>
                                    <p className="text-[11.5px] text-slate-400 mt-0.5">Create a campaign first, then add this segment to it.</p>
                                </div>
                            ) : (
                                <ul className="divide-y divide-slate-100">
                                    {campaigns.campaigns.map((c) => {
                                        const on = picked === c.id;
                                        return (
                                            <li key={c.id}>
                                                <button
                                                    type="button"
                                                    onClick={() => setPicked(c.id)}
                                                    aria-pressed={on}
                                                    className={cn(
                                                        "w-full px-4 h-10 flex items-center gap-3 text-left transition-colors",
                                                        on ? "bg-sky-50/60 hover:bg-sky-50" : "hover:bg-slate-50",
                                                    )}
                                                >
                                                    <span
                                                        className={cn(
                                                            "size-3.5 rounded-full border flex items-center justify-center shrink-0",
                                                            on ? "border-sky-600 bg-sky-600" : "border-slate-300 bg-white",
                                                        )}
                                                    >
                                                        {on && <span className="size-1.5 rounded-full bg-white" />}
                                                    </span>
                                                    <span className="text-[12.5px] text-slate-900 font-medium truncate">{c.name}</span>
                                                    <span
                                                        className={cn(
                                                            "ml-auto shrink-0 text-[10.5px] uppercase tracking-[0.1em]",
                                                            campaignStatusTone(c.status),
                                                        )}
                                                    >
                                                        {campaignStatusLabel(c.status)}
                                                    </span>
                                                </button>
                                            </li>
                                        );
                                    })}
                                    {campaigns.hasNextPage && (
                                        <li className="p-2">
                                            <button
                                                type="button"
                                                onClick={() => campaigns.fetchNextPage()}
                                                disabled={campaigns.isFetchingNextPage}
                                                className="w-full h-8 rounded-md text-[12px] text-slate-600 hover:text-slate-900 hover:bg-slate-50 inline-flex items-center justify-center gap-1.5 transition-colors disabled:opacity-60"
                                            >
                                                {campaigns.isFetchingNextPage && <Loader2Icon className="w-3 h-3 animate-spin" />}
                                                Load more
                                            </button>
                                        </li>
                                    )}
                                </ul>
                            )}
                        </div>
                        <label className="px-4 py-2.5 border-t border-slate-100 flex items-start gap-1.5 text-[11.5px] text-slate-700 cursor-pointer select-none shrink-0">
                            <Checkbox tone="slate" className="mt-px" checked={skipActive} onChange={(e) => setSkipActive(e.target.checked)} />
                            <span>
                                Skip contacts already running in another campaign
                                <span className="block text-slate-500">Members another campaign is still emailing are left out.</span>
                            </span>
                        </label>
                        {/* Wraps instead of clipping: the hint takes its own row when the
                            buttons need the width, so neither is ever cut mid-word. */}
                        <footer className="px-3 py-2 min-h-12 border-t border-slate-200 flex flex-wrap items-center gap-x-2 gap-y-1.5 shrink-0 bg-slate-50/30">
                            <span className="text-[11px] text-slate-400 leading-snug basis-full sm:basis-0 sm:flex-1 sm:min-w-[160px]">
                                Adds today's members. Contacts already in the campaign are skipped.
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
                                disabled={busy || !picked}
                                className="shrink-0 whitespace-nowrap h-7 px-2.5 rounded-md bg-sky-600 hover:bg-sky-700 text-white text-[12px] font-medium inline-flex items-center gap-1.5 transition-colors disabled:opacity-50"
                            >
                                {busy ? <Loader2Icon className="w-3 h-3 animate-spin" /> : <MegaphoneIcon className="w-3 h-3" />}
                                Add leads
                            </button>
                        </footer>
                    </motion.div>
                </motion.div>
            )}
        </AnimatePresence>
    );
}
