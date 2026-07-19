// ContactRecipientField — the composer recipient editor shared by compose and
// reply: chip-based like a mail client, with a live contact autocomplete under
// the input. Typing searches the org's contacts (name, email, company); when
// the text matches a contact category, the category is offered as a filter so
// "sales" narrows the suggestions to that group. Enter/Tab/"," still commit
// raw addresses, so recipients outside the CRM work too.

import React from "react";
import { AnimatePresence, motion } from "framer-motion";
import { TagIcon, UserIcon, XIcon } from "lucide-react";
import useSearchContacts from "@/lib/api/hooks/app/contacts/useSearchContacts";
import { useUserProfile } from "@/hooks/context/user";
import type Contact from "@/lib/api/models/app/contacts/Contact";
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
        focused && (query.length > 0 || !!catFilter) && (suggestions.length > 0 || matchedCats.length > 0);

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
        <div className="relative flex-1 min-w-0 flex flex-wrap items-center gap-1.5">
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
        </div>
    );
}
