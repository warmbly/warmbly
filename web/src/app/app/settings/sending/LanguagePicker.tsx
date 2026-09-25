// The languages inbox tagging reads a workspace's mail in: the same chip box
// and searchable dropdown as the category picker.

import React from "react";
import { AnimatePresence, motion } from "framer-motion";
import { PlusIcon, XIcon } from "lucide-react";
import { CheckSquare } from "@/components/ui/check-square";
import { SearchInput } from "@/components/ui/field";
import useClickOutside from "@/hooks/useClickOutside";
import useFlipPlacement from "@/hooks/useFlipPlacement";
import { MAIL_LANGUAGES, OFFLINE_RULE_LANGUAGES } from "@/lib/api/models/app/outreach/OutreachSettings";

const NAMES = new Map(MAIL_LANGUAGES.map((l) => [l.code, l.name]));

export default function LanguagePicker({ value, onChange }: { value: string[]; onChange: (next: string[]) => void }) {
    const [open, setOpen] = React.useState(false);
    const [query, setQuery] = React.useState("");
    const ref = React.useRef<HTMLDivElement>(null);
    const triggerRef = React.useRef<HTMLDivElement>(null);
    const close = React.useCallback(() => {
        setOpen(false);
        setQuery("");
    }, []);
    useClickOutside(ref, close);
    const placement = useFlipPlacement(triggerRef, open, 270);

    const filtered = React.useMemo(() => {
        const q = query.trim().toLowerCase();
        return q ? MAIL_LANGUAGES.filter((l) => l.name.toLowerCase().includes(q) || l.code === q) : MAIL_LANGUAGES;
    }, [query]);

    const toggle = (code: string) =>
        onChange(value.includes(code) ? value.filter((c) => c !== code) : [...value, code]);

    return (
        <div
            ref={ref}
            className="relative w-full sm:w-[360px]"
            onKeyDown={(e) => {
                if (e.key === "Escape" && open) {
                    e.stopPropagation();
                    close();
                }
            }}
        >
            <div ref={triggerRef} className="rounded-md border border-slate-200 bg-white min-h-[34px]">
                {value.length === 0 ? (
                    <button
                        type="button"
                        onClick={() => setOpen((o) => !o)}
                        className="w-full text-left px-3 py-2 text-[11.5px] text-slate-400 hover:text-slate-600"
                    >
                        Only the languages read by default
                    </button>
                ) : (
                    <div className="px-2 py-2 flex flex-wrap gap-1">
                        {value.map((code) => (
                            <span
                                key={code}
                                className="inline-flex items-center gap-1 h-5 pl-1.5 pr-0.5 rounded bg-sky-50 text-sky-700 text-[11px]"
                            >
                                {NAMES.get(code) ?? code}
                                <button
                                    type="button"
                                    aria-label={`Remove ${NAMES.get(code) ?? code}`}
                                    onClick={() => toggle(code)}
                                    className="size-4 rounded inline-flex items-center justify-center hover:bg-sky-100"
                                >
                                    <XIcon className="w-2.5 h-2.5" />
                                </button>
                            </span>
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
                        data-floating
                        initial={{ opacity: 0, y: placement === "top" ? 4 : -4 }}
                        animate={{ opacity: 1, y: 0 }}
                        exit={{ opacity: 0, y: placement === "top" ? 4 : -4 }}
                        transition={{ duration: 0.12 }}
                        className={`absolute left-0 right-0 z-30 rounded-md border border-slate-200 bg-white shadow-[0_12px_32px_-8px_rgba(15,23,42,0.18)] overflow-hidden ${
                            placement === "top" ? "bottom-full mb-1" : "top-full mt-1"
                        }`}
                    >
                        <div className="p-1.5 border-b border-slate-200">
                            <SearchInput
                                value={query}
                                onChange={setQuery}
                                placeholder="Search languages…"
                                autoFocus
                                className="w-full"
                            />
                        </div>
                        <div className="max-h-56 overflow-y-auto py-1">
                            {filtered.length === 0 && (
                                <div className="px-3 py-3 text-[11.5px] text-slate-400 text-center">No language matches.</div>
                            )}
                            {filtered.map((l) => (
                                <button
                                    key={l.code}
                                    type="button"
                                    onClick={() => toggle(l.code)}
                                    className="w-full px-2.5 h-7 flex items-center gap-2 text-[12px] text-slate-700 hover:bg-slate-100 transition-colors"
                                >
                                    <CheckSquare checked={value.includes(l.code)} />
                                    <span className="truncate">{l.name}</span>
                                    {!OFFLINE_RULE_LANGUAGES.has(l.code) && (
                                        <span className="ml-auto text-[10.5px] text-slate-400">classifier hint only</span>
                                    )}
                                </button>
                            ))}
                        </div>
                    </motion.div>
                )}
            </AnimatePresence>
        </div>
    );
}
