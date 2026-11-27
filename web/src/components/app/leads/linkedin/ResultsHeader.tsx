// The sticky results header: LinkedIn's count, today's budget, the view switch, columns and the smart selections.
import { Building2Icon, CheckIcon, Columns3Icon, EyeOffIcon, GaugeIcon, LayoutListIcon, ListChecksIcon, Rows3Icon, WandSparklesIcon } from "lucide-react";
import { useState } from "react";
import { Checkbox } from "@/components/ui/checkbox";
import { CheckSquare } from "@/components/ui/check-square";
import { NumberInput } from "@/components/ui/field";
import { PopoverMenu, PopoverMenuContent, PopoverMenuLabel, PopoverMenuTrigger } from "@/components/ui/popover-menu";
import type { FinderUsage } from "@/lib/api/models/app/social/Social";
import { cn } from "@/lib/utils";
import { Tip } from "./bits";
import { TableHeader } from "./Results";
import { loadEta, showingLabel } from "./state";
import { ALL_COLUMNS, COLUMN_LABELS, VIEWS, type ResultView, type TableColumn } from "./view";

const fmt = (n: number) => n.toLocaleString("en-US");
const BTN = "h-7 px-2 rounded-md border border-slate-200 bg-white text-[12px] font-medium text-slate-700 hover:bg-slate-50 hover:text-slate-900 inline-flex items-center gap-1.5 disabled:opacity-50";

export interface FirstNProgress {
    target: number;
    loaded: number;
}

export function ResultsHeader(p: {
    loaded: number;
    total: number | null;
    cap: number;
    usage: FinderUsage | undefined;
    pageSize: number;
    selectable: string[];
    selection: Set<string>;
    onToggleAll: () => void;
    canSelect: boolean;
    busy: boolean;
    maxFirst: number;
    onSelectFirst: (n: number) => void;
    firstN: FirstNProgress | null;
    onStopFirst: () => void;
    searchedWith?: string;
    hidden: number;
    view: ResultView;
    onView: (v: ResultView) => void;
    columns: TableColumn[];
    shownColumns: TableColumn[];
    onColumns: (c: TableColumn[]) => void;
    isAccounts: boolean;
    notContacted: number;
    onSelectNotContacted: () => void;
    onSelectTopPerCompany: (n: number) => void;
}) {
    const on = p.selectable.filter((k) => p.selection.has(k)).length;
    const all = on > 0 && on === p.selectable.length;
    return (
        <div className="sticky top-0 z-[9] border-b border-slate-200 bg-white/95 backdrop-blur">
            <div className="flex min-h-12 flex-wrap items-center gap-x-3 gap-y-1.5 px-4 py-2">
                <Checkbox
                    checked={all}
                    ref={(el) => {
                        if (el) el.indeterminate = !all && on > 0;
                    }}
                    onChange={p.onToggleAll}
                    disabled={!p.canSelect || p.selectable.length === 0 || !!p.firstN}
                    aria-label={`Select all ${fmt(p.selectable.length)} loaded`}
                />
                <div className="grid min-w-0 flex-1 basis-[15rem] gap-0.5">
                    <p className="text-[12.5px] font-semibold text-slate-900 tabular-nums" aria-live="polite">
                        {showingLabel(p.loaded, p.total)}
                        {p.busy && <span className="ml-2 text-[11.5px] font-normal text-slate-500">Updating…</span>}
                    </p>
                    <p className="flex min-w-0 flex-wrap items-center gap-x-2 text-[11.5px] text-slate-500 tabular-nums">
                        {p.usage && <BudgetMeter usage={p.usage} />}
                        {p.searchedWith && <span className="truncate">· Searched with {p.searchedWith}</span>}
                        {p.hidden > 0 && (
                            <Tip content="Warmbly's filters hide loaded rows and leave them out of selections. LinkedIn's count is unchanged.">
                                <span tabIndex={0} className="inline-flex items-center gap-1 text-slate-700 outline-none">
                                    · <EyeOffIcon aria-hidden className="size-3" /> {fmt(p.hidden)} hidden by Warmbly filters
                                </span>
                            </Tip>
                        )}
                        {p.total != null && p.total > p.cap && <span className="max-sm:hidden">· LinkedIn shows the first {fmt(p.cap)} of a search</span>}
                    </p>
                </div>
                <div className="flex flex-wrap items-center gap-1.5">
                    {!p.isAccounts && <ViewSwitch view={p.view} onView={p.onView} />}
                    {!p.isAccounts && p.view === "table" && <ColumnPicker columns={p.columns} shown={p.shownColumns} onColumns={p.onColumns} />}
                    {p.canSelect && !p.firstN && !p.isAccounts && <SmartSelect notContacted={p.notContacted} onNotContacted={p.onSelectNotContacted} onTopPerCompany={p.onSelectTopPerCompany} />}
                    {p.canSelect && !p.firstN && <SelectFirst loaded={p.loaded} max={p.maxFirst} pageSize={p.pageSize} remaining={p.usage?.remaining ?? 0} onSelect={p.onSelectFirst} />}
                </div>
            </div>
            {p.firstN && <FirstNBar progress={p.firstN} onStop={p.onStopFirst} />}
            {!p.isAccounts && p.view === "table" && <TableHeader columns={p.shownColumns} />}
        </div>
    );
}

function BudgetMeter({ usage }: { usage: FinderUsage }) {
    const pct = usage.limit > 0 ? Math.min(100, (usage.used / usage.limit) * 100) : 100;
    const low = usage.remaining < Math.max(50, usage.limit * 0.1);
    return (
        <span className="inline-flex items-center gap-1.5">
            <GaugeIcon aria-hidden className="size-3.5" />
            <span className="relative inline-block h-1 w-14 overflow-hidden rounded-full bg-slate-100" aria-hidden>
                <span className={cn("absolute inset-y-0 left-0 rounded-full", low ? "bg-amber-500" : "bg-sky-500")} style={{ width: `${pct}%` }} />
            </span>
            <span>
                <span className="text-slate-800">{fmt(usage.remaining)}</span> of {fmt(usage.limit)}
                <span className="max-sm:hidden"> search results</span> left today
            </span>
        </span>
    );
}

function SelectFirst({ loaded, max, pageSize, remaining, onSelect }: { loaded: number; max: number; pageSize: number; remaining: number; onSelect: (n: number) => void }) {
    const [open, setOpen] = useState(false);
    const [n, setN] = useState(100);
    const presets = [100, 250, 500, 1000].filter((x) => x < max);
    const value = Math.min(Math.max(Math.floor(n || 0), 0), max);
    const more = Math.max(0, value - loaded);
    const chip = (v: number, label: string) => (
        <button
            key={label}
            type="button"
            onClick={() => setN(v)}
            className={cn("h-6 rounded-full border px-2.5 text-[11.5px] tabular-nums transition-colors", value === v ? "border-slate-900 bg-slate-900 text-white" : "border-slate-200 text-slate-700 hover:bg-slate-50")}
        >
            {label}
        </button>
    );
    return (
        <PopoverMenu
            align="end"
            open={open}
            onOpenChange={(o) => {
                if (o) setN(Math.min(max, Math.max(100, loaded)));
                setOpen(o);
            }}
        >
            <PopoverMenuTrigger asChild>
                <button type="button" disabled={max === 0} className={BTN}>
                    <ListChecksIcon className="size-3.5" /> <span className="max-sm:hidden">Select first…</span>
                    <span className="sm:hidden">First N</span>
                </button>
            </PopoverMenuTrigger>
            <PopoverMenuContent role="dialog" aria-label="Select the first results" minWidth={300} className="px-3 py-3">
                <form
                    className="grid gap-3"
                    onSubmit={(e) => {
                        e.preventDefault();
                        if (value < 1) return;
                        setOpen(false);
                        onSelect(value);
                    }}
                >
                    <div className="grid gap-1">
                        <p className="text-[12.5px] font-semibold text-slate-900">Select the first results</p>
                        <p className="text-[11.5px] text-slate-500">Up to {fmt(max)}: LinkedIn's cap, the result count and what today's search budget can still load.</p>
                    </div>
                    <div className="flex items-center gap-2 text-[12.5px] text-slate-500">
                        First
                        <NumberInput value={n} onChange={setN} min={1} max={max} className="w-24" />
                        results
                    </div>
                    <div className="flex flex-wrap gap-1.5">
                        {presets.map((x) => chip(x, fmt(x)))}
                        {chip(max, `All ${fmt(max)}`)}
                    </div>
                    <p className="rounded-md bg-slate-50 px-2.5 py-2 text-[11.5px] text-slate-600">
                        {more === 0
                            ? `All ${fmt(value)} are loaded already.`
                            : `Loads ${fmt(more)} more first (${loadEta(more, pageSize)}, paced to keep the account safe), using ${fmt(more)} of the ${fmt(remaining)} search results left today.`}
                    </p>
                    <button type="submit" disabled={value < 1} className="h-7 rounded-md bg-slate-900 text-white text-[12px] font-medium disabled:opacity-50">
                        Select {value >= 1 ? fmt(value) : ""}
                    </button>
                </form>
            </PopoverMenuContent>
        </PopoverMenu>
    );
}

function FirstNBar({ progress, onStop }: { progress: FirstNProgress; onStop: () => void }) {
    const pct = Math.min(100, (progress.loaded / Math.max(progress.target, 1)) * 100);
    return (
        <div className="flex items-center gap-3 border-t border-slate-100 px-4 py-2" role="status">
            <div className="grid min-w-0 flex-1 gap-1">
                <p className="text-[12px] text-slate-700 tabular-nums">
                    Loading results to select: <span className="font-medium">{fmt(Math.min(progress.loaded, progress.target))}</span> of {fmt(progress.target)}
                </p>
                <span className="relative block h-1 overflow-hidden rounded-full bg-slate-100" aria-hidden>
                    <span className="absolute inset-y-0 left-0 rounded-full bg-sky-500 transition-[width] duration-500" style={{ width: `${pct}%` }} />
                </span>
            </div>
            <button type="button" onClick={onStop} className="h-7 px-2.5 rounded-md text-[12px] text-slate-600 hover:text-slate-900 hover:bg-slate-100">
                Stop
            </button>
        </div>
    );
}

const VIEW_ICON: Record<ResultView, typeof Rows3Icon> = { table: Rows3Icon, cards: LayoutListIcon, companies: Building2Icon };

function ViewSwitch({ view, onView }: { view: ResultView; onView: (v: ResultView) => void }) {
    return (
        <div className="inline-flex rounded-md border border-slate-200 p-0.5" role="radiogroup" aria-label="Result view">
            {VIEWS.map((v) => {
                const Icon = VIEW_ICON[v.value];
                const on = view === v.value;
                return (
                    <button
                        key={v.value}
                        type="button"
                        role="radio"
                        aria-checked={on}
                        aria-label={v.label}
                        title={v.label}
                        onClick={() => onView(v.value)}
                        className={cn("h-6 px-2 rounded inline-flex items-center gap-1 text-[11.5px]", on ? "bg-slate-100 text-slate-900 font-medium" : "text-slate-500 hover:text-slate-900")}
                    >
                        <Icon className="size-3.5" />
                        <span className="max-xl:hidden">{v.label}</span>
                    </button>
                );
            })}
        </div>
    );
}

function ColumnPicker({ columns, shown, onColumns }: { columns: TableColumn[]; shown: TableColumn[]; onColumns: (c: TableColumn[]) => void }) {
    const on = new Set(columns);
    const fits = new Set(shown);
    const dropped = columns.filter((c) => !fits.has(c)).length;
    return (
        <PopoverMenu align="end">
            <PopoverMenuTrigger asChild>
                <button type="button" className={BTN} aria-label={dropped ? `Columns (${dropped} without room)` : "Columns"}>
                    <Columns3Icon className="size-3.5" /> <span className="max-xl:hidden">Columns</span>
                    {dropped > 0 && <span className="rounded bg-slate-100 px-1 text-[10.5px] tabular-nums">-{dropped}</span>}
                </button>
            </PopoverMenuTrigger>
            <PopoverMenuContent role="dialog" aria-label="Columns" minWidth={220}>
                <PopoverMenuLabel>Columns</PopoverMenuLabel>
                {ALL_COLUMNS.map((c) => {
                    const checked = on.has(c);
                    const locked = checked && on.size === 1;
                    return (
                        <button
                            key={c}
                            type="button"
                            role="menuitemcheckbox"
                            aria-checked={checked}
                            disabled={locked}
                            onClick={() => {
                                const next = new Set(on);
                                if (checked) next.delete(c);
                                else next.add(c);
                                onColumns(ALL_COLUMNS.filter((x) => next.has(x)));
                            }}
                            className="w-full h-7 px-3 flex items-center gap-2 text-[12.5px] text-slate-700 hover:bg-slate-50 disabled:opacity-60"
                        >
                            <CheckSquare checked={checked} tone="sky" />
                            <span className="min-w-0 flex-1 text-left">{COLUMN_LABELS[c]}</span>
                            {checked && !fits.has(c) && <span className="text-[11px] text-slate-400">No room</span>}
                        </button>
                    );
                })}
                {dropped > 0 && (
                    <p className="mt-1 border-t border-slate-100 px-3 pt-2 pb-1.5 text-[11.5px] leading-snug text-slate-500">
                        {dropped === 1 ? "1 column does not" : `${dropped} columns do not`} fit next to the filters. Narrow the filters, hide them, or use a wider window.
                    </p>
                )}
            </PopoverMenuContent>
        </PopoverMenu>
    );
}

function SmartSelect({ notContacted, onNotContacted, onTopPerCompany }: { notContacted: number; onNotContacted: () => void; onTopPerCompany: (n: number) => void }) {
    const [open, setOpen] = useState(false);
    return (
        <PopoverMenu align="end" open={open} onOpenChange={setOpen}>
            <PopoverMenuTrigger asChild>
                <button type="button" className={BTN}>
                    <WandSparklesIcon className="size-3.5" /> <span className="max-sm:hidden">Smart select</span>
                </button>
            </PopoverMenuTrigger>
            <PopoverMenuContent role="dialog" aria-label="Smart select" minWidth={300} className="px-3 py-3">
                <div className="grid gap-3">
                    <div className="grid gap-1">
                        <p className="text-[12.5px] font-semibold text-slate-900">Select among loaded results</p>
                        <p className="text-[11.5px] text-slate-500">Replaces the selection.</p>
                    </div>
                    <button
                        type="button"
                        disabled={notContacted === 0}
                        onClick={() => {
                            setOpen(false);
                            onNotContacted();
                        }}
                        className={cn(BTN, "justify-start")}
                    >
                        <CheckIcon className="size-3.5" /> Not contacted yet <span className="ml-auto text-slate-400 tabular-nums">{fmt(notContacted)}</span>
                    </button>
                    <div className="grid gap-1.5">
                        <p className="text-[12px] font-medium text-slate-700">Top people per company, by reach</p>
                        <div className="flex gap-1.5">
                            {[1, 2, 3, 5].map((n) => (
                                <button
                                    key={n}
                                    type="button"
                                    onClick={() => {
                                        setOpen(false);
                                        onTopPerCompany(n);
                                    }}
                                    className={BTN}
                                >
                                    Top {n}
                                </button>
                            ))}
                        </div>
                    </div>
                </div>
            </PopoverMenuContent>
        </PopoverMenu>
    );
}
