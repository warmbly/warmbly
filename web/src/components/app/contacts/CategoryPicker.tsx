// CategoryPicker — multi-select dropdown for contact categories.
//
// Used by the bulk edit panel, the edit modal, the new-contact modal,
// the contact filters sheet, and the import wizard. Single source of
// truth for "let the user pick + create category chips".
//
// Behaviour:
//   - Reads the user's existing categories from UserContext.
//   - Type-ahead filter.
//   - "Create '<query>'" appears when the query is non-empty and
//     doesn't match an existing category — POSTs to /categories and
//     auto-selects the new one.
//
// Visual language matches CampaignPicker in ContactsEditBulk: chip
// row + dropdown panel + keyboard navigation.

import React from "react";
import { AnimatePresence, motion } from "framer-motion";
import { CheckIcon, Loader2Icon, PlusIcon, XIcon } from "lucide-react";
import toast from "react-hot-toast";

import { useUserProfile } from "@/hooks/context/user";
import useClickOutside from "@/hooks/useClickOutside";
import useFlipPlacement from "@/hooks/useFlipPlacement";
import useCreateCategory from "@/lib/api/hooks/app/categories/useCreateCategory";
import type Category from "@/lib/api/models/app/Category";

interface Props {
    // Selected ids — kept as ids so the consumer can store them in the
    // request payload directly without a name-id resolution step.
    value: string[];
    onChange: (next: string[]) => void;

    placeholder?: string;
    className?: string;
    // Disable inline category creation. Useful in contexts where the
    // user shouldn't be inventing tags (e.g. legacy import flows).
    allowCreate?: boolean;
}

export default function CategoryPicker({
    value,
    onChange,
    placeholder = "Click to add categories…",
    className,
    allowCreate = true,
}: Props) {
    const { user } = useUserProfile();
    const categories = user.categories ?? [];
    const createCategory = useCreateCategory();

    const [open, setOpen] = React.useState(false);
    const [query, setQuery] = React.useState("");
    const ref = React.useRef<HTMLDivElement>(null);
    const triggerRef = React.useRef<HTMLDivElement>(null);
    useClickOutside(ref, () => setOpen(false));
    // ~270px: 33px search input + 56 max-h list (224px) + borders.
    const placement = useFlipPlacement(triggerRef, open, 270);

    // Map id → category for chip rendering. We don't render orphaned
    // ids — if the underlying category was deleted, the chip just
    // disappears on next render, which is the right behaviour.
    const byId = React.useMemo(() => {
        const m = new Map<string, Category>();
        for (const c of categories) m.set(c.id, c);
        return m;
    }, [categories]);

    const selectedChips = value
        .map((id) => byId.get(id))
        .filter((c): c is Category => !!c);

    const filtered = React.useMemo(() => {
        const q = query.trim().toLowerCase();
        if (!q) return categories;
        return categories.filter((c) => c.title.toLowerCase().includes(q));
    }, [categories, query]);

    const queryMatchesExisting = React.useMemo(() => {
        const q = query.trim().toLowerCase();
        if (!q) return true;
        return categories.some((c) => c.title.toLowerCase() === q);
    }, [categories, query]);

    function toggle(id: string) {
        if (value.includes(id)) {
            onChange(value.filter((x) => x !== id));
        } else {
            onChange([...value, id]);
        }
    }

    async function createAndPick() {
        const title = query.trim();
        if (!title) return;
        try {
            const c = await createCategory.mutateAsync(title);
            onChange([...value, c.id]);
            setQuery("");
        } catch (err) {
            toast.error(err instanceof Error ? err.message : "Failed to create category");
        }
    }

    return (
        <div ref={ref} className={`relative ${className ?? ""}`}>
            <div ref={triggerRef} className="rounded-md border border-slate-200 bg-white min-h-[34px]">
                {selectedChips.length === 0 ? (
                    <div
                        onClick={() => setOpen((o) => !o)}
                        className="px-3 py-2 text-[11.5px] text-slate-400 cursor-pointer hover:text-slate-600"
                    >
                        {placeholder}
                    </div>
                ) : (
                    <div className="px-2 py-2 flex flex-wrap gap-1">
                        {selectedChips.map((c) => (
                            <CategoryChip
                                key={c.id}
                                category={c}
                                onRemove={() => toggle(c.id)}
                            />
                        ))}
                        <button
                            type="button"
                            onClick={() => setOpen((o) => !o)}
                            className="inline-flex items-center gap-1 h-5 px-1.5 rounded text-[11px] font-medium border border-dashed border-slate-300 text-slate-500 hover:border-slate-400 hover:text-slate-700"
                        >
                            <PlusIcon className="w-2.5 h-2.5" />
                            Add
                        </button>
                    </div>
                )}
            </div>

            <AnimatePresence>
                {open && (
                    <motion.div
                        initial={{ opacity: 0, y: placement === "top" ? 4 : -4 }}
                        animate={{ opacity: 1, y: 0 }}
                        exit={{ opacity: 0, y: placement === "top" ? 4 : -4 }}
                        transition={{ duration: 0.12 }}
                        className={`absolute left-0 right-0 z-30 rounded-md border border-slate-200 bg-white shadow-[0_12px_32px_-8px_rgba(15,23,42,0.18)] overflow-hidden ${
                            placement === "top" ? "bottom-full mb-1" : "top-full mt-1"
                        }`}
                    >
                        <div className="px-2 py-1.5 border-b border-slate-200">
                            <input
                                value={query}
                                onChange={(e) => setQuery(e.target.value)}
                                placeholder={allowCreate ? "Search or create…" : "Search…"}
                                autoFocus
                                className="w-full h-5 bg-transparent text-[12px] text-slate-900 placeholder:text-slate-400 outline-none"
                            />
                        </div>
                        <div className="max-h-56 overflow-y-auto py-1">
                            {filtered.length === 0 && !allowCreate && (
                                <div className="px-3 py-3 text-[11.5px] text-slate-400 text-center">
                                    No categories.
                                </div>
                            )}
                            {filtered.map((c) => {
                                const checked = value.includes(c.id);
                                return (
                                    <button
                                        key={c.id}
                                        type="button"
                                        onClick={() => toggle(c.id)}
                                        className="w-full px-2.5 h-7 flex items-center gap-2 text-[12px] text-slate-700 hover:bg-slate-100 transition-colors"
                                    >
                                        <span
                                            className={`size-3.5 rounded border flex items-center justify-center transition-colors shrink-0 ${
                                                checked
                                                    ? "border-slate-900 bg-slate-900"
                                                    : "border-slate-300 bg-white"
                                            }`}
                                        >
                                            {checked && <CheckIcon className="w-2 h-2 text-white" />}
                                        </span>
                                        <span
                                            className="size-2.5 rounded-full shrink-0"
                                            style={{ backgroundColor: c.color }}
                                        />
                                        <span className="truncate">{c.title}</span>
                                    </button>
                                );
                            })}
                            {allowCreate && query.trim() && !queryMatchesExisting && (
                                <button
                                    type="button"
                                    onClick={createAndPick}
                                    disabled={createCategory.isPending}
                                    className="w-full px-2.5 h-7 flex items-center gap-2 text-[12px] text-slate-900 font-medium hover:bg-sky-50 border-t border-slate-100 transition-colors"
                                >
                                    {createCategory.isPending ? (
                                        <Loader2Icon className="w-3 h-3 animate-spin text-slate-400" />
                                    ) : (
                                        <PlusIcon className="w-3 h-3 text-sky-600" />
                                    )}
                                    Create "{query.trim()}"
                                </button>
                            )}
                        </div>
                    </motion.div>
                )}
            </AnimatePresence>
        </div>
    );
}

/**
 * Small read-only chip used in tables and detail panes. Exported so
 * other surfaces can render the same look without needing the full
 * picker.
 */
export function CategoryChip({
    category,
    onRemove,
    compact,
}: {
    category: { id: string; title: string; color: string };
    onRemove?: () => void;
    compact?: boolean;
}) {
    return (
        <span
            className={`inline-flex items-center gap-1 ${compact ? "h-4 pl-1 pr-1 text-[10px]" : "h-5 pl-1.5 pr-1 text-[11px]"} rounded font-medium`}
            style={{
                backgroundColor: hexToRgba(category.color, 0.12),
                color: category.color,
                border: `1px solid ${hexToRgba(category.color, 0.25)}`,
            }}
        >
            <span
                className={`${compact ? "size-1.5" : "size-2"} rounded-full shrink-0`}
                style={{ backgroundColor: category.color }}
            />
            <span className="truncate max-w-[72px] md:max-w-none">{category.title}</span>
            {onRemove && (
                <button
                    type="button"
                    onClick={(e) => {
                        e.stopPropagation();
                        onRemove();
                    }}
                    className="opacity-70 hover:opacity-100"
                    aria-label={`Remove ${category.title}`}
                >
                    <XIcon className="w-2.5 h-2.5" />
                </button>
            )}
        </span>
    );
}

// hexToRgba converts a "#rrggbb" string to "rgba(r,g,b,a)". Defensive:
// non-hex input falls back to a slate-ish tint so the chip is still
// visible.
function hexToRgba(hex: string, alpha: number): string {
    const m = /^#([0-9a-f]{6})$/i.exec(hex);
    if (!m) return `rgba(100,116,139,${alpha})`;
    const v = m[1];
    const r = parseInt(v.slice(0, 2), 16);
    const g = parseInt(v.slice(2, 4), 16);
    const b = parseInt(v.slice(4, 6), 16);
    return `rgba(${r},${g},${b},${alpha})`;
}
