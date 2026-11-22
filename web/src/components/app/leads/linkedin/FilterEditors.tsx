// The filter editors the grid opens in a popover: LinkedIn lookups with Include or Exclude, option lists, text and ranges.
import { BanIcon, CheckIcon, Loader2Icon, SearchIcon, XIcon } from "lucide-react";
import { useEffect, useId, useMemo, useRef, useState, type Dispatch, type KeyboardEvent } from "react";
import { NumberInput } from "@/components/ui/field";
import { useFinderTypeahead } from "@/lib/api/hooks/app/social/useSocial";
import type { FinderFilter, FinderSource, FinderValue } from "@/lib/api/models/app/social/Social";
import { socialErrorMessage } from "@/lib/social/errors";
import { cn } from "@/lib/utils";
import { apiAccount, valueKey, type FinderAction } from "./state";

/** One selected value: the mark switches include and exclude, the cross removes it. */
export function ValueChip({ filter, value: v, dispatch, size = "md", label: shown }: { filter: FinderFilter; value: FinderValue; dispatch: Dispatch<FinderAction>; size?: "sm" | "md"; label?: string }) {
    const label = shown ?? (v.text || v.id || "");
    const sm = size === "sm";
    return (
        <li
            data-chip={v.exclude ? "exclude" : "include"}
            className={cn(
                "inline-flex max-w-full min-w-0 items-center gap-0.5 rounded-full px-0.5 font-medium ring-1 ring-inset",
                sm ? "h-5 text-[11px]" : "h-6 text-[11.5px]",
                v.exclude ? "bg-red-50 text-red-700 ring-red-200" : "bg-white text-slate-800 ring-slate-200",
            )}
        >
            <button
                type="button"
                disabled={!filter.exclude}
                onClick={(e) => {
                    e.stopPropagation();
                    dispatch({ type: "toggleExclude", filter, value: v });
                }}
                aria-label={v.exclude ? `Excluded: ${label}. Include instead` : `Included: ${label}${filter.exclude ? ". Exclude instead" : ""}`}
                className={cn("grid size-4 shrink-0 place-items-center rounded-full", filter.exclude && "hover:bg-slate-900/10", v.exclude ? "text-red-600" : "text-emerald-600")}
            >
                {v.exclude ? <BanIcon className="size-2.5" /> : <CheckIcon className="size-2.5" strokeWidth={3} />}
            </button>
            <span className="truncate">{label}</span>
            <button
                type="button"
                onClick={(e) => {
                    e.stopPropagation();
                    dispatch({ type: "remove", key: filter.key, value: v });
                }}
                aria-label={`Remove ${label}`}
                className="grid size-4 shrink-0 place-items-center rounded-full text-slate-400 hover:bg-slate-900/10 hover:text-slate-700"
            >
                <XIcon className="size-2.5" />
            </button>
        </li>
    );
}

export function Chips({ filter, values, dispatch }: { filter: FinderFilter; values: FinderValue[]; dispatch: Dispatch<FinderAction> }) {
    return (
        <ul className="flex flex-wrap gap-1" aria-label={`${filter.label} values`}>
            {values.map((v) => (
                <ValueChip key={valueKey(v)} filter={filter} value={v} dispatch={dispatch} />
            ))}
        </ul>
    );
}

function useDebounced<T>(value: T, ms: number): T {
    const [v, setV] = useState(value);
    useEffect(() => {
        const t = window.setTimeout(() => setV(value), ms);
        return () => window.clearTimeout(t);
    }, [value, ms]);
    return v;
}

const INPUT = "flex h-7 items-center gap-2 rounded-md border border-slate-200 bg-white px-2 focus-within:border-sky-400 focus-within:ring-2 focus-within:ring-sky-100";

/** A LinkedIn lookup: type, then Include or Exclude a suggestion (Enter includes, Alt+Enter excludes). */
export function Typeahead({ filter: f, source, values, dispatch, accountId }: { filter: FinderFilter; source: FinderSource; values: FinderValue[]; dispatch: Dispatch<FinderAction>; accountId: string }) {
    const [q, setQ] = useState("");
    const [active, setActive] = useState(0);
    const debounced = useDebounced(q.trim(), 220);
    const listId = useId();
    const inputRef = useRef<HTMLInputElement>(null);
    const res = useFinderTypeahead({ source, filter: f.key, q: debounced, account_id: apiAccount(accountId) }, !!accountId && (debounced.length >= 1 || !!f.suggest));
    const chosen = useMemo(() => new Set(values.map(valueKey)), [values]);
    type Item = { id?: string; text: string; free?: boolean };
    const typed = q.trim();
    const items: Item[] = useMemo(() => {
        if (!typed && !f.suggest) return [];
        const list: Item[] = (typed === debounced || !typed ? (res.data ?? []) : []).map((o) => ({ id: o.id, text: o.label }));
        if (f.free_text && typed && !list.some((i) => i.text.toLowerCase() === typed.toLowerCase())) list.unshift({ text: typed, free: true });
        return list;
    }, [res.data, typed, debounced, f.free_text, f.suggest]);
    useEffect(() => setActive(0), [items.length, debounced]);

    const pick = (item: Item | undefined, exclude: boolean) => {
        if (!item) return;
        dispatch({ type: "add", filter: f, value: { id: item.id, text: item.text, ...(exclude && f.exclude ? { exclude: true } : {}) } });
        setQ("");
        inputRef.current?.focus();
    };
    const onKey = (e: KeyboardEvent<HTMLInputElement>) => {
        if (e.key === "ArrowDown") {
            e.preventDefault();
            setActive((a) => Math.min(a + 1, items.length - 1));
        } else if (e.key === "ArrowUp") {
            e.preventDefault();
            setActive((a) => Math.max(a - 1, 0));
        } else if (e.key === "Enter") {
            e.preventDefault();
            pick(items[active], e.altKey);
        }
    };
    const showList = items.length > 0 || res.isFetching || res.isError || debounced.length > 0;
    return (
        <div className="grid gap-1.5">
            <div className={INPUT}>
                <SearchIcon aria-hidden className="size-3.5 text-slate-400" />
                <input
                    ref={inputRef}
                    autoFocus
                    role="combobox"
                    aria-expanded={showList}
                    aria-controls={listId}
                    aria-autocomplete="list"
                    aria-label={`Add ${f.label.toLowerCase()}`}
                    aria-activedescendant={items[active] ? `${listId}-${active}` : undefined}
                    value={q}
                    disabled={!accountId}
                    placeholder={accountId ? (f.placeholder ?? `Add ${f.label.toLowerCase()}`) : "Choose an account first"}
                    onChange={(e) => setQ(e.target.value)}
                    onKeyDown={onKey}
                    className="h-full min-w-0 flex-1 bg-transparent text-[12.5px] outline-none placeholder:text-slate-400"
                />
                {res.isFetching && <Loader2Icon className="size-3 animate-spin text-slate-400" />}
            </div>
            {showList && (
                <ul id={listId} role="listbox" aria-label={`${f.label} suggestions`} className="max-h-56 overflow-y-auto rounded-md border border-slate-200 p-0.5">
                    {items.map((it, i) => {
                        const isChosen = chosen.has((it.id || it.text).toLowerCase());
                        return (
                            <li
                                key={`${it.id ?? ""}|${it.text}`}
                                id={`${listId}-${i}`}
                                role="option"
                                aria-selected={i === active}
                                onMouseEnter={() => setActive(i)}
                                className={cn("group flex items-center gap-2 rounded px-2 py-1 text-[12.5px]", i === active && "bg-slate-50")}
                            >
                                <button type="button" onMouseDown={(e) => e.preventDefault()} onClick={() => pick(it, false)} className="min-w-0 flex-1 truncate text-left text-slate-800">
                                    {it.free ? (
                                        <>
                                            Use "<span className="font-medium">{it.text}</span>"
                                        </>
                                    ) : (
                                        it.text
                                    )}
                                </button>
                                {isChosen && <CheckIcon aria-label="Selected" className="size-3.5 text-emerald-600" />}
                                <span className={cn("flex items-center gap-0.5", i === active ? "opacity-100" : "opacity-100 md:opacity-0 md:group-hover:opacity-100")}>
                                    <button
                                        type="button"
                                        onMouseDown={(e) => e.preventDefault()}
                                        onClick={() => pick(it, false)}
                                        className="inline-flex items-center gap-1 rounded px-1.5 py-0.5 text-[11px] font-medium text-emerald-700 hover:bg-emerald-50"
                                    >
                                        <CheckIcon className="size-3" strokeWidth={2.5} /> Include
                                    </button>
                                    {f.exclude && (
                                        <button
                                            type="button"
                                            onMouseDown={(e) => e.preventDefault()}
                                            onClick={() => pick(it, true)}
                                            className="inline-flex items-center gap-1 rounded px-1.5 py-0.5 text-[11px] font-medium text-red-700 hover:bg-red-50"
                                        >
                                            <BanIcon className="size-3" /> Exclude
                                        </button>
                                    )}
                                </span>
                            </li>
                        );
                    })}
                    {res.isFetching && items.length === 0 && <li className="px-2 py-1.5 text-[12px] text-slate-500">Searching LinkedIn…</li>}
                    {res.isError && <li className="px-2 py-1.5 text-[12px] text-red-600">{socialErrorMessage(res.error)}</li>}
                    {!res.isFetching && !res.isError && items.length === 0 && debounced.length > 0 && <li className="px-2 py-1.5 text-[12px] text-slate-500">No matches on LinkedIn.</li>}
                </ul>
            )}
        </div>
    );
}

/** A fixed list (seniority, headcount): each option can be included or, where LinkedIn allows, excluded. */
export function Options({ filter: f, values, dispatch }: { filter: FinderFilter; values: FinderValue[]; dispatch: Dispatch<FinderAction> }) {
    const state = (id: string) => {
        const v = values.find((x) => x.id === id);
        return v ? (v.exclude ? "exclude" : "include") : "off";
    };
    return (
        <ul className="grid gap-px" aria-label={f.label}>
            {(f.options ?? []).map((o) => {
                const st = state(o.id);
                return (
                    <li key={o.id} className="group flex items-center gap-2 rounded px-1.5 py-1 text-[12.5px] hover:bg-slate-50">
                        <button
                            type="button"
                            role="checkbox"
                            aria-checked={st !== "off"}
                            onClick={() => dispatch({ type: "option", filter: f, id: o.id, label: o.label, mode: st === "off" ? "include" : "off" })}
                            className="flex min-w-0 flex-1 items-center gap-2 text-left"
                        >
                            <span
                                className={cn(
                                    "size-3.5 shrink-0 rounded border flex items-center justify-center transition-colors",
                                    st === "include" && "border-slate-900 bg-slate-900 text-white",
                                    st === "exclude" && "border-red-600 bg-red-600 text-white",
                                    st === "off" && "border-slate-300 bg-white",
                                )}
                            >
                                {st === "include" && <CheckIcon className="size-2" strokeWidth={3.5} />}
                                {st === "exclude" && <BanIcon className="size-2" strokeWidth={3} />}
                            </span>
                            <span className={cn("truncate text-slate-800", st === "exclude" && "text-red-700 line-through decoration-red-300")}>{o.label}</span>
                        </button>
                        {f.exclude && (
                            <button
                                type="button"
                                title={st === "exclude" ? "Include instead" : "Exclude"}
                                aria-label={`${st === "exclude" ? "Include" : "Exclude"} ${o.label}`}
                                onClick={() => dispatch({ type: "option", filter: f, id: o.id, label: o.label, mode: st === "exclude" ? "include" : "exclude" })}
                                className={cn(
                                    "grid size-6 place-items-center rounded text-slate-400 hover:bg-red-50 hover:text-red-600",
                                    st === "exclude" ? "opacity-100" : "opacity-100 md:opacity-0 md:group-hover:opacity-100 focus-visible:opacity-100",
                                )}
                            >
                                <BanIcon className="size-3.5" />
                            </button>
                        )}
                    </li>
                );
            })}
        </ul>
    );
}

export function TextValue({ filter: f, dispatch }: { filter: FinderFilter; dispatch: Dispatch<FinderAction> }) {
    const [v, setV] = useState("");
    const add = () => {
        if (!v.trim()) return;
        dispatch({ type: "add", filter: f, value: { text: v } });
        setV("");
    };
    return (
        <div className="flex gap-1.5">
            <div className={cn(INPUT, "flex-1 min-w-0")}>
                <input
                    autoFocus
                    value={v}
                    aria-label={f.label}
                    placeholder={f.placeholder ?? `Type a ${f.label.toLowerCase()}`}
                    onChange={(e) => setV(e.target.value)}
                    onKeyDown={(e) => {
                        if (e.key === "Enter") {
                            e.preventDefault();
                            add();
                        }
                    }}
                    className="h-full min-w-0 flex-1 bg-transparent text-[12.5px] outline-none placeholder:text-slate-400"
                />
            </div>
            <button type="button" onClick={add} disabled={!v.trim()} className="h-7 px-2.5 rounded-md border border-slate-200 bg-white text-[12px] font-medium text-slate-700 hover:bg-slate-50 disabled:opacity-50">
                Add
            </button>
        </div>
    );
}

/** A min to max range (Recruiter years, headcount growth). Signed ranges are written "lo..hi"; NaN is an open end. */
export function RangeValue({ filter: f, value, dispatch }: { filter: FinderFilter; value: string; dispatch: Dispatch<FinderAction> }) {
    const signed = (f.min ?? 0) < 0;
    const sep = signed ? ".." : "-";
    const [lo, setLo] = useState(NaN);
    const [hi, setHi] = useState(NaN);
    useEffect(() => {
        const [a, b] = value ? value.split(sep) : [];
        setLo(a ? Number(a) : NaN);
        setHi(b ? Number(b) : NaN);
    }, [value, sep]);
    const min = f.min ?? 0;
    const max = f.max ?? 30;
    const has = (n: number) => Number.isFinite(n);
    const valid = (has(lo) || has(hi)) && (!has(lo) || !has(hi) || lo <= hi) && (signed || (has(lo) && has(hi)));
    const why = !valid && (has(lo) || has(hi)) ? (has(lo) && has(hi) ? "The first number has to be the smaller one." : "Enter both numbers.") : null;
    const part = (n: number) => (has(n) ? String(n) : "");
    return (
        <div className="grid gap-2">
            <div className="flex items-center gap-2 text-[12.5px] text-slate-500">
                <NumberInput value={lo} onChange={setLo} min={min} max={max} placeholder={signed ? "Min %" : "From"} className="w-20" />
                <span>to</span>
                <NumberInput value={hi} onChange={setHi} min={min} max={max} placeholder={signed ? "Max %" : "To"} className="w-20" />
                <span>{signed ? "%" : "years"}</span>
            </div>
            {why && <p className="text-[11.5px] text-amber-700">{why}</p>}
            <div className="flex gap-1.5">
                <button
                    type="button"
                    disabled={!valid}
                    onClick={() => dispatch({ type: "range", filter: f, value: `${part(lo)}${sep}${part(hi)}` })}
                    className="h-7 px-2.5 rounded-md bg-slate-900 text-white text-[12px] font-medium disabled:opacity-50"
                >
                    Apply
                </button>
                {value && (
                    <button type="button" onClick={() => dispatch({ type: "range", filter: f, value: null })} className="h-7 px-2.5 rounded-md text-[12px] text-slate-600 hover:text-slate-900">
                        Clear
                    </button>
                )}
            </div>
        </div>
    );
}
