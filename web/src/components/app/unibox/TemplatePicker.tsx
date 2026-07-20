// Template picker content, shared by the reply composer and the compose
// window. Rich rows (not PopoverMenuItem) so each entry can run two lines,
// with an inline search past a handful of templates, plus skeleton, empty,
// and error states. The host mounts it inside its own PopoverMenu.

import React from "react";
import { Link } from "react-router-dom";
import { FileTextIcon, SearchIcon, SettingsIcon, XIcon } from "lucide-react";
import type useTemplates from "@/lib/api/hooks/app/templates/useTemplates";
import type Template from "@/lib/api/models/app/templates/Template";

export default function TemplatePickerContent({
    query,
    onPick,
    onClose,
}: {
    query: ReturnType<typeof useTemplates>;
    onPick: (t: Template) => void;
    onClose: () => void;
}) {
    const [search, setSearch] = React.useState("");
    const all = React.useMemo(() => query.data ?? [], [query.data]);
    const showSearch = all.length > 5;
    const filtered = React.useMemo(() => {
        const q = search.trim().toLowerCase();
        if (!q) return all;
        return all.filter((t) => {
            const hay = `${t.name} ${t.subject} ${t.body_plain}`.toLowerCase();
            return hay.includes(q);
        });
    }, [all, search]);

    return (
        <div className="w-[340px] max-w-[92vw]">
            <div className="px-3 pt-2.5 pb-1 flex items-center justify-between gap-2">
                <span className="text-[10px] uppercase tracking-[0.14em] text-slate-400 font-semibold">
                    Templates
                </span>
                {all.length > 0 && (
                    <span className="font-mono text-[10px] text-slate-400 tabular-nums">
                        {filtered.length === all.length
                            ? all.length
                            : `${filtered.length}/${all.length}`}
                    </span>
                )}
            </div>

            {showSearch && (
                <div className="px-2 pb-2">
                    <div className="flex items-center gap-1.5 px-2 h-7 rounded-md border border-slate-200 bg-slate-50 focus-within:bg-white focus-within:border-sky-400 focus-within:ring-2 focus-within:ring-sky-100 transition-colors">
                        <SearchIcon className="w-3 h-3 text-slate-400 shrink-0" />
                        <input
                            type="text"
                            value={search}
                            onChange={(e) => setSearch(e.target.value)}
                            placeholder="Search templates"
                            autoFocus
                            className="flex-1 min-w-0 h-6 bg-transparent text-[12px] text-slate-900 placeholder:text-slate-400 outline-none"
                        />
                        {search && (
                            <button
                                type="button"
                                onClick={() => setSearch("")}
                                className="size-4 inline-flex items-center justify-center rounded text-slate-400 hover:text-slate-700 hover:bg-slate-200/60 shrink-0"
                                aria-label="Clear search"
                            >
                                <XIcon className="w-2.5 h-2.5" />
                            </button>
                        )}
                    </div>
                </div>
            )}

            {query.isPending ? (
                <div className="px-2 pb-2 space-y-1">
                    {[0, 1, 2].map((i) => (
                        <div
                            key={i}
                            className="h-12 rounded-md bg-slate-100/70 animate-pulse"
                        />
                    ))}
                </div>
            ) : query.isError ? (
                <div className="px-3 py-3 flex items-start gap-2 text-[11.5px] text-rose-600 bg-rose-50/60 mx-2 mb-2 rounded-md border border-rose-200/60">
                    <span>Couldn&apos;t load templates. Try again in a moment.</span>
                </div>
            ) : all.length === 0 ? (
                <TemplatePickerEmpty onClose={onClose} />
            ) : filtered.length === 0 ? (
                <div className="px-3 py-6 text-center">
                    <p className="text-[12px] text-slate-500">
                        No templates match &ldquo;{search}&rdquo;.
                    </p>
                    <button
                        type="button"
                        onClick={() => setSearch("")}
                        className="mt-2 text-[11.5px] text-sky-700 hover:text-sky-900 font-medium"
                    >
                        Clear search
                    </button>
                </div>
            ) : (
                <div className="max-h-[300px] overflow-y-auto px-1 pb-1 space-y-0.5">
                    {filtered.map((t) => (
                        <TemplateRow
                            key={t.id}
                            template={t}
                            onPick={() => {
                                onPick(t);
                                onClose();
                            }}
                        />
                    ))}
                </div>
            )}

            {all.length > 0 && (
                <div className="border-t border-slate-200/70 px-2 py-1.5 flex items-center justify-between">
                    <Link
                        to="/app/templates"
                        onClick={onClose}
                        className="inline-flex items-center gap-1.5 h-6 px-1.5 rounded text-[11px] text-slate-500 hover:text-slate-900 hover:bg-slate-100 transition-colors"
                    >
                        <SettingsIcon className="w-3 h-3" />
                        Manage templates
                    </Link>
                    <span className="font-mono text-[9.5px] uppercase tracking-[0.14em] text-slate-400">
                        Click to insert
                    </span>
                </div>
            )}
        </div>
    );
}

// TemplateRow : one template entry. Two compact lines, no icon, no
// left accent bar : just name on top, single-line body preview
// underneath. Hover and active states are conveyed by the row
// background alone.
function TemplateRow({
    template,
    onPick,
}: {
    template: Template;
    onPick: () => void;
}) {
    const bodyPreview = (template.body_plain ?? "")
        .replace(/\s+/g, " ")
        .trim()
        .slice(0, 120);

    return (
        <button
            type="button"
            onClick={onPick}
            className="w-full text-left rounded-md px-2.5 py-1.5 flex flex-col gap-0.5 hover:bg-slate-50 active:bg-slate-100 transition-colors focus:outline-none focus-visible:ring-2 focus-visible:ring-sky-200"
        >
            <span className="text-[12.5px] font-medium text-slate-900 truncate">
                {template.name}
            </span>
            {bodyPreview && (
                <span className="text-[11px] text-slate-400 truncate leading-snug">
                    {bodyPreview}
                </span>
            )}
        </button>
    );
}

// TemplatePickerEmpty : the zero-state surface. A blank list is a
// teaching moment, not a dead-end, so we link straight to the
// templates settings page where the user can create their first one.
function TemplatePickerEmpty({ onClose }: { onClose: () => void }) {
    return (
        <div className="px-4 pb-4 pt-2 text-center">
            <div className="size-9 rounded-lg bg-slate-100 text-slate-400 inline-flex items-center justify-center mb-2">
                <FileTextIcon className="w-4 h-4" />
            </div>
            <p className="text-[12.5px] font-medium text-slate-900">
                No templates yet
            </p>
            <p className="text-[11px] text-slate-500 mt-1 leading-relaxed max-w-[34ch] mx-auto">
                Save your most-used replies once and drop them into any
                conversation with two clicks.
            </p>
            <Link
                to="/app/templates"
                onClick={onClose}
                className="inline-flex items-center gap-1.5 h-7 px-2.5 mt-3 rounded-md bg-sky-600 hover:bg-sky-700 text-white text-[12px] font-medium transition-colors"
            >
                <SettingsIcon className="w-3 h-3" />
                Create a template
            </Link>
        </div>
    );
}
