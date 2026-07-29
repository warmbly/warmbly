// BulkTagPopover — the "Tags" action on the mailboxes selection bar.
// Anchored popover in the TagSelector/CategoryPicker visual language
// (search header + checkbox-square rows with colored dots) with an
// Add | Remove toggle and inline tag creation; Apply bulk-patches the
// selected mailboxes via PATCH /emails/tags. Row selection is kept
// after applying so users can chain add/remove actions.

import React from "react";
import { AnimatePresence, motion } from "framer-motion";
import { CheckIcon, Loader2Icon, PlusIcon, TagIcon } from "lucide-react";
import toast from "react-hot-toast";
import { useQueryClient } from "@tanstack/react-query";
import { useUserProfile } from "@/hooks/context/user";
import useClickOutside from "@/hooks/useClickOutside";
import useFlipPlacement from "@/hooks/useFlipPlacement";
import useBulkTagEmails from "@/lib/api/hooks/app/emails/useBulkTagEmails";
import createTag from "@/lib/api/client/app/tags/createTag";
import type User from "@/lib/api/models/auth/User";

// Same 8 hexes as the tags editor palette; inline creates cycle through
// them so consecutive new tags don't all share one color.
const PALETTE = [
    "#94a3b8", // slate
    "#38bdf8", // sky
    "#10b981", // emerald
    "#f59e0b", // amber
    "#ef4444", // red
    "#a855f7", // violet
    "#ec4899", // pink
    "#14b8a6", // teal
];

export default function BulkTagPopover({ ids }: { ids: string[] }) {
    const profile = useUserProfile();
    const queryClient = useQueryClient();
    const bulk = useBulkTagEmails();

    const [open, setOpen] = React.useState(false);
    const [mode, setMode] = React.useState<"add" | "remove">("add");
    const [query, setQuery] = React.useState("");
    const [picked, setPicked] = React.useState<string[]>([]);
    const [creating, setCreating] = React.useState(false);

    const ref = React.useRef<HTMLDivElement>(null);
    const triggerRef = React.useRef<HTMLButtonElement>(null);
    useClickOutside(ref, () => setOpen(false));
    // ~330px: toggle + search header + max-h-48 list + footer.
    const placement = useFlipPlacement(triggerRef, open, 330);

    const tags = React.useMemo(
        () => [...(profile?.user.tags ?? [])].sort((a, b) => a.position - b.position),
        [profile?.user.tags],
    );

    const filtered = React.useMemo(() => {
        const q = query.trim().toLowerCase();
        if (!q) return tags;
        return tags.filter((t) => t.title.toLowerCase().includes(q));
    }, [tags, query]);

    const queryMatchesExisting = React.useMemo(() => {
        const q = query.trim().toLowerCase();
        if (!q) return true;
        return tags.some((t) => t.title.toLowerCase() === q);
    }, [tags, query]);

    function togglePick(id: string) {
        setPicked((p) => (p.includes(id) ? p.filter((x) => x !== id) : [...p, id]));
    }

    async function createAndPick() {
        const title = query.trim();
        if (!title || creating) return;
        setCreating(true);
        try {
            const t = await createTag(title, PALETTE[tags.length % PALETTE.length]);
            queryClient.setQueryData<User>(["auth", "me"], (old) =>
                old ? { ...old, tags: [...old.tags, t] } : old,
            );
            setPicked((p) => [...p, t.id]);
            setQuery("");
        } catch (err) {
            toast.error(err instanceof Error ? err.message : "Failed to create tag");
        } finally {
            setCreating(false);
        }
    }

    async function apply() {
        if (picked.length === 0 || bulk.isPending) return;
        const n = ids.length;
        try {
            await bulk.mutateAsync({
                emailIds: ids,
                addTags: mode === "add" ? picked : [],
                removeTags: mode === "remove" ? picked : [],
            });
            toast.success(
                mode === "add"
                    ? `Tags added to ${n} mailbox${n > 1 ? "es" : ""}`
                    : `Tags removed from ${n} mailbox${n > 1 ? "es" : ""}`,
            );
            setOpen(false);
            setPicked([]);
            setQuery("");
        } catch {
            toast.error("Couldn't update tags");
        }
    }

    return (
        <div ref={ref} className="relative">
            <button
                type="button"
                ref={triggerRef}
                onClick={() => {
                    if (!open) {
                        setMode("add");
                        setQuery("");
                        setPicked([]);
                    }
                    setOpen((o) => !o);
                }}
                className="inline-flex items-center gap-1.5 h-7 px-2.5 rounded text-[12px] font-medium text-slate-600 hover:bg-slate-100 transition-colors"
            >
                <TagIcon className="w-3.5 h-3.5" />
                Tags
            </button>

            <AnimatePresence>
                {open && (
                    <div
                        className={`absolute left-1/2 -translate-x-1/2 z-40 w-60 ${
                            placement === "top" ? "bottom-full mb-1.5" : "top-full mt-1.5"
                        }`}
                    >
                        <motion.div
                            initial={{ opacity: 0, y: placement === "top" ? 4 : -4 }}
                            animate={{ opacity: 1, y: 0 }}
                            exit={{ opacity: 0, y: placement === "top" ? 4 : -4 }}
                            transition={{ duration: 0.12 }}
                            className="rounded-md border border-slate-200 bg-white shadow-[0_12px_32px_-8px_rgba(15,23,42,0.18)] overflow-hidden"
                        >
                            <div className="p-1.5 border-b border-slate-200">
                                <div className="flex rounded-md bg-slate-100 p-0.5">
                                    {(["add", "remove"] as const).map((m) => (
                                        <button
                                            key={m}
                                            type="button"
                                            onClick={() => setMode(m)}
                                            className={`flex-1 h-6 rounded text-[11.5px] font-medium transition-colors ${
                                                mode === m
                                                    ? "bg-white text-slate-900 shadow-sm"
                                                    : "text-slate-500 hover:text-slate-700"
                                            }`}
                                        >
                                            {m === "add" ? "Add" : "Remove"}
                                        </button>
                                    ))}
                                </div>
                            </div>
                            <div className="px-2 py-1.5 border-b border-slate-200">
                                <input
                                    value={query}
                                    onChange={(e) => setQuery(e.target.value)}
                                    placeholder="Search or create…"
                                    autoFocus
                                    className="w-full h-5 bg-transparent text-[12px] text-slate-900 placeholder:text-slate-400 outline-none"
                                />
                            </div>
                            <div className="max-h-48 overflow-y-auto py-1">
                                {filtered.length === 0 && queryMatchesExisting && (
                                    <div className="px-3 py-3 text-[11.5px] text-slate-400 text-center">
                                        {tags.length === 0 ? "No tags yet." : "No matches."}
                                    </div>
                                )}
                                {filtered.map((t) => {
                                    const checked = picked.includes(t.id);
                                    return (
                                        <button
                                            key={t.id}
                                            type="button"
                                            onClick={() => togglePick(t.id)}
                                            className="w-full px-2.5 h-7 flex items-center gap-2 text-[12px] text-slate-700 hover:bg-slate-100 transition-colors"
                                        >
                                            <span
                                                className={`size-3.5 rounded border flex items-center justify-center transition-colors shrink-0 ${
                                                    checked ? "border-slate-900 bg-slate-900" : "border-slate-300 bg-white"
                                                }`}
                                            >
                                                {checked && <CheckIcon className="w-2 h-2 text-white" />}
                                            </span>
                                            <span className="size-2.5 rounded-full shrink-0" style={{ backgroundColor: t.color }} />
                                            <span className="truncate">{t.title}</span>
                                        </button>
                                    );
                                })}
                                {query.trim() && !queryMatchesExisting && (
                                    <button
                                        type="button"
                                        onClick={createAndPick}
                                        disabled={creating}
                                        className="w-full px-2.5 h-7 flex items-center gap-2 text-[12px] text-slate-900 font-medium hover:bg-sky-50 border-t border-slate-100 transition-colors"
                                    >
                                        {creating ? (
                                            <Loader2Icon className="w-3 h-3 animate-spin text-slate-400" />
                                        ) : (
                                            <PlusIcon className="w-3 h-3 text-sky-600" />
                                        )}
                                        Create "{query.trim()}"
                                    </button>
                                )}
                            </div>
                            <div className="p-1.5 border-t border-slate-200">
                                <button
                                    type="button"
                                    onClick={apply}
                                    disabled={picked.length === 0 || bulk.isPending}
                                    className="w-full h-7 rounded-md bg-sky-600 hover:bg-sky-700 text-white text-[12px] font-medium inline-flex items-center justify-center gap-1.5 transition-colors disabled:opacity-50"
                                >
                                    {bulk.isPending && <Loader2Icon className="w-3 h-3 animate-spin" />}
                                    {mode === "add"
                                        ? `Add to ${ids.length} mailbox${ids.length > 1 ? "es" : ""}`
                                        : `Remove from ${ids.length} mailbox${ids.length > 1 ? "es" : ""}`}
                                </button>
                            </div>
                        </motion.div>
                    </div>
                )}
            </AnimatePresence>
        </div>
    );
}
