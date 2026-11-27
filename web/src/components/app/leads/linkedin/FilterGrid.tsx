// The filters without the long scroll: active chips on top, group tabs, a "Find a filter" box and a two-column grid of cells.
import { InfoIcon, LockIcon, PlusIcon, XIcon } from "lucide-react";
import { useMemo, useState, type Dispatch, type ReactNode } from "react";
import { Toggle } from "@/components/app/campaigns/preferences/components/CampaignPreferenceBoolBox";
import { NumberInput, SearchInput } from "@/components/ui/field";
import { PopoverMenu, PopoverMenuContent, PopoverMenuTrigger } from "@/components/ui/popover-menu";
import ScrollStrip from "@/components/ui/scroll-strip";
import type { FinderCatalog, FinderFilter, FinderSource, FinderValue } from "@/lib/api/models/app/social/Social";
import { cn } from "@/lib/utils";
import { Chips, Options, RangeValue, TextValue, Typeahead, ValueChip } from "./FilterEditors";
import { readPrefs, writePrefs } from "./prefs";
import { activeFilterCount, isSupported, valueKey, type FinderAction, type FinderState } from "./state";
import { clampPerCompany, NO_VIEW_FILTERS, viewFilterChips, withoutViewFilter, type ViewFilters } from "./view";

const ALL_TAB = "all";
const WARMBLY_TAB = "warmbly";
const SECTION = "text-[10px] uppercase tracking-[0.14em] text-slate-400 font-medium";

export function FilterGrid({
    accountId,
    catalog,
    state,
    dispatch,
    viewFilters,
    onViewFilters,
    onClearAll,
    hidden,
}: {
    /** The account lookups run on; "auto" for any licensed account, "" for none. */
    accountId: string;
    catalog: FinderCatalog;
    state: FinderState;
    dispatch: Dispatch<FinderAction>;
    viewFilters: ViewFilters;
    onViewFilters: (v: ViewFilters) => void;
    onClearAll: () => void;
    /** Rows the view filters hide now. */
    hidden: number;
}) {
    const [tab, setTabState] = useState(() => readPrefs().tab ?? ALL_TAB);
    const [find, setFind] = useState("");
    const tabs = useMemo(
        () => [
            { key: ALL_TAB, label: "All", count: activeFilterCount(state) },
            ...catalog.groups.map((g) => ({ key: g.key, label: g.label, count: g.filters.filter((f) => (state.filters[f.key]?.length ?? 0) > 0).length })),
            { key: WARMBLY_TAB, label: "Warmbly", count: viewFilterChips(viewFilters).length },
        ],
        [catalog, state, viewFilters],
    );
    const current = tabs.some((t) => t.key === tab) ? tab : ALL_TAB;
    const setTab = (t: string) => {
        setTabState(t);
        writePrefs({ tab: t });
    };

    const needle = find.trim().toLowerCase();
    const groups = needle
        ? catalog.groups
              .map((g) => ({ ...g, filters: g.filters.filter((f) => f.label.toLowerCase().includes(needle) || g.label.toLowerCase().includes(needle)) }))
              .filter((g) => g.filters.length)
        : current === ALL_TAB
          ? catalog.groups
          : catalog.groups.filter((g) => g.key === current);
    const showWarmbly = needle ? "warmbly contacts contacted campaign company".includes(needle) : current === ALL_TAB || current === WARMBLY_TAB;

    return (
        <div className="grid content-start">
            <ActiveFilters catalog={catalog} state={state} dispatch={dispatch} viewFilters={viewFilters} onViewFilters={onViewFilters} onClearAll={onClearAll} />
            <div className="sticky top-0 z-[2] grid gap-2 border-b border-slate-200 bg-white/95 px-3 pt-2 pb-2.5 backdrop-blur">
                <ScrollStrip activeKey={needle ? undefined : current} innerClassName="flex gap-1">
                    {tabs.map((t) => {
                        const on = !needle && current === t.key;
                        return (
                            <button
                                key={t.key}
                                type="button"
                                data-active={on ? "true" : undefined}
                                aria-pressed={on}
                                onClick={() => {
                                    setFind("");
                                    setTab(t.key);
                                }}
                                className={cn(
                                    "inline-flex h-6 shrink-0 items-center gap-1 rounded-md px-2 text-[12px] whitespace-nowrap transition-colors",
                                    on ? "bg-slate-900 text-white font-medium" : "text-slate-600 hover:bg-slate-100 hover:text-slate-900",
                                    t.key === WARMBLY_TAB && !on && "text-sky-700",
                                )}
                            >
                                {t.label}
                                {t.count > 0 && <span className={cn("rounded px-1 text-[10.5px] tabular-nums", on ? "bg-white/20" : "bg-slate-100 text-slate-700")}>{t.count}</span>}
                            </button>
                        );
                    })}
                </ScrollStrip>
                <SearchInput value={find} onChange={setFind} placeholder="Find a filter…" />
            </div>
            <div className="@container grid content-start gap-1 px-3 pt-2 pb-6">
                {groups.map((g) => (
                    <section key={g.key} aria-label={g.label} className="grid gap-1.5 pb-2">
                        <h3 className={cn(SECTION, "pt-2", !needle && current !== ALL_TAB && "sr-only")}>{g.label}</h3>
                        <div className="grid grid-cols-1 gap-1.5 @sm:grid-cols-2">
                            {g.filters.map((f) => (
                                <FilterCell key={f.key} filter={f} source={catalog.source} values={state.filters[f.key] ?? []} dispatch={dispatch} accountId={accountId} />
                            ))}
                        </div>
                    </section>
                ))}
                {showWarmbly && (
                    <section aria-label="Warmbly" className="grid gap-1.5 pb-2">
                        <h3 className={cn(SECTION, "pt-2 text-sky-700", !needle && current === WARMBLY_TAB && "sr-only")}>Warmbly</h3>
                        <WarmblyFilters value={viewFilters} onChange={onViewFilters} hidden={hidden} />
                    </section>
                )}
                {needle && !groups.length && !showWarmbly && <p className="py-6 text-center text-[12.5px] text-slate-500">No filter called "{find}" on {catalog.label}.</p>}
            </div>
        </div>
    );
}

// ---------------------------------------------------------------- active filters

function ActiveFilters({
    catalog,
    state,
    dispatch,
    viewFilters,
    onViewFilters,
    onClearAll,
}: {
    catalog: FinderCatalog;
    state: FinderState;
    dispatch: Dispatch<FinderAction>;
    viewFilters: ViewFilters;
    onViewFilters: (v: ViewFilters) => void;
    onClearAll: () => void;
}) {
    const active = catalog.groups.flatMap((g) => g.filters).filter((f) => (state.filters[f.key]?.length ?? 0) > 0);
    const view = viewFilterChips(viewFilters);
    const count = activeFilterCount(state);
    if (!active.length && !view.length && !state.keywords.trim()) return null;
    return (
        <div className="grid gap-1.5 border-b border-slate-200 px-3 py-2.5">
            <div className="flex items-center gap-2">
                <p className={cn(SECTION, "flex-1")}>
                    Active{count > 0 && <span className="ml-1 tabular-nums">· {count}</span>}
                </p>
                <button type="button" onClick={onClearAll} className="h-6 px-2 rounded-md text-[11.5px] text-slate-500 hover:text-slate-900 hover:bg-slate-100">
                    Clear all
                </button>
            </div>
            <ul className="flex max-h-28 flex-wrap gap-1 overflow-y-auto" aria-label="Active filters">
                {active.flatMap((f) =>
                    (state.filters[f.key] ?? []).map((v) => (
                        <ValueChip key={`${f.key}:${valueKey(v)}`} filter={f.kind === "toggle" ? { ...f, exclude: false } : f} value={v} label={summaryLabel(f, v)} dispatch={dispatch} />
                    )),
                )}
                {view.map((c) => (
                    <li key={c.key} className="inline-flex h-6 max-w-full items-center gap-1 rounded-full bg-sky-50 pr-1 pl-2 text-[11.5px] font-medium text-sky-700 ring-1 ring-inset ring-sky-200">
                        <span className="truncate">{c.label}</span>
                        <button type="button" onClick={() => onViewFilters(withoutViewFilter(viewFilters, c.key))} aria-label={`Turn off: ${c.label}`} className="grid size-4 place-items-center rounded-full hover:bg-sky-100">
                            <XIcon className="size-3" />
                        </button>
                    </li>
                ))}
            </ul>
        </div>
    );
}

/** In the summary a chip names its filter: "Seniority: Director". */
function summaryLabel(f: FinderFilter, v: FinderValue): string {
    return f.kind === "toggle" ? f.label : `${f.label}: ${v.text || v.id || ""}`;
}

// ---------------------------------------------------------------- a cell

const CELL = "relative grid min-h-[52px] content-start gap-1 rounded-md border px-2.5 py-2 text-[12.5px] transition-colors";
const CELL_CHIPS = 4;

function FilterCell({ filter: f, source, values, dispatch, accountId }: { filter: FinderFilter; source: FinderSource; values: FinderValue[]; dispatch: Dispatch<FinderAction>; accountId: string }) {
    const [open, setOpen] = useState(false);
    if (!isSupported(f)) {
        return (
            <div title={f.reason || "LinkedIn does not offer this filter through the API"} className={cn(CELL, "border-dashed border-slate-200 bg-slate-50/60 text-slate-400")}>
                <span className="flex items-center gap-1.5">
                    <LockIcon aria-hidden className="size-3 shrink-0" />
                    <span className="truncate">{f.label}</span>
                </span>
                <span className="line-clamp-1 text-[11px] leading-snug">{f.reason || "Not available through the API"}</span>
            </div>
        );
    }
    if (f.kind === "toggle") {
        const on = values.length > 0;
        return (
            <div className={cn(CELL, "cursor-pointer", on ? "border-sky-200 bg-sky-50/40" : "border-slate-200 hover:bg-slate-50")} onClick={() => dispatch({ type: "toggle", filter: f, on: !on })}>
                <span className="flex items-center gap-2">
                    <span className="min-w-0 flex-1 truncate font-medium text-slate-800">{f.label}</span>
                    <span onClick={(e) => e.stopPropagation()} className="inline-flex">
                        <Toggle value={on} onChange={(c) => dispatch({ type: "toggle", filter: f, on: c })} ariaLabel={f.label} />
                    </span>
                </span>
                {f.hint && <span className="line-clamp-1 text-[11px] leading-snug text-slate-500">{f.hint}</span>}
            </div>
        );
    }
    const shown = values.slice(0, CELL_CHIPS);
    let editor: ReactNode = null;
    if (f.kind === "typeahead") editor = <Typeahead filter={f} source={source} values={values} dispatch={dispatch} accountId={accountId} />;
    else if (f.kind === "options") editor = <Options filter={f} values={values} dispatch={dispatch} />;
    else if (f.kind === "text") editor = <TextValue filter={f} dispatch={dispatch} />;
    else if (f.kind === "range") editor = <RangeValue filter={f} value={values[0]?.id ?? ""} dispatch={dispatch} />;
    return (
        <PopoverMenu open={open} onOpenChange={setOpen}>
            <PopoverMenuTrigger asChild>
                <div className={cn(CELL, "cursor-pointer", values.length ? "border-sky-200 bg-sky-50/40" : "border-slate-200 hover:bg-slate-50", open && "ring-2 ring-sky-100 border-sky-400")}>
                    <button type="button" aria-haspopup="dialog" aria-expanded={open} className="flex w-full items-center gap-1.5 text-left font-medium text-slate-800 outline-none focus-visible:underline">
                        <span className="min-w-0 flex-1 truncate">{f.label}</span>
                        {values.length > 0 ? (
                            <span className="rounded bg-slate-900 px-1.5 py-px text-[10.5px] font-semibold text-white tabular-nums">{values.length}</span>
                        ) : (
                            <PlusIcon aria-hidden className="size-3.5 text-slate-400" />
                        )}
                    </button>
                    {values.length > 0 ? (
                        <ul className="flex min-w-0 flex-wrap gap-1" aria-label={`${f.label} values`}>
                            {shown.map((v) => (
                                <ValueChip key={valueKey(v)} filter={f} value={v} dispatch={dispatch} size="sm" />
                            ))}
                            {values.length > shown.length && <li className="self-center text-[11px] text-slate-500">+{values.length - shown.length}</li>}
                        </ul>
                    ) : (
                        <span className="truncate text-[11.5px] text-slate-400">{f.kind !== "range" && f.exclude ? "Include or exclude" : "Any"}</span>
                    )}
                </div>
            </PopoverMenuTrigger>
            <PopoverMenuContent role="dialog" aria-label={f.label} minWidth={340} className="w-[min(360px,calc(100vw-16px))] px-3 py-3">
                <div className="grid gap-2.5">
                    <div className="flex items-center gap-2">
                        <p className="flex-1 text-[12.5px] font-semibold text-slate-900">{f.label}</p>
                        {values.length > 0 && (
                            <button type="button" onClick={() => dispatch({ type: "clear", key: f.key })} className="h-6 px-2 rounded-md text-[11.5px] text-slate-500 hover:text-slate-900 hover:bg-slate-100">
                                Clear
                            </button>
                        )}
                    </div>
                    {values.length > 0 && f.kind !== "options" && f.kind !== "range" && <Chips filter={f} values={values} dispatch={dispatch} />}
                    <div className={cn(f.kind === "options" && "max-h-72 overflow-y-auto")}>{editor}</div>
                    {f.hint && <p className="text-[11.5px] leading-snug text-slate-500">{f.hint}</p>}
                    {f.exclude && f.kind !== "text" && f.kind !== "range" && values.length === 0 && (
                        <p className="text-[11.5px] leading-snug text-slate-400">Include or exclude values. Excluded values leave people out.</p>
                    )}
                </div>
            </PopoverMenuContent>
        </PopoverMenu>
    );
}

// ---------------------------------------------------------------- Warmbly's own filters

const SWITCHES: { key: "hideContacts" | "hideContacted" | "hideInCampaign" | "newCompanies"; label: string; hint: string }[] = [
    { key: "hideContacts", label: "Hide contacts", hint: "Already a contact in this workspace" },
    { key: "hideContacted", label: "Hide contacted", hint: "Sent an invite or a message from any account" },
    { key: "hideInCampaign", label: "Hide people in a campaign", hint: "In any campaign" },
    { key: "newCompanies", label: "Only new companies", hint: "Nobody at their company is a contact yet" },
];

function WarmblyFilters({ value, onChange, hidden }: { value: ViewFilters; onChange: (v: ViewFilters) => void; hidden: number }) {
    const active = viewFilterChips(value).length;
    return (
        <div className="grid gap-1.5">
            <p className="flex items-start gap-1.5 rounded-md bg-sky-50/70 px-2.5 py-2 text-[11.5px] leading-snug text-slate-700">
                <InfoIcon aria-hidden className="mt-px size-3.5 shrink-0 text-sky-600" />
                <span>
                    These hide loaded rows and limit what you select and add. <span className="font-medium">LinkedIn's count is unchanged</span> and they cost nothing.
                    {hidden > 0 && <span className="text-sky-700"> {hidden.toLocaleString("en-US")} hidden now.</span>}
                </span>
            </p>
            <div className="grid grid-cols-1 gap-1.5 @sm:grid-cols-2">
                {SWITCHES.map((s) => (
                    <div
                        key={s.key}
                        onClick={() => onChange({ ...value, [s.key]: !value[s.key] })}
                        className={cn(CELL, "cursor-pointer", value[s.key] ? "border-sky-200 bg-sky-50/40" : "border-slate-200 hover:bg-slate-50")}
                    >
                        <span className="flex items-center gap-2">
                            <span className="min-w-0 flex-1 truncate font-medium text-slate-800">{s.label}</span>
                            <span onClick={(e) => e.stopPropagation()} className="inline-flex">
                                <Toggle value={value[s.key]} onChange={(c) => onChange({ ...value, [s.key]: c })} ariaLabel={s.label} />
                            </span>
                        </span>
                        <span className="line-clamp-1 text-[11px] text-slate-500">{s.hint}</span>
                    </div>
                ))}
                <div className="flex items-center gap-2 rounded-md border border-slate-200 px-2.5 py-2 text-[12.5px] @sm:col-span-2">
                    <span className="min-w-0 flex-1">
                        <span className="block font-medium text-slate-800">At most per company</span>
                        <span className="block text-[11px] text-slate-500">Keeps the first people of each company in LinkedIn's order. 0 shows everyone.</span>
                    </span>
                    <NumberInput value={value.maxPerCompany} onChange={(n) => onChange({ ...value, maxPerCompany: clampPerCompany(n) })} min={0} max={100} className="w-20" />
                </div>
            </div>
            {active > 0 && (
                <button type="button" onClick={() => onChange(NO_VIEW_FILTERS)} className="justify-self-start text-[12px] font-medium text-slate-500 hover:text-slate-900 hover:underline underline-offset-2">
                    Show everyone again
                </button>
            )}
        </div>
    );
}
