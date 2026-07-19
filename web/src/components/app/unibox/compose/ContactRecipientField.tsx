// ContactRecipientField — the composer recipient editor shared by compose and
// reply: chip-based like a mail client, with a live contact autocomplete under
// the input. Typing searches the org's contacts (name, email, company); when
// the text matches a contact category, the category is offered as a filter so
// "sales" narrows the suggestions to that group. Enter/Tab/"," still commit
// raw addresses, so recipients outside the CRM work too.

import React from "react";
import { createPortal } from "react-dom";
import { AnimatePresence, motion } from "framer-motion";
import {
    ArrowUpDownIcon,
    CheckIcon,
    SearchIcon,
    TagIcon,
    UserIcon,
    UserPlusIcon,
    XIcon,
} from "lucide-react";
import FilterMenu from "@/components/app/unibox/compose/FilterMenu";
import useSearchContacts from "@/lib/api/hooks/app/contacts/useSearchContacts";
import { useUserProfile } from "@/hooks/context/user";
import useClickOutside from "@/hooks/useClickOutside";
import type Contact from "@/lib/api/models/app/contacts/Contact";
import type { SearchContactsSortBy } from "@/lib/api/models/app/contacts/search-contacts.types";
import { cn } from "@/lib/utils";

interface CategoryRef {
    id: string;
    title: string;
    color: string;
}

function looksLikeEmail(s: string): boolean {
    return /^[^\s@]+@[^\s@]+\.[^\s@]+$/.test(s.trim());
}

function contactName(c: Contact): string {
    const name = `${c.first_name ?? ""} ${c.last_name ?? ""}`.trim();
    return name || c.email;
}

// Sort options for the browse panel, mapped onto search sort_by keys.
const BROWSE_SORTS: { key: SearchContactsSortBy; label: string }[] = [
    { key: "updated_at", label: "Recent" },
    { key: "first_name", label: "Name" },
    { key: "email", label: "Email" },
];

const BROWSE_PANEL_WIDTH = 360;
const BROWSE_PANEL_HEIGHT = 380;

interface ContactRecipientFieldProps {
    value: string[];
    onChange: (next: string[]) => void;
    placeholder: string;
    autoFocus?: boolean;
}

export default function ContactRecipientField({
    value,
    onChange,
    placeholder,
    autoFocus,
}: ContactRecipientFieldProps) {
    const [input, setInput] = React.useState("");
    const [focused, setFocused] = React.useState(false);
    const [highlight, setHighlight] = React.useState(0);
    const [catFilter, setCatFilter] = React.useState<CategoryRef | null>(null);
    const inputRef = React.useRef<HTMLInputElement>(null);
    const blurTimer = React.useRef<number | null>(null);

    // Browse panel — click-driven contact picker with its own search,
    // category filter, sort, and multi-select, separate from the
    // type-ahead suggestions.
    const [browseOpen, setBrowseOpen] = React.useState(false);
    const [browseQuery, setBrowseQuery] = React.useState("");
    const [browseCat, setBrowseCat] = React.useState<string | null>(null);
    const [browseSort, setBrowseSort] = React.useState<SearchContactsSortBy>("updated_at");
    const [browsePicked, setBrowsePicked] = React.useState<string[]>([]);
    // Viewport anchor for the portaled panel (the compose window clips
    // overflow, so the panel can't render inside it — same as MailboxPicker).
    const [browseAnchor, setBrowseAnchor] = React.useState<{
        top: number;
        left: number;
        up: boolean;
    } | null>(null);
    const rootRef = React.useRef<HTMLDivElement>(null);
    useClickOutside(rootRef, () => setBrowseOpen(false));

    const measureBrowse = React.useCallback(() => {
        const el = rootRef.current;
        if (!el) return;
        const r = el.getBoundingClientRect();
        // clientWidth excludes any scrollbar; innerWidth would let the
        // panel's right edge slide underneath it.
        const vw = document.documentElement.clientWidth;
        const vh = window.innerHeight;
        const left = Math.min(Math.max(r.left, 8), Math.max(8, vw - BROWSE_PANEL_WIDTH - 8));
        const up = vh - r.bottom < BROWSE_PANEL_HEIGHT + 12 && r.top > vh - r.bottom;
        setBrowseAnchor({ top: up ? r.top - 4 : r.bottom + 4, left, up });
    }, []);

    React.useEffect(() => {
        if (!browseOpen) return;
        measureBrowse();
        window.addEventListener("scroll", measureBrowse, true);
        window.addEventListener("resize", measureBrowse);
        return () => {
            window.removeEventListener("scroll", measureBrowse, true);
            window.removeEventListener("resize", measureBrowse);
        };
    }, [browseOpen, measureBrowse]);

    const { user } = useUserProfile();
    const allCategories = React.useMemo<CategoryRef[]>(
        () => user.categories ?? [],
        [user.categories],
    );

    const query = input.trim();
    const search = useSearchContacts({
        options: {
            query,
            filters: [],
            campaign_ids: [],
            category_ids: catFilter ? [catFilter.id] : undefined,
            sort_by: "updated_at",
            reverse: false,
        },
        limit: catFilter ? 8 : 6,
        enabled: focused && (query.length > 0 || !!catFilter),
    });

    const suggestions = React.useMemo(() => {
        const seen = new Set(value.map((v) => v.toLowerCase()));
        return (search.contacts ?? [])
            .filter((c) => c.email && !seen.has(c.email.toLowerCase()))
            .slice(0, catFilter ? 8 : 6);
    }, [catFilter, search.contacts, value]);

    // Categories the typed text matches, offered as filters above the
    // contact rows. Hidden once a filter is active.
    const matchedCats = React.useMemo(() => {
        if (catFilter || query.length === 0) return [];
        const q = query.toLowerCase();
        return allCategories.filter((c) => c.title.toLowerCase().includes(q)).slice(0, 3);
    }, [allCategories, catFilter, query]);

    const showMenu =
        focused &&
        !browseOpen &&
        (query.length > 0 || !!catFilter) &&
        (suggestions.length > 0 || matchedCats.length > 0);

    const browseSearch = useSearchContacts({
        options: {
            query: browseQuery.trim(),
            filters: [],
            campaign_ids: [],
            category_ids: browseCat ? [browseCat] : undefined,
            sort_by: browseSort,
            reverse: false,
        },
        limit: 25,
        enabled: browseOpen,
    });
    const browseRows = React.useMemo(
        () => (browseSearch.contacts ?? []).filter((c) => !!c.email).slice(0, 25),
        [browseSearch.contacts],
    );
    const existingEmails = React.useMemo(
        () => new Set(value.map((v) => v.toLowerCase())),
        [value],
    );

    const toggleBrowsePick = (email: string) => {
        setBrowsePicked((p) =>
            p.some((e) => e.toLowerCase() === email.toLowerCase())
                ? p.filter((e) => e.toLowerCase() !== email.toLowerCase())
                : [...p, email],
        );
    };

    const addBrowsePicked = () => {
        const seen = new Set(value.map((v) => v.toLowerCase()));
        const next = [...value];
        for (const e of browsePicked) {
            if (seen.has(e.toLowerCase())) continue;
            seen.add(e.toLowerCase());
            next.push(e);
        }
        onChange(next);
        setBrowseOpen(false);
        setBrowsePicked([]);
    };

    React.useEffect(() => {
        setHighlight(0);
    }, [query, catFilter, suggestions.length]);

    const commit = (raw: string) => {
        const trimmed = raw.trim().replace(/,$/, "").trim();
        if (!trimmed) return;
        if (value.some((v) => v.toLowerCase() === trimmed.toLowerCase())) {
            setInput("");
            return;
        }
        onChange([...value, trimmed]);
        setInput("");
    };

    const pick = (c: Contact) => {
        commit(c.email);
        inputRef.current?.focus();
    };

    const applyCatFilter = (c: CategoryRef) => {
        setCatFilter(c);
        setInput("");
        inputRef.current?.focus();
    };

    return (
        <div ref={rootRef} className="relative flex-1 min-w-0 flex flex-wrap items-center gap-1.5">
            {value.map((v) => (
                <span
                    key={v}
                    className={cn(
                        "inline-flex items-center gap-1 h-5 pl-1.5 pr-0.5 rounded-md text-[11px] font-medium max-w-full min-w-0 border",
                        looksLikeEmail(v)
                            ? "bg-sky-50 text-sky-800 border-sky-200"
                            : "bg-rose-50 text-rose-800 border-rose-200",
                    )}
                    title={v}
                >
                    <span className="font-mono truncate">{v}</span>
                    <button
                        type="button"
                        onClick={() => onChange(value.filter((x) => x !== v))}
                        aria-label={`Remove ${v}`}
                        className="size-4 shrink-0 inline-flex items-center justify-center rounded hover:bg-black/10"
                    >
                        <XIcon className="w-2.5 h-2.5" />
                    </button>
                </span>
            ))}
            {catFilter && (
                <span className="inline-flex items-center gap-1 h-5 pl-1.5 pr-0.5 rounded-md text-[11px] font-medium border border-slate-200 bg-slate-50 text-slate-700">
                    <span className="size-1.5 rounded-full" style={{ backgroundColor: catFilter.color }} />
                    {catFilter.title}
                    <button
                        type="button"
                        onClick={() => {
                            setCatFilter(null);
                            inputRef.current?.focus();
                        }}
                        aria-label={`Clear ${catFilter.title} filter`}
                        className="size-4 shrink-0 inline-flex items-center justify-center rounded hover:bg-black/10"
                    >
                        <XIcon className="w-2.5 h-2.5" />
                    </button>
                </span>
            )}
            <input
                ref={inputRef}
                type="email"
                value={input}
                autoFocus={autoFocus}
                onChange={(e) => setInput(e.target.value)}
                placeholder={value.length === 0 && !catFilter ? placeholder : ""}
                onFocus={() => {
                    if (blurTimer.current) window.clearTimeout(blurTimer.current);
                    setFocused(true);
                }}
                onBlur={() => {
                    // Delay so a mousedown on a suggestion row wins over blur.
                    blurTimer.current = window.setTimeout(() => {
                        setFocused(false);
                        commit(input);
                    }, 120);
                }}
                onKeyDown={(e) => {
                    const rows = matchedCats.length + suggestions.length;
                    if (showMenu && e.key === "ArrowDown") {
                        e.preventDefault();
                        setHighlight((h) => Math.min(h + 1, rows - 1));
                    } else if (showMenu && e.key === "ArrowUp") {
                        e.preventDefault();
                        setHighlight((h) => Math.max(h - 1, 0));
                    } else if (e.key === "Enter" || e.key === "," || e.key === "Tab") {
                        if (showMenu && e.key === "Enter") {
                            e.preventDefault();
                            if (highlight < matchedCats.length) {
                                applyCatFilter(matchedCats[highlight]);
                            } else {
                                pick(suggestions[highlight - matchedCats.length]);
                            }
                        } else if (input.trim()) {
                            e.preventDefault();
                            commit(input);
                        }
                    } else if (e.key === "Escape" && (showMenu || catFilter)) {
                        e.stopPropagation();
                        setCatFilter(null);
                        setFocused(false);
                    } else if (e.key === "Backspace" && !input) {
                        if (catFilter) {
                            e.preventDefault();
                            setCatFilter(null);
                        } else if (value.length > 0) {
                            e.preventDefault();
                            onChange(value.slice(0, -1));
                        }
                    }
                }}
                onPaste={(e) => {
                    const text = e.clipboardData.getData("text");
                    if (text.includes(",") || text.includes(" ")) {
                        e.preventDefault();
                        const parts = text
                            .split(/[,\s]+/)
                            .map((s) => s.trim())
                            .filter(Boolean);
                        const fresh = [...value];
                        for (const p of parts) {
                            if (!fresh.includes(p)) fresh.push(p);
                        }
                        onChange(fresh);
                    }
                }}
                className="flex-1 min-w-[14ch] h-5 bg-transparent text-[11.5px] text-slate-900 placeholder:text-slate-400 outline-none font-mono"
            />
            <button
                type="button"
                onClick={() => {
                    if (!browseOpen) {
                        setBrowseQuery("");
                        setBrowseCat(null);
                        setBrowsePicked([]);
                    }
                    setBrowseOpen((o) => !o);
                }}
                aria-label="Browse contacts"
                title="Browse contacts"
                className="shrink-0 size-5 inline-flex items-center justify-center rounded text-slate-400 hover:text-slate-600 hover:bg-slate-100 transition-colors"
            >
                <UserPlusIcon className="w-3.5 h-3.5" />
            </button>

            <AnimatePresence>
                {showMenu && (
                    <motion.div
                        initial={{ opacity: 0, y: -4 }}
                        animate={{ opacity: 1, y: 0 }}
                        exit={{ opacity: 0, y: -4 }}
                        transition={{ duration: 0.12, ease: [0.16, 1, 0.3, 1] }}
                        className="absolute left-0 right-0 top-full mt-1 z-30 rounded-md border border-slate-200 bg-white shadow-lg overflow-hidden"
                    >
                        {matchedCats.map((c, i) => (
                            <button
                                key={`cat-${c.id}`}
                                type="button"
                                onMouseDown={(e) => {
                                    e.preventDefault();
                                    applyCatFilter(c);
                                }}
                                onMouseEnter={() => setHighlight(i)}
                                className={cn(
                                    "w-full px-2.5 h-7 flex items-center gap-2 text-left transition-colors",
                                    i === highlight ? "bg-sky-50" : "bg-white",
                                )}
                            >
                                <TagIcon className="w-3 h-3 text-slate-400 shrink-0" />
                                <span className="size-1.5 rounded-full shrink-0" style={{ backgroundColor: c.color }} />
                                <span className="text-[11.5px] text-slate-800 truncate">{c.title}</span>
                                <span className="ml-auto text-[9.5px] text-slate-400 shrink-0">
                                    filter by category
                                </span>
                            </button>
                        ))}
                        {matchedCats.length > 0 && suggestions.length > 0 && (
                            <div className="h-px bg-slate-100" />
                        )}
                        {suggestions.map((c, i) => {
                            const idx = matchedCats.length + i;
                            return (
                                <button
                                    key={c.id}
                                    type="button"
                                    // onMouseDown so the row fires before the input's blur.
                                    onMouseDown={(e) => {
                                        e.preventDefault();
                                        pick(c);
                                    }}
                                    onMouseEnter={() => setHighlight(idx)}
                                    className={cn(
                                        "w-full px-2.5 h-9 flex items-center gap-2 text-left transition-colors",
                                        idx === highlight ? "bg-sky-50" : "bg-white",
                                    )}
                                >
                                    <span className="size-5 rounded-full bg-slate-100 text-slate-500 inline-flex items-center justify-center shrink-0">
                                        <UserIcon className="w-3 h-3" />
                                    </span>
                                    <span className="min-w-0 flex-1">
                                        <span className="flex items-center gap-1.5 min-w-0">
                                            <span className="text-[12px] font-medium text-slate-900 truncate leading-tight">
                                                {contactName(c)}
                                                {c.company && (
                                                    <span className="font-normal text-slate-400"> · {c.company}</span>
                                                )}
                                            </span>
                                            {(c.categories ?? []).slice(0, 2).map((cat) => (
                                                <span
                                                    key={cat.id}
                                                    className="inline-flex items-center gap-0.5 text-[9px] text-slate-400 shrink-0"
                                                    title={cat.title}
                                                >
                                                    <span
                                                        className="size-1.5 rounded-full"
                                                        style={{ backgroundColor: cat.color }}
                                                    />
                                                    {cat.title}
                                                </span>
                                            ))}
                                        </span>
                                        <span className="block font-mono text-[10.5px] text-slate-500 truncate leading-tight">
                                            {c.email}
                                        </span>
                                    </span>
                                </button>
                            );
                        })}
                    </motion.div>
                )}
            </AnimatePresence>

            {createPortal(
                <AnimatePresence>
                    {browseOpen && browseAnchor && (
                        <motion.div
                            data-floating=""
                            initial={{ opacity: 0, y: browseAnchor.up ? 4 : -4, scale: 0.98 }}
                            animate={{ opacity: 1, y: 0, scale: 1 }}
                            exit={{ opacity: 0, y: browseAnchor.up ? 4 : -4, scale: 0.98 }}
                            transition={{ duration: 0.14, ease: [0.16, 1, 0.3, 1] }}
                            style={{
                                position: "fixed",
                                left: browseAnchor.left,
                                width: BROWSE_PANEL_WIDTH,
                                zIndex: 120,
                                ...(browseAnchor.up
                                    ? { bottom: window.innerHeight - browseAnchor.top }
                                    : { top: browseAnchor.top }),
                            }}
                            className="max-w-[calc(100vw-16px)] max-h-[min(420px,70vh)] flex flex-col rounded-lg border border-slate-200 bg-white shadow-xl overflow-hidden"
                        >
                        {/* One compact header row: search + category + sort. */}
                        <div className="shrink-0 px-1.5 pt-1.5 pb-1 border-b border-slate-100 flex items-center gap-1">
                            <div className="flex-1 min-w-0 flex items-center gap-1.5 px-1.5 h-6 rounded-md border border-slate-200 bg-white focus-within:border-sky-300 focus-within:ring-1 focus-within:ring-sky-100 transition-colors">
                                <SearchIcon className="w-3 h-3 text-slate-400 shrink-0" />
                                <input
                                    value={browseQuery}
                                    onChange={(e) => setBrowseQuery(e.target.value)}
                                    onKeyDown={(e) => {
                                        if (e.key === "Escape") {
                                            e.stopPropagation();
                                            setBrowseOpen(false);
                                        }
                                    }}
                                    placeholder="Search contacts…"
                                    autoFocus
                                    className="flex-1 min-w-0 bg-transparent text-[11.5px] text-slate-900 placeholder:text-slate-400 outline-none"
                                />
                            </div>
                            {allCategories.length > 0 && (
                                <FilterMenu
                                    icon={TagIcon}
                                    allLabel="All categories"
                                    options={allCategories.map((c) => ({
                                        id: c.id,
                                        label: c.title,
                                        color: c.color,
                                    }))}
                                    value={browseCat}
                                    onChange={setBrowseCat}
                                />
                            )}
                            <FilterMenu
                                icon={ArrowUpDownIcon}
                                allLabel="Sort"
                                allowAll={false}
                                options={BROWSE_SORTS.map((s) => ({ id: s.key, label: s.label }))}
                                value={browseSort}
                                onChange={(id) => {
                                    if (id) setBrowseSort(id as SearchContactsSortBy);
                                }}
                            />
                        </div>
                        <div className="min-h-0 overflow-y-auto py-1">
                            {browseSearch.isLoading ? (
                                <div className="px-3 py-4 text-[11.5px] text-slate-400 text-center">Loading…</div>
                            ) : browseRows.length === 0 ? (
                                <div className="px-3 py-4 text-[11.5px] text-slate-400 text-center">No contacts.</div>
                            ) : (
                                browseRows.map((c) => {
                                    const added = existingEmails.has(c.email.toLowerCase());
                                    const checked = browsePicked.some(
                                        (e) => e.toLowerCase() === c.email.toLowerCase(),
                                    );
                                    return (
                                        <button
                                            key={c.id}
                                            type="button"
                                            disabled={added}
                                            onClick={() => toggleBrowsePick(c.email)}
                                            className={cn(
                                                "w-full px-2.5 h-9 flex items-center gap-2 text-left transition-colors",
                                                added ? "opacity-50" : "hover:bg-slate-50",
                                            )}
                                        >
                                            <span
                                                className={cn(
                                                    "size-3.5 rounded border flex items-center justify-center transition-colors shrink-0",
                                                    checked || added
                                                        ? "border-slate-900 bg-slate-900"
                                                        : "border-slate-300 bg-white",
                                                )}
                                            >
                                                {(checked || added) && <CheckIcon className="w-2 h-2 text-white" />}
                                            </span>
                                            <span className="min-w-0 flex-1">
                                                <span className="flex items-center gap-1.5 min-w-0">
                                                    <span className="text-[12px] font-medium text-slate-900 truncate leading-tight">
                                                        {contactName(c)}
                                                    </span>
                                                    {(c.categories ?? []).slice(0, 1).map((cat) => (
                                                        <span
                                                            key={cat.id}
                                                            className="inline-flex items-center gap-0.5 text-[9px] text-slate-400 shrink-0"
                                                            title={cat.title}
                                                        >
                                                            <span
                                                                className="size-1.5 rounded-full"
                                                                style={{ backgroundColor: cat.color }}
                                                            />
                                                            {cat.title}
                                                        </span>
                                                    ))}
                                                </span>
                                                <span className="block font-mono text-[10.5px] text-slate-500 truncate leading-tight">
                                                    {c.email}
                                                </span>
                                            </span>
                                            {added && (
                                                <span className="shrink-0 text-[9.5px] text-slate-400">Added</span>
                                            )}
                                        </button>
                                    );
                                })
                            )}
                        </div>
                        {browsePicked.length > 0 && (
                            <div className="shrink-0 p-1.5 border-t border-slate-200 flex items-center gap-2">
                                <span className="pl-1 text-[10.5px] text-slate-400">
                                    {browsePicked.length} selected
                                </span>
                                <button
                                    type="button"
                                    onClick={addBrowsePicked}
                                    className="ml-auto h-6 px-2.5 rounded-md bg-sky-600 hover:bg-sky-700 text-white text-[11.5px] font-medium transition-colors"
                                >
                                    Add {browsePicked.length}
                                </button>
                            </div>
                        )}
                        </motion.div>
                    )}
                </AnimatePresence>,
                document.body,
            )}
        </div>
    );
}
