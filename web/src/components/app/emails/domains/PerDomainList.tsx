// The per-domain half of a choice made for many domains at once: a dropdown
// listing each domain with its own tracking host and redirect website, filled
// from the shared value until someone types over it.
import React from "react";
import { AnimatePresence, motion } from "framer-motion";
import { ChevronDownIcon, RotateCcwIcon } from "lucide-react";
import { SearchInput, TextInput } from "@/components/ui/field";
import { CheckSquare } from "@/components/ui/check-square";
import { cn } from "@/lib/utils";
import { Chip } from "./parts";

export interface DomainOverride {
    track?: boolean;
    host?: string;
    redirect?: boolean;
    url?: string;
}

export interface PerDomainRow {
    domain: string;
    /** False when tracking cannot be offered, e.g. the instance has no tracking host. */
    canTrack: boolean;
    track: boolean;
    host: string;
    redirect: boolean;
    url: string;
    /** The shared website, shown while this domain has none of its own. */
    urlPlaceholder: string;
    custom: boolean;
    trackChip?: React.ReactNode;
    redirectChip?: React.ReactNode;
    hostProblem?: string | null;
    urlProblem?: string | null;
    loading?: boolean;
}

function Tick({ checked, onToggle, label, disabled }: { checked: boolean; onToggle: () => void; label: string; disabled?: boolean }) {
    return (
        <button
            type="button"
            role="checkbox"
            aria-checked={checked}
            disabled={disabled}
            onClick={onToggle}
            className="inline-flex items-center gap-2 w-[76px] shrink-0 text-[11.5px] text-slate-700 hover:text-slate-900 rounded outline-none focus-visible:ring-2 focus-visible:ring-sky-100 disabled:opacity-40"
        >
            <CheckSquare checked={checked} />
            {label}
        </button>
    );
}

export function PerDomainList({
    rows,
    onChange,
    onReset,
    defaultOpen = false,
}: {
    rows: PerDomainRow[];
    onChange: (domain: string, patch: DomainOverride) => void;
    onReset: (domain: string) => void;
    defaultOpen?: boolean;
}) {
    const [open, setOpen] = React.useState(defaultOpen);
    const [query, setQuery] = React.useState("");
    const [limit, setLimit] = React.useState(20);
    const changed = rows.filter((r) => r.custom).length;
    const problems = rows.filter((r) => (r.track && r.hostProblem) || (r.redirect && r.urlProblem)).length;
    const q = query.trim().toLowerCase();
    const matched = q ? rows.filter((r) => r.domain.includes(q)) : rows;
    const shown = matched.slice(0, limit);
    if (rows.length === 0) return null;
    return (
        <div className="rounded-md border border-slate-200 bg-white">
            <button
                type="button"
                onClick={() => setOpen((v) => !v)}
                aria-expanded={open}
                className="w-full h-9 px-3 flex items-center gap-2 text-left text-[12px] text-slate-700 hover:bg-slate-50 rounded-md transition-colors"
            >
                <ChevronDownIcon className={cn("w-3.5 h-3.5 text-slate-400 transition-transform", !open && "-rotate-90")} />
                <span className="font-medium">Change per domain</span>
                <span className="text-slate-400">{rows.length.toLocaleString()}</span>
                <span className="ml-auto flex items-center gap-1">
                    {problems > 0 && <Chip tone="amber">{problems.toLocaleString()} to fix</Chip>}
                    {changed > 0 && <Chip tone="sky">{changed.toLocaleString()} changed</Chip>}
                </span>
            </button>
            <AnimatePresence initial={false}>
                {open && (
                    <motion.div
                        initial={{ height: 0, opacity: 0 }}
                        animate={{ height: "auto", opacity: 1 }}
                        exit={{ height: 0, opacity: 0 }}
                        transition={{ duration: 0.18, ease: [0.22, 1, 0.36, 1] }}
                        className="overflow-hidden border-t border-slate-100"
                    >
                        {rows.length > 8 && (
                            <div className="px-3 pt-2">
                                <SearchInput value={query} onChange={setQuery} placeholder="Find a domain…" className="w-full" />
                            </div>
                        )}
                        <div className="divide-y divide-slate-100">
                            {shown.map((r) => (
                                <div key={r.domain} className={cn("px-3 py-2 space-y-1", r.custom && "bg-sky-50/30")}>
                                    <div className="flex items-center gap-2 min-w-0 h-5">
                                        <span className="text-[12px] font-medium text-slate-900 truncate">{r.domain}</span>
                                        {r.custom && (
                                            <button
                                                type="button"
                                                onClick={() => onReset(r.domain)}
                                                title="Use the same values as every other domain"
                                                className="ml-auto h-5 px-1.5 rounded text-[10.5px] text-slate-500 hover:text-slate-900 hover:bg-slate-100 inline-flex items-center gap-1 transition-colors"
                                            >
                                                <RotateCcwIcon className="w-2.5 h-2.5" />
                                                Reset
                                            </button>
                                        )}
                                    </div>
                                    {r.canTrack && (
                                        <div className="flex items-center gap-2 min-w-0">
                                            <Tick checked={r.track} onToggle={() => onChange(r.domain, { track: !r.track })} label="Tracking" />
                                            <TextInput
                                                value={r.host}
                                                onChange={(v) => onChange(r.domain, { host: v, track: true })}
                                                disabled={!r.track || r.loading}
                                                invalid={r.track && !!r.hostProblem}
                                                title={r.track ? (r.hostProblem ?? undefined) : undefined}
                                                placeholder={r.loading ? "Checking DNS…" : `link.${r.domain}`}
                                                className="flex-1 min-w-0 font-mono"
                                            />
                                            {r.track && r.trackChip}
                                        </div>
                                    )}
                                    {r.track && r.hostProblem && <p className="pl-[84px] text-[11px] text-rose-700">{r.hostProblem}</p>}
                                    <div className="flex items-center gap-2 min-w-0">
                                        <Tick checked={r.redirect} onToggle={() => onChange(r.domain, { redirect: !r.redirect })} label="Redirect" />
                                        <TextInput
                                            value={r.url}
                                            onChange={(v) => onChange(r.domain, { url: v, redirect: true })}
                                            disabled={!r.redirect}
                                            invalid={r.redirect && !!r.urlProblem && r.url.trim() !== ""}
                                            title={r.redirect ? (r.urlProblem ?? undefined) : undefined}
                                            placeholder={r.urlPlaceholder || "https://yourcompany.com"}
                                            className="flex-1 min-w-0"
                                        />
                                        {r.redirect && r.redirectChip}
                                    </div>
                                    {r.redirect && r.urlProblem && <p className="pl-[84px] text-[11px] text-rose-700">{r.urlProblem}</p>}
                                </div>
                            ))}
                            {matched.length === 0 && <p className="px-3 py-3 text-[11.5px] text-slate-500">No domain matches.</p>}
                        </div>
                        {matched.length > shown.length && (
                            <button
                                type="button"
                                onClick={() => setLimit((l) => l + 50)}
                                className="w-full h-8 border-t border-slate-100 text-[11.5px] text-slate-600 hover:text-slate-900 hover:bg-slate-50 transition-colors"
                            >
                                Show more ({(matched.length - shown.length).toLocaleString()} left)
                            </button>
                        )}
                    </motion.div>
                )}
            </AnimatePresence>
        </div>
    );
}
