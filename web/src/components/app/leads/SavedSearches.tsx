import { useState } from "react";
import { AnimatePresence, LayoutGroup, motion } from "framer-motion";
import toast from "react-hot-toast/headless";
import { BookmarkIcon, ChevronDownIcon, HistoryIcon, MoreHorizontalIcon, PencilIcon, Trash2Icon } from "lucide-react";
import { PopoverMenu, PopoverMenuContent, PopoverMenuItem, PopoverMenuSeparator, PopoverMenuTrigger } from "@/components/ui/popover-menu";
import { useConfirm } from "@/hooks/context/confirm";
import type { AppError } from "@/lib/api/client/normalizeError";
import { useDeleteLeadSearch, useLeadSearches, useUpdateLeadSearch } from "@/lib/api/hooks/app/leads/useLeads";
import type { LeadFinderConfig, LeadSearch } from "@/lib/api/models/app/leads/Lead";
import buildError from "@/lib/helper/buildError";
import timeAgo from "@/lib/helper/timeAgo";
import { cn } from "@/lib/utils";

export default function SavedSearches({
    config,
    activeId,
    onOpen,
    onDelete,
}: {
    config: LeadFinderConfig;
    activeId: string | null;
    onOpen: (s: LeadSearch) => void;
    onDelete: (id: string) => void;
}) {
    const searches = useLeadSearches();
    const list = searches.data ?? [];
    const saved = list.filter((s) => s.saved);
    const recent = list.filter((s) => !s.saved);

    if (searches.isPending) {
        return (
            <div className="space-y-1.5">
                {[0, 1, 2].map((i) => (
                    <div key={i} className="h-12 rounded-md bg-slate-100 animate-pulse" />
                ))}
            </div>
        );
    }
    if (!list.length) {
        return (
            <div className="flex items-start gap-2.5 rounded-lg border border-dashed border-slate-200 px-3 py-3">
                <HistoryIcon className="mt-0.5 size-4 shrink-0 text-slate-300" />
                <p className="text-[11.5px] leading-relaxed text-slate-500">
                    Your searches show up here, so you can open one again later. Bookmark the ones you come back to.
                </p>
            </div>
        );
    }
    // One layout group, so a bookmarked search glides from Recent up into Saved.
    return (
        <LayoutGroup>
            <div className="space-y-4">
                <AnimatePresence initial={false}>
                    {saved.length > 0 && <Section key="saved" title="Saved" items={saved} config={config} activeId={activeId} onOpen={onOpen} onDelete={onDelete} />}
                </AnimatePresence>
                {recent.length > 0 && <Section title="Recent" items={recent} config={config} activeId={activeId} onOpen={onOpen} onDelete={onDelete} cap={4} />}
            </div>
        </LayoutGroup>
    );
}

function Section({
    title,
    items,
    config,
    activeId,
    onOpen,
    onDelete,
    cap,
}: {
    title: string;
    items: LeadSearch[];
    config: LeadFinderConfig;
    activeId: string | null;
    onOpen: (s: LeadSearch) => void;
    onDelete: (id: string) => void;
    /** Show this many until the list is opened up. */
    cap?: number;
}) {
    const [all, setAll] = useState(false);
    const hidden = cap && !all ? Math.max(items.length - cap, 0) : 0;
    const shown = hidden ? items.slice(0, cap) : items;
    return (
        <motion.div layout initial={{ opacity: 0 }} animate={{ opacity: 1 }} exit={{ opacity: 0 }} transition={{ duration: 0.18 }}>
            <motion.div layout="position" className="mb-1.5 flex items-center text-[10px] font-medium uppercase tracking-[0.14em] text-slate-400">
                {title}
                <span className="ml-1.5 tabular-nums text-slate-300">{items.length}</span>
            </motion.div>
            <ul className="space-y-1">
                <AnimatePresence initial={false}>
                    {shown.map((s) => (
                        <SearchRow key={s.id} search={s} config={config} active={s.id === activeId} onOpen={() => onOpen(s)} onDelete={() => onDelete(s.id)} />
                    ))}
                </AnimatePresence>
            </ul>
            {cap && items.length > cap && (
                <motion.button
                    layout="position"
                    type="button"
                    onClick={() => setAll((v) => !v)}
                    className="mt-1.5 inline-flex h-6 items-center gap-1 rounded-md px-1.5 text-[11.5px] text-slate-500 hover:bg-slate-100 hover:text-slate-900"
                >
                    {all ? "Show fewer" : `Show ${hidden} more`}
                    <ChevronDownIcon className={cn("size-3 transition-transform duration-200", all && "rotate-180")} />
                </motion.button>
            )}
        </motion.div>
    );
}

function SearchRow({ search, config, active, onOpen, onDelete }: { search: LeadSearch; config: LeadFinderConfig; active: boolean; onOpen: () => void; onDelete: () => void }) {
    const update = useUpdateLeadSearch();
    const remove = useDeleteLeadSearch();
    const confirm = useConfirm();
    const [editing, setEditing] = useState(false);
    const [name, setName] = useState(search.name);
    const provider = config.providers.find((p) => p.id === search.provider)?.name ?? search.provider;

    const commit = () => {
        setEditing(false);
        const n = name.trim();
        if (!n || n === search.name) {
            setName(search.name);
            return;
        }
        update.mutate({ id: search.id, patch: { name: n } }, { onError: (e) => toast.error(buildError(e as unknown as AppError)) });
    };

    return (
        <motion.li
            layout
            layoutId={`lead-search-${search.id}`}
            initial={{ opacity: 0, y: -4 }}
            animate={{ opacity: 1, y: 0 }}
            exit={{ opacity: 0, scale: 0.97 }}
            transition={{ type: "spring", stiffness: 480, damping: 38, mass: 0.7 }}
            onClick={() => !editing && onOpen()}
            className={cn(
                "group rounded-md border px-2.5 py-2 cursor-pointer transition-colors",
                active ? "border-sky-300 bg-sky-50/60" : "border-slate-200 hover:border-slate-300 hover:bg-slate-50",
            )}
        >
            <div className="flex items-center gap-1.5">
                {editing ? (
                    <input
                        autoFocus
                        value={name}
                        maxLength={120}
                        onClick={(e) => e.stopPropagation()}
                        onChange={(e) => setName(e.target.value)}
                        onBlur={commit}
                        onKeyDown={(e) => {
                            if (e.key === "Enter") commit();
                            if (e.key === "Escape") {
                                setName(search.name);
                                setEditing(false);
                            }
                        }}
                        className="h-6 min-w-0 flex-1 rounded border border-sky-300 px-1.5 text-[12.5px] outline-none"
                    />
                ) : (
                    // A real button, so the row opens from the keyboard; its click bubbles to the row.
                    <button
                        type="button"
                        aria-current={active || undefined}
                        className="min-w-0 flex-1 truncate rounded text-left text-[12.5px] font-medium text-slate-900 outline-none focus-visible:ring-2 focus-visible:ring-sky-200"
                    >
                        {search.name}
                    </button>
                )}
                <button
                    type="button"
                    aria-label={search.saved ? "Remove from saved" : "Save search"}
                    title={search.saved ? "Remove from saved" : "Save search"}
                    onClick={(e) => {
                        e.stopPropagation();
                        update.mutate({ id: search.id, patch: { saved: !search.saved } });
                    }}
                    className={cn(
                        "size-6 rounded inline-flex items-center justify-center",
                        search.saved ? "text-amber-500" : "text-slate-300 hover:text-slate-600 opacity-100 md:opacity-0 md:group-hover:opacity-100",
                    )}
                >
                    <BookmarkIcon className={cn("size-3.5", search.saved && "fill-amber-400")} />
                </button>
                <PopoverMenu align="end">
                    <PopoverMenuTrigger asChild>
                        <button
                            type="button"
                            aria-label="More"
                            onClick={(e) => e.stopPropagation()}
                            className="size-6 rounded text-slate-400 hover:text-slate-700 hover:bg-white inline-flex items-center justify-center"
                        >
                            <MoreHorizontalIcon className="size-3.5" />
                        </button>
                    </PopoverMenuTrigger>
                    <PopoverMenuContent minWidth={170}>
                        <PopoverMenuItem icon={<PencilIcon className="size-3.5" />} onSelect={() => setEditing(true)}>
                            Rename
                        </PopoverMenuItem>
                        <PopoverMenuSeparator />
                        <PopoverMenuItem
                            danger
                            icon={<Trash2Icon className="size-3.5" />}
                            onSelect={() =>
                                confirm?.show(`Delete "${search.name}"? The leads it found stay in your list.`, async () => {
                                    try {
                                        await remove.mutateAsync(search.id);
                                        onDelete();
                                    } catch (e) {
                                        toast.error(buildError(e as AppError));
                                    }
                                })
                            }
                        >
                            Delete search
                        </PopoverMenuItem>
                    </PopoverMenuContent>
                </PopoverMenu>
            </div>
            <div className="mt-0.5 flex items-center gap-1.5 text-[11px] text-slate-500">
                <span>{search.result_count.toLocaleString()} leads</span>
                <span className="text-slate-300">·</span>
                <span className="truncate">{provider}</span>
                <span className="ml-auto shrink-0 text-slate-400">{timeAgo(search.last_run_at)}</span>
            </div>
        </motion.li>
    );
}
