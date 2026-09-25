// Floating bar for a multi-row selection in the conversation list.
//
// Same shape as the contacts table's: fixed to the bottom centre, the count on
// the left, the actions in the middle, Clear on the right. Fixed rather than
// absolute, so it cannot park itself below the fold of a long list.

import React from "react";
import { AnimatePresence, motion } from "framer-motion";
import {
    ArchiveIcon,
    CheckIcon,
    InboxIcon,
    Loader2Icon,
    MailCheckIcon,
    MailOpenIcon,
    MoonIcon,
    TrashIcon,
} from "lucide-react";

import {
    PopoverMenu,
    PopoverMenuContent,
    PopoverMenuItem,
    PopoverMenuLabel,
    PopoverMenuTrigger,
} from "@/components/ui/popover-menu";
import { SNOOZE_PRESETS } from "@/lib/unibox/snooze";
import type { ConversationActions } from "@/hooks/useConversationActions";
import { cn } from "@/lib/utils";

interface SelectionBarProps {
    threadIds: string[];
    actions: ConversationActions;
    /** The scope the rows are listed in, which decides the filing direction. */
    scope?: string;
    onClear: () => void;
}

export function SelectionBar({
    threadIds,
    actions,
    scope,
    onClear,
}: SelectionBarProps) {
    const count = threadIds.length;
    // Every action clears the selection: the rows it applied to have left the
    // list, and a count that outlives its rows is a lie.
    const run = React.useCallback(
        (fn: () => void | Promise<void>) => {
            void Promise.resolve(fn()).finally(onClear);
        },
        [onClear],
    );

    const filed = scope === "archive" || scope === "trash";
    const snoozedScope = scope === "snoozed";

    return (
        // Centred by a full-width track rather than a translate, which the
        // motion transform would overwrite.
        <div className="fixed inset-x-0 bottom-4 z-30 flex justify-center pointer-events-none">
            <motion.div
                role="toolbar"
                aria-label="Selection actions"
                initial={{ opacity: 0, y: 16, scale: 0.97 }}
                animate={{ opacity: 1, y: 0, scale: 1 }}
                exit={{ opacity: 0, y: 12, scale: 0.97, transition: { duration: 0.14 } }}
                transition={{ type: "spring", stiffness: 520, damping: 34 }}
                className="pointer-events-auto flex items-center max-w-[calc(100vw-16px)] flex-wrap justify-center md:max-w-none md:flex-nowrap gap-1.5 rounded-md border border-slate-200 bg-white shadow-[0_6px_20px_-4px_rgba(15,23,42,0.12),0_2px_4px_rgba(15,23,42,0.04)] px-2 py-1.5"
            >
                <div className="inline-flex items-center gap-1.5 px-2 h-7 rounded bg-sky-50 text-sky-700 text-[12px] font-medium">
                    <CheckIcon className="w-3 h-3" />
                    <span className="sr-only">{count.toLocaleString()} selected</span>
                    {/* The count ticks rather than jumps when rows are added. */}
                    <span aria-hidden className="inline-flex items-center gap-1">
                        <span className="relative inline-flex overflow-hidden tabular-nums">
                            <AnimatePresence mode="popLayout" initial={false}>
                                <motion.span
                                    key={count}
                                    initial={{ y: 8, opacity: 0 }}
                                    animate={{ y: 0, opacity: 1 }}
                                    exit={{ y: -8, opacity: 0 }}
                                    transition={{ duration: 0.16, ease: [0.16, 1, 0.3, 1] }}
                                >
                                    {count.toLocaleString()}
                                </motion.span>
                            </AnimatePresence>
                        </span>
                        selected
                    </span>
                </div>

                <BarButton
                    icon={<MailCheckIcon className="w-3 h-3" />}
                    label="Mark read"
                    onClick={() => run(() => actions.setSeen(threadIds, true))}
                />
                <BarButton
                    icon={<MailOpenIcon className="w-3 h-3" />}
                    label="Mark unread"
                    onClick={() => run(() => actions.setSeen(threadIds, false))}
                />

                {snoozedScope ? (
                    <BarButton
                        icon={<MoonIcon className="w-3 h-3" />}
                        label="Un-snooze"
                        onClick={() => run(() => actions.unsnooze(threadIds))}
                    />
                ) : (
                    <PopoverMenu side="top" align="center">
                        <PopoverMenuTrigger asChild>
                            <button
                                type="button"
                                className="h-7 px-2.5 rounded text-[12px] text-slate-700 hover:text-slate-900 hover:bg-slate-100 font-medium inline-flex items-center gap-1.5 transition-colors"
                            >
                                <MoonIcon className="w-3 h-3" />
                                <span className="hidden sm:inline">Snooze</span>
                            </button>
                        </PopoverMenuTrigger>
                        <PopoverMenuContent>
                            <PopoverMenuLabel>
                                Snooze {count.toLocaleString()} until
                            </PopoverMenuLabel>
                            {SNOOZE_PRESETS.map((p) => (
                                <PopoverMenuItem
                                    key={p.label}
                                    onSelect={() => run(() => actions.snooze(threadIds, p.until()))}
                                >
                                    {p.label}
                                </PopoverMenuItem>
                            ))}
                        </PopoverMenuContent>
                    </PopoverMenu>
                )}

                <BarButton
                    icon={
                        filed ? (
                            <InboxIcon className="w-3 h-3" />
                        ) : (
                            <ArchiveIcon className="w-3 h-3" />
                        )
                    }
                    label={filed ? "Move to inbox" : "Archive"}
                    busy={actions.filing}
                    onClick={() =>
                        run(() => actions.file(threadIds, filed ? "inbox" : "archive"))
                    }
                />

                {scope !== "trash" && (
                    <BarButton
                        icon={<TrashIcon className="w-3 h-3" />}
                        label="Delete"
                        danger
                        busy={actions.filing}
                        onClick={() => run(() => actions.file(threadIds, "trash"))}
                    />
                )}

                <div className="h-4 w-px bg-slate-200" />
                <button
                    type="button"
                    onClick={onClear}
                    className="h-7 px-2.5 rounded text-[12px] text-slate-500 hover:text-slate-900 transition-colors"
                >
                    Clear
                </button>
            </motion.div>
        </div>
    );
}

function BarButton({
    icon,
    label,
    onClick,
    busy,
    danger,
}: {
    icon: React.ReactNode;
    label: string;
    onClick: () => void;
    busy?: boolean;
    danger?: boolean;
}) {
    return (
        <button
            type="button"
            onClick={onClick}
            disabled={busy}
            title={label}
            className={cn(
                "h-7 px-2.5 rounded text-[12px] font-medium inline-flex items-center gap-1.5 transition-colors disabled:opacity-60",
                danger
                    ? "text-red-600 hover:text-white hover:bg-red-600"
                    : "text-slate-700 hover:text-slate-900 hover:bg-slate-100",
            )}
        >
            {busy ? <Loader2Icon className="w-3 h-3 animate-spin" /> : icon}
            <span className="hidden sm:inline">{label}</span>
        </button>
    );
}
