// TagSelector — mailbox tag multi-select. Mirrors the contacts
// CategoryPicker visual language (bordered chip box + framer-motion
// dropdown with a search header + checkbox-square rows), so tag and
// category pickers look identical across the app.
//
// The dropdown renders through the PopoverMenu portal so it escapes
// overflow-hidden / overflow-y-auto ancestors (e.g. the campaign
// dialog body) instead of being clipped by them.
//
// Tags live on the user profile (no inline create here); a "Manage
// tags" footer opens the tag editor.

import React from "react";
import { CheckIcon, PlusIcon, XIcon, SlidersHorizontalIcon } from "lucide-react";
import { useUserProfile } from "@/hooks/context/user";
import {
    PopoverMenu,
    PopoverMenuContent,
    PopoverMenuTrigger,
} from "@/components/ui/popover-menu";

export default function TagSelector({
    onAdd,
    onRemove,
    selected,
}: {
    onAdd: (id: string) => void;
    onRemove: (id: string) => void;
    selected: string[];
}) {
    const profile = useUserProfile();
    const tags = React.useMemo(
        () => [...(profile?.user.tags ?? [])].sort((a, b) => a.position - b.position),
        [profile?.user.tags],
    );

    const [open, setOpen] = React.useState(false);
    const [query, setQuery] = React.useState("");

    const byId = React.useMemo(() => {
        const m = new Map<string, (typeof tags)[number]>();
        for (const t of tags) m.set(t.id, t);
        return m;
    }, [tags]);

    const selectedChips = selected
        .map((id) => byId.get(id))
        .filter((t): t is NonNullable<typeof t> => !!t);

    const filtered = React.useMemo(() => {
        const q = query.trim().toLowerCase();
        if (!q) return tags;
        return tags.filter((t) => t.title.toLowerCase().includes(q));
    }, [tags, query]);

    function toggle(id: string) {
        if (selected.includes(id)) onRemove(id);
        else onAdd(id);
    }

    return (
        <PopoverMenu open={open} onOpenChange={setOpen}>
            <PopoverMenuTrigger asChild>
                <div className="rounded-md border border-slate-200 bg-white min-h-[34px] cursor-pointer">
                    {selectedChips.length === 0 ? (
                        <div className="px-3 py-2 text-[11.5px] text-slate-400 hover:text-slate-600">
                            Click to add tags…
                        </div>
                    ) : (
                        <div className="px-2 py-2 flex flex-wrap gap-1">
                            {selectedChips.map((t) => (
                                <span
                                    key={t.id}
                                    className="inline-flex items-center gap-1 h-5 pl-1.5 pr-1 rounded text-[11px] font-medium"
                                    style={{
                                        backgroundColor: hexToRgba(t.color, 0.12),
                                        color: t.color,
                                        border: `1px solid ${hexToRgba(t.color, 0.25)}`,
                                    }}
                                >
                                    <span className="size-2 rounded-full shrink-0" style={{ backgroundColor: t.color }} />
                                    <span className="truncate">{t.title}</span>
                                    <button
                                        type="button"
                                        onClick={(e) => {
                                            e.stopPropagation();
                                            onRemove(t.id);
                                        }}
                                        className="opacity-70 hover:opacity-100 p-1 -m-1 md:p-0 md:m-0"
                                        aria-label={`Remove ${t.title}`}
                                    >
                                        <XIcon className="w-2.5 h-2.5" />
                                    </button>
                                </span>
                            ))}
                            <button
                                type="button"
                                className="inline-flex items-center gap-1 h-5 px-1.5 rounded text-[11px] font-medium border border-dashed border-slate-300 text-slate-500 hover:border-slate-400 hover:text-slate-700"
                            >
                                <PlusIcon className="w-2.5 h-2.5" />
                                Add
                            </button>
                        </div>
                    )}
                </div>
            </PopoverMenuTrigger>
            <PopoverMenuContent matchTriggerWidth minWidth={220} className="py-0">
                <div className="px-2 py-1.5 border-b border-slate-200">
                    <input
                        value={query}
                        onChange={(e) => setQuery(e.target.value)}
                        placeholder="Search…"
                        autoFocus
                        className="w-full h-5 bg-transparent text-[12px] text-slate-900 placeholder:text-slate-400 outline-none"
                    />
                </div>
                <div className="max-h-56 overflow-y-auto py-1">
                    {filtered.length === 0 && (
                        <div className="px-3 py-3 text-[11.5px] text-slate-400 text-center">
                            {tags.length === 0 ? "No tags yet." : "No matches."}
                        </div>
                    )}
                    {filtered.map((t) => {
                        const checked = selected.includes(t.id);
                        return (
                            <button
                                key={t.id}
                                type="button"
                                onClick={() => toggle(t.id)}
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
                    <button
                        type="button"
                        onClick={() => {
                            profile?.setTagsEdit(true);
                            setOpen(false);
                        }}
                        className="w-full px-2.5 h-7 flex items-center gap-2 text-[12px] text-slate-500 hover:bg-slate-50 hover:text-slate-800 border-t border-slate-100 transition-colors"
                    >
                        <SlidersHorizontalIcon className="w-3 h-3" />
                        Manage tags
                    </button>
                </div>
            </PopoverMenuContent>
        </PopoverMenu>
    );
}

// hexToRgba converts "#rrggbb" to "rgba(...)"; non-hex falls back to a
// slate tint so the chip stays visible.
function hexToRgba(hex: string, alpha: number): string {
    const m = /^#([0-9a-f]{6})$/i.exec(hex);
    if (!m) return `rgba(100,116,139,${alpha})`;
    const v = m[1];
    return `rgba(${parseInt(v.slice(0, 2), 16)},${parseInt(v.slice(2, 4), 16)},${parseInt(v.slice(4, 6), 16)},${alpha})`;
}
