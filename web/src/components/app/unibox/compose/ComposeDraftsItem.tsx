// ComposeDraftsItem — the "Drafts (n)" row under the rail's Compose button.
// Opens a small popover listing autosaved compose drafts; clicking one resumes
// it in the compose window, the trash deletes it. Renders nothing while there
// are no drafts, so the rail stays clean for most users.

import React from "react";
import { AnimatePresence, motion } from "framer-motion";
import { FileTextIcon, Trash2Icon } from "lucide-react";
import useComposeDrafts, { useDeleteComposeDraft } from "@/lib/api/hooks/app/unibox/useComposeDrafts";
import type { ComposeDraft } from "@/lib/api/client/app/unibox/composeDrafts";
import { useComposeStore } from "@/hooks/useComposeStore";
import useClickOutside from "@/hooks/useClickOutside";

function draftTitle(d: ComposeDraft): string {
    return d.subject.trim() || d.to[0] || "(no subject)";
}

function formatWhen(iso: string): string {
    const d = new Date(iso);
    const now = new Date();
    const sameDay =
        d.getFullYear() === now.getFullYear() &&
        d.getMonth() === now.getMonth() &&
        d.getDate() === now.getDate();
    if (sameDay) return d.toLocaleTimeString(undefined, { hour: "2-digit", minute: "2-digit" });
    return d.toLocaleDateString(undefined, { month: "short", day: "numeric" });
}

export default function ComposeDraftsItem() {
    const [open, setOpen] = React.useState(false);
    const boxRef = React.useRef<HTMLDivElement>(null);
    useClickOutside(boxRef, () => setOpen(false));

    const draftsQ = useComposeDrafts();
    const deleteMut = useDeleteComposeDraft();
    const drafts = draftsQ.data ?? [];

    if (drafts.length === 0) return null;

    return (
        <div ref={boxRef} className="relative mt-1">
            <button
                type="button"
                onClick={() => setOpen((o) => !o)}
                className="w-full h-7 px-2 rounded-md inline-flex items-center gap-2 text-[12px] text-slate-600 hover:text-slate-900 hover:bg-slate-100 transition-colors"
            >
                <FileTextIcon className="w-3.5 h-3.5 text-slate-400" />
                Drafts
                <span className="ml-auto font-mono text-[10.5px] text-slate-400 tabular-nums">
                    {drafts.length}
                </span>
            </button>

            <AnimatePresence>
                {open && (
                    <motion.div
                        initial={{ opacity: 0, y: -4 }}
                        animate={{ opacity: 1, y: 0 }}
                        exit={{ opacity: 0, y: -4 }}
                        transition={{ duration: 0.12, ease: [0.16, 1, 0.3, 1] }}
                        className="absolute left-0 right-0 top-full mt-1 z-40 rounded-lg border border-slate-200 bg-white shadow-xl overflow-hidden"
                    >
                        <div className="max-h-64 overflow-y-auto">
                            {drafts.map((d) => (
                                <div
                                    key={d.id}
                                    className="group flex items-center gap-1 pr-1 hover:bg-slate-50 transition-colors"
                                >
                                    <button
                                        type="button"
                                        onClick={() => {
                                            useComposeStore.getState().openDraft(d);
                                            setOpen(false);
                                        }}
                                        className="flex-1 min-w-0 px-2.5 py-1.5 text-left"
                                    >
                                        <span className="flex items-baseline gap-1.5 min-w-0">
                                            <span className="text-[11.5px] font-medium text-slate-800 truncate">
                                                {draftTitle(d)}
                                            </span>
                                            <span className="ml-auto font-mono text-[9.5px] text-slate-400 tabular-nums shrink-0">
                                                {formatWhen(d.updated_at)}
                                            </span>
                                        </span>
                                        {d.body.trim() && (
                                            <span className="block text-[10.5px] text-slate-400 truncate leading-snug">
                                                {d.body.replace(/\s+/g, " ").trim().slice(0, 80)}
                                            </span>
                                        )}
                                    </button>
                                    <button
                                        type="button"
                                        onClick={() => deleteMut.mutate(d.id)}
                                        aria-label={`Delete draft ${draftTitle(d)}`}
                                        className="size-6 shrink-0 rounded inline-flex items-center justify-center text-slate-300 opacity-100 md:opacity-0 md:group-hover:opacity-100 hover:text-rose-600 hover:bg-rose-50 transition-all"
                                    >
                                        <Trash2Icon className="w-3 h-3" />
                                    </button>
                                </div>
                            ))}
                        </div>
                    </motion.div>
                )}
            </AnimatePresence>
        </div>
    );
}
