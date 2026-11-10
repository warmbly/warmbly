import { AnimatePresence, motion } from "framer-motion";
import { BookmarkIcon, CheckIcon, ChevronDownIcon, ChevronUpIcon, DownloadIcon, Loader2Icon, MoreHorizontalIcon, SparklesIcon, TagIcon, Trash2Icon, UserPlusIcon, XIcon, EyeOffIcon, SendIcon } from "lucide-react";
import { useState } from "react";
import { Toggle } from "@/components/app/campaigns/preferences/components/CampaignPreferenceBoolBox";
import { PopoverMenu, PopoverMenuContent, PopoverMenuItem, PopoverMenuSeparator, PopoverMenuTrigger } from "@/components/ui/popover-menu";
import type { LeadStatus } from "@/lib/api/models/app/leads/Lead";
import { fmtUsdCost } from "@/lib/money";

// Whether Find people and emails may search the web, and what one search costs.
export interface WebSearchChoice {
    on: boolean;
    costUsd: number;
    onChange: (on: boolean) => void;
}

// The options arrow beside Find people and emails: the web search switch.
export function WebSearchOption({ choice, side }: { choice: WebSearchChoice; side: "top" | "bottom" }) {
    return (
        <PopoverMenu side={side} align="center">
            <PopoverMenuTrigger asChild>
                <button
                    type="button"
                    aria-label="Find people and emails options"
                    className="-ml-1 h-7 px-1 rounded-md text-slate-500 hover:text-slate-900 hover:bg-slate-100 inline-flex items-center"
                >
                    {side === "top" ? <ChevronUpIcon className="size-3.5" /> : <ChevronDownIcon className="size-3.5" />}
                </button>
            </PopoverMenuTrigger>
            <PopoverMenuContent role="dialog" aria-label="Find people and emails options" minWidth={280}>
                <div className="flex items-start gap-3 px-2.5 py-2">
                    <div className="min-w-0 flex-1">
                        <p className="text-[12.5px] font-medium text-slate-800">Search the web</p>
                        <p className="mt-0.5 text-[11.5px] leading-snug text-slate-500">
                            Looks for a missing website, and for addresses a blocked site shows elsewhere.
                            {choice.costUsd > 0 && ` ${fmtUsdCost(choice.costUsd)} for each search.`}
                        </p>
                    </div>
                    <Toggle value={choice.on} onChange={choice.onChange} ariaLabel="Search the web" />
                </div>
            </PopoverMenuContent>
        </PopoverMenu>
    );
}

export default function LeadSelectionBar({
    count,
    enrichCost,
    enriching,
    webSearch,
    onEnrich,
    onAddToContacts,
    onExport,
    onStatus,
    onTag,
    onDelete,
    onClear,
}: {
    count: number;
    enrichCost: number;
    enriching: boolean;
    // Set only when the workspace has a web search the run can use.
    webSearch?: WebSearchChoice;
    onEnrich: () => void;
    onAddToContacts: () => void;
    onExport: () => void;
    onStatus: (s: LeadStatus) => void;
    onTag: (tag: string) => void;
    onDelete: () => void;
    onClear: () => void;
}) {
    const [tag, setTag] = useState("");
    return (
        <AnimatePresence>
            {count > 0 && (
                <motion.div
                    initial={{ y: 16, opacity: 0 }}
                    animate={{ y: 0, opacity: 1 }}
                    exit={{ y: 16, opacity: 0 }}
                    transition={{ duration: 0.16 }}
                    className="fixed bottom-4 left-1/2 -translate-x-1/2 z-30 flex items-center max-w-[calc(100vw-16px)] flex-wrap justify-center md:max-w-none md:flex-nowrap gap-1.5 rounded-md border border-slate-200 bg-white shadow-[0_6px_20px_-4px_rgba(15,23,42,0.12),0_2px_4px_rgba(15,23,42,0.04)] px-2 py-1.5"
                >
                    <div className="inline-flex items-center gap-1.5 px-2 h-7 rounded bg-sky-50 text-sky-700 text-[12px] font-medium">
                        <CheckIcon className="size-3" />
                        <span>{count.toLocaleString()} selected</span>
                    </div>
                    <Action onClick={onEnrich} disabled={enriching} title={enrichCost > 0 ? `${fmtUsdCost(enrichCost)} for each website read; unreachable sites are refunded` : undefined}>
                        {enriching ? <Loader2Icon className="size-3.5 animate-spin" /> : <SparklesIcon className="size-3.5" />}
                        Find people and emails
                    </Action>
                    {webSearch && <WebSearchOption choice={webSearch} side="top" />}
                    <Action onClick={onAddToContacts}>
                        <UserPlusIcon className="size-3.5" />
                        Add to contacts
                    </Action>
                    <Action onClick={onExport}>
                        <DownloadIcon className="size-3.5" />
                        Export
                    </Action>
                    <PopoverMenu side="top" align="center" onOpenChange={(o) => !o && setTag("")}>
                        <PopoverMenuTrigger asChild>
                            <button type="button" aria-label="More actions" className="h-7 px-2 rounded-md text-slate-600 hover:text-slate-900 hover:bg-slate-100 inline-flex items-center">
                                <MoreHorizontalIcon className="size-3.5" />
                            </button>
                        </PopoverMenuTrigger>
                        <PopoverMenuContent minWidth={210}>
                            <PopoverMenuItem icon={<BookmarkIcon className="size-3.5" />} onSelect={() => onStatus("saved")}>
                                Mark as saved
                            </PopoverMenuItem>
                            <PopoverMenuItem icon={<SendIcon className="size-3.5" />} onSelect={() => onStatus("contacted")}>
                                Mark as contacted
                            </PopoverMenuItem>
                            <PopoverMenuItem icon={<EyeOffIcon className="size-3.5" />} onSelect={() => onStatus("dismissed")}>
                                Dismiss
                            </PopoverMenuItem>
                            <PopoverMenuSeparator />
                            <form
                                className="px-2 py-1.5 flex items-center gap-1.5"
                                onSubmit={(e) => {
                                    e.preventDefault();
                                    if (tag.trim()) onTag(tag.trim());
                                    setTag("");
                                }}
                            >
                                <TagIcon className="size-3.5 text-slate-400" />
                                <input
                                    value={tag}
                                    maxLength={40}
                                    onChange={(e) => setTag(e.target.value)}
                                    placeholder="Add a tag, press Enter"
                                    className="h-6 min-w-0 flex-1 rounded border border-slate-200 px-1.5 text-[12px] outline-none focus:border-sky-400"
                                />
                            </form>
                            <PopoverMenuSeparator />
                            <PopoverMenuItem danger icon={<Trash2Icon className="size-3.5" />} onSelect={onDelete}>
                                Delete leads
                            </PopoverMenuItem>
                        </PopoverMenuContent>
                    </PopoverMenu>
                    <button type="button" aria-label="Clear selection" onClick={onClear} className="size-7 rounded-md text-slate-400 hover:text-slate-900 hover:bg-slate-100 inline-flex items-center justify-center">
                        <XIcon className="size-3.5" />
                    </button>
                </motion.div>
            )}
        </AnimatePresence>
    );
}

function Action({ onClick, disabled, title, children }: { onClick: () => void; disabled?: boolean; title?: string; children: React.ReactNode }) {
    return (
        <button
            type="button"
            onClick={onClick}
            disabled={disabled}
            title={title}
            className="h-7 px-2.5 rounded-md text-[12px] font-medium text-slate-700 hover:text-slate-900 hover:bg-slate-100 inline-flex items-center gap-1.5 disabled:opacity-50"
        >
            {children}
        </button>
    );
}
