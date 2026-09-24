// The mailbox picker the vendor and admin-grant imports share: search, a
// filter for what is connected already, a workspace switcher when the rows
// span several, checkbox rows and paging. The caller owns the selection so the
// wizard's floating bar can act on it.
import React from "react";
import { ChevronLeftIcon, ChevronRightIcon, LayersIcon, MailIcon, RefreshCwIcon } from "lucide-react";
import { SearchInput } from "@/components/ui/field";
import { CheckSquare } from "@/components/ui/check-square";
import ProviderLogo from "@/components/app/emails/ProviderLogo";
import { cn } from "@/lib/utils";
import { Pill } from "./parts";
import { Discovering, RefreshBar } from "./Discovering";

export interface PickItem {
    id: string;
    email: string;
    name: string;
    domain?: string;
    /** A second line under the domain, such as the vendor workspace the mailbox sits in. */
    group?: string;
    /** Shows a Google / Microsoft / SMTP badge when set. */
    provider?: "google" | "microsoft" | "smtp" | "";
    status?: { label: string; cls: string };
    connected: boolean;
    /** Connected here on its own sign-in; picking it moves it onto this source, keeping its history. */
    upgrade?: boolean;
    /** Set when the row cannot be picked, with why. */
    disabledReason?: string;
}

type Filter = "all" | "new" | "moving" | "connected";

const PAGE_SIZE = 50;

const PROVIDER_LABEL: Record<"google" | "microsoft" | "smtp", string> = { google: "Google", microsoft: "Microsoft", smtp: "SMTP" };

export function ProviderBadge({ provider }: { provider?: PickItem["provider"] }) {
    if (!provider) return <span className="text-[11px] text-slate-300">Unknown</span>;
    return (
        <span className="inline-flex items-center gap-1 h-[18px] px-1.5 rounded border border-slate-200 bg-white text-[10.5px] font-medium text-slate-600 whitespace-nowrap">
            <ProviderLogo id={provider} size="xs" framed={false} title="" />
            {PROVIDER_LABEL[provider]}
        </span>
    );
}

export default function PickTable({
    items,
    loading,
    fetching,
    error,
    onRefresh,
    selected,
    setSelected,
    noun,
    loadingLogo,
    loadingSteps,
    groupLabel = "Workspace",
}: {
    items: PickItem[] | undefined;
    /** Whose mailboxes are being listed, and what is happening while they are. */
    loadingLogo?: string;
    loadingSteps?: string[];
    loading: boolean;
    fetching?: boolean;
    error?: string | null;
    onRefresh?: () => void;
    selected: Set<string>;
    setSelected: React.Dispatch<React.SetStateAction<Set<string>>>;
    noun: { one: string; many: string };
    /** What PickItem.group names, for the switcher shown when rows span several. */
    groupLabel?: string;
}) {
    const [query, setQuery] = React.useState("");
    const [filter, setFilter] = React.useState<Filter>("all");
    const [group, setGroup] = React.useState<string | null>(null);
    const [page, setPage] = React.useState(0);

    const all = React.useMemo(() => items ?? [], [items]);
    // Each group with its rows, in the order the source lists them; only worth a switcher with two or more.
    const groups = React.useMemo(() => {
        const by = new Map<string, PickItem[]>();
        for (const i of all) {
            if (!i.group) continue;
            const list = by.get(i.group);
            if (list) list.push(i);
            else by.set(i.group, [i]);
        }
        return by.size > 1 ? [...by.entries()].map(([name, rows]) => ({ name, rows })) : [];
    }, [all]);
    // A group that disappears on reload drops back to every row.
    const activeGroup = group !== null && groups.some((g) => g.name === group) ? group : null;
    const inScope = React.useMemo(() => (activeGroup === null ? all : all.filter((i) => i.group === activeGroup)), [all, activeGroup]);
    const showProvider = all.some((i) => i.provider !== undefined);
    const counts = React.useMemo(
        () => ({
            all: inScope.length,
            new: inScope.filter((i) => !i.connected).length,
            moving: inScope.filter((i) => i.connected && i.upgrade).length,
            connected: inScope.filter((i) => i.connected).length,
        }),
        [inScope],
    );

    const q = query.trim().toLowerCase();
    const shown = React.useMemo(
        () =>
            inScope.filter((i) => {
                if (filter === "new" && i.connected) return false;
                if (filter === "connected" && !i.connected) return false;
                if (filter === "moving" && !(i.connected && i.upgrade)) return false;
                if (!q) return true;
                return [i.email, i.name, i.domain ?? "", i.group ?? ""].some((v) => v.toLowerCase().includes(q));
            }),
        [inScope, filter, q],
    );

    // A new search, filter or group starts from the first page.
    React.useEffect(() => setPage(0), [q, filter, activeGroup]);

    const pages = Math.max(1, Math.ceil(shown.length / PAGE_SIZE));
    const at = Math.min(page, pages - 1);
    const slice = shown.slice(at * PAGE_SIZE, at * PAGE_SIZE + PAGE_SIZE);

    const pickable = shown.filter((i) => !i.disabledReason);
    const allShownPicked = pickable.length > 0 && pickable.every((i) => selected.has(i.id));

    const toggle = (item: PickItem) => {
        if (item.disabledReason) return;
        setSelected((prev) => {
            const next = new Set(prev);
            if (next.has(item.id)) next.delete(item.id);
            else next.add(item.id);
            return next;
        });
    };

    // What "select the group" picks: rows that would be new here, or that move onto this source.
    const wanted = (i: PickItem) => !i.disabledReason && (!i.connected || !!i.upgrade);
    const setGroupPicked = (rows: PickItem[], on: boolean) =>
        setSelected((prev) => {
            const next = new Set(prev);
            for (const i of rows) {
                if (!wanted(i)) continue;
                if (on) next.add(i.id);
                else next.delete(i.id);
            }
            return next;
        });

    const toggleShown = () =>
        setSelected((prev) => {
            const next = new Set(prev);
            if (allShownPicked) for (const i of pickable) next.delete(i.id);
            else for (const i of pickable) next.add(i.id);
            return next;
        });

    if (loading) {
        return (
            <Discovering
                logo={loadingLogo}
                icon={<MailIcon className="w-5 h-5 text-sky-600" />}
                steps={loadingSteps?.length ? loadingSteps : [`Loading ${noun.many}…`]}
            />
        );
    }
    if (error) {
        return (
            <div className="rounded-md border border-red-200 bg-red-50 px-3 py-3 flex items-start gap-2.5">
                <div className="min-w-0 flex-1">
                    <p className="text-[12.5px] font-medium text-red-900">Could not load the {noun.many}</p>
                    <p className="text-[11.5px] text-red-800/90 mt-0.5 leading-relaxed">{error}</p>
                </div>
                {onRefresh && (
                    <button
                        type="button"
                        onClick={onRefresh}
                        className="shrink-0 h-7 px-2.5 rounded-md border border-red-200 bg-white text-[12px] text-red-800 hover:bg-red-50 inline-flex items-center gap-1.5 transition-colors"
                    >
                        <RefreshCwIcon className="w-3 h-3" />
                        Try again
                    </button>
                )}
            </div>
        );
    }

    return (
        <div>
            <div className="flex items-center gap-2 mb-2 flex-wrap">
                <SearchInput value={query} onChange={setQuery} placeholder={`Search ${noun.many}`} className="flex-1 min-w-[180px]" />
                <div className="flex items-center gap-1">
                    {(
                        [
                            ["all", "All"],
                            ["new", "Not connected"],
                            ["moving", "To move"],
                            ["connected", "Connected"],
                        ] as const
                    )
                        .filter(([key]) => key !== "moving" || counts.moving > 0)
                        .map(([key, label]) => (
                            <button
                                key={key}
                                type="button"
                                onClick={() => setFilter(key)}
                                className={cn(
                                    "h-6 px-2 rounded-md text-[11.5px] font-medium transition-colors inline-flex items-center gap-1",
                                    filter === key ? "bg-sky-50 text-sky-700" : "text-slate-500 hover:text-slate-900 hover:bg-slate-100",
                                )}
                            >
                                {label}
                                <span className="tabular-nums opacity-70">{counts[key].toLocaleString()}</span>
                            </button>
                        ))}
                </div>
                {onRefresh && (
                    <button
                        type="button"
                        onClick={onRefresh}
                        aria-label="Reload"
                        title="Reload"
                        className="size-7 rounded-md text-slate-500 hover:text-slate-900 hover:bg-slate-100 inline-flex items-center justify-center transition-colors"
                    >
                        <RefreshCwIcon className={cn("w-3.5 h-3.5", fetching && "animate-spin")} />
                    </button>
                )}
            </div>

            {groups.length > 0 && (
                <GroupSwitcher
                    label={groupLabel}
                    groups={groups}
                    total={all.length}
                    active={activeGroup}
                    onPick={setGroup}
                    selected={selected}
                    wanted={wanted}
                    onPickAll={setGroupPicked}
                />
            )}

            <div className="relative rounded-md border border-slate-200 overflow-hidden">
                <RefreshBar active={!!fetching} />
                <div
                    className={cn(
                        "grid gap-2 px-3 py-1.5 bg-slate-50/60 border-b border-slate-200 items-center",
                        showProvider
                            ? "grid-cols-[18px_minmax(0,1fr)_auto] sm:grid-cols-[18px_minmax(0,1.4fr)_minmax(0,1fr)_96px_110px]"
                            : "grid-cols-[18px_minmax(0,1fr)_auto] sm:grid-cols-[18px_minmax(0,1.4fr)_minmax(0,1fr)_110px]",
                    )}
                >
                    <button
                        type="button"
                        onClick={toggleShown}
                        disabled={pickable.length === 0}
                        aria-label={allShownPicked ? "Clear the shown rows" : "Select the shown rows"}
                        className="inline-flex items-center justify-center disabled:opacity-40"
                    >
                        <CheckSquare checked={allShownPicked} />
                    </button>
                    <span className="text-[10px] font-medium text-slate-400 uppercase tracking-[0.14em]">Mailbox</span>
                    <span className="hidden sm:block text-[10px] font-medium text-slate-400 uppercase tracking-[0.14em]">Domain</span>
                    {showProvider && (
                        <span className="hidden sm:block text-[10px] font-medium text-slate-400 uppercase tracking-[0.14em]">Provider</span>
                    )}
                    <span className="text-[10px] font-medium text-slate-400 uppercase tracking-[0.14em] text-right sm:text-left">Status</span>
                </div>
                {slice.length === 0 ? (
                    <div className="px-3 py-8 text-center text-[11.5px] text-slate-400">
                        {all.length === 0 ? `No ${noun.many} here yet.` : `No ${noun.many} match.`}
                    </div>
                ) : (
                    slice.map((item) => {
                        const on = selected.has(item.id);
                        return (
                            <button
                                key={item.id}
                                type="button"
                                onClick={() => toggle(item)}
                                aria-pressed={on}
                                disabled={!!item.disabledReason}
                                title={item.disabledReason}
                                className={cn(
                                    "w-full grid gap-2 px-3 py-2 items-center text-left border-b border-slate-100 last:border-b-0 transition-colors",
                                    showProvider
                                        ? "grid-cols-[18px_minmax(0,1fr)_auto] sm:grid-cols-[18px_minmax(0,1.4fr)_minmax(0,1fr)_96px_110px]"
                                        : "grid-cols-[18px_minmax(0,1fr)_auto] sm:grid-cols-[18px_minmax(0,1.4fr)_minmax(0,1fr)_110px]",
                                    item.disabledReason ? "opacity-60 cursor-not-allowed" : on ? "bg-sky-50/50" : "hover:bg-slate-50",
                                )}
                            >
                                <CheckSquare checked={on} />
                                <span className="min-w-0">
                                    <span className="block text-[12px] text-slate-800 truncate">{item.email}</span>
                                    {item.name && <span className="block text-[11px] text-slate-500 truncate">{item.name}</span>}
                                </span>
                                <span className="hidden sm:block min-w-0">
                                    <span className="block text-[11.5px] text-slate-600 truncate">{item.domain || "-"}</span>
                                    {item.group && activeGroup === null && (
                                        <span className="block text-[11px] text-slate-400 truncate">{item.group}</span>
                                    )}
                                </span>
                                {showProvider && (
                                    <span className="hidden sm:block">
                                        <ProviderBadge provider={item.provider} />
                                    </span>
                                )}
                                <span className="flex items-center gap-1 flex-wrap justify-end sm:justify-start">
                                    {item.connected && item.upgrade ? (
                                        <Pill className="text-amber-700 bg-amber-50" title="Moves to the admin grant, keeps its history">
                                            Moves here
                                        </Pill>
                                    ) : (
                                        item.connected && <Pill className="text-emerald-700 bg-emerald-50">Connected</Pill>
                                    )}
                                    {item.status && <Pill className={item.status.cls}>{item.status.label}</Pill>}
                                </span>
                            </button>
                        );
                    })
                )}
                {pages > 1 && (
                    <div className="px-3 h-9 border-t border-slate-100 bg-slate-50/40 flex items-center gap-1">
                        <span className="text-[11px] text-slate-500 tabular-nums">
                            {(at * PAGE_SIZE + 1).toLocaleString()}-{Math.min(shown.length, (at + 1) * PAGE_SIZE).toLocaleString()} of{" "}
                            {shown.length.toLocaleString()}
                        </span>
                        <button
                            type="button"
                            aria-label="Previous page"
                            disabled={at === 0}
                            onClick={() => setPage(at - 1)}
                            className="ml-auto size-6 rounded-md border border-slate-200 bg-white text-slate-500 hover:text-slate-900 inline-flex items-center justify-center disabled:opacity-40 transition-colors"
                        >
                            <ChevronLeftIcon className="w-3 h-3" />
                        </button>
                        <button
                            type="button"
                            aria-label="Next page"
                            disabled={at >= pages - 1}
                            onClick={() => setPage(at + 1)}
                            className="size-6 rounded-md border border-slate-200 bg-white text-slate-500 hover:text-slate-900 inline-flex items-center justify-center disabled:opacity-40 transition-colors"
                        >
                            <ChevronRightIcon className="w-3 h-3" />
                        </button>
                    </div>
                )}
            </div>
        </div>
    );
}

// GroupSwitcher narrows the table to one workspace and picks or clears a whole
// workspace in one click, with how many of each are picked already.
function GroupSwitcher({
    label,
    groups,
    total,
    active,
    onPick,
    selected,
    wanted,
    onPickAll,
}: {
    label: string;
    groups: { name: string; rows: PickItem[] }[];
    total: number;
    active: string | null;
    onPick: (group: string | null) => void;
    selected: Set<string>;
    wanted: (i: PickItem) => boolean;
    onPickAll: (rows: PickItem[], on: boolean) => void;
}) {
    const current = groups.find((g) => g.name === active);
    const pickable = current ? current.rows.filter(wanted) : [];
    const picked = pickable.filter((i) => selected.has(i.id)).length;
    const chip = (key: string | null, name: string, count: number, pickedHere: number) => {
        const on = active === key;
        return (
            <button
                key={key ?? "__all"}
                type="button"
                onClick={() => onPick(key)}
                aria-pressed={on}
                className={cn(
                    "shrink-0 h-6 px-2 rounded-md border text-[11.5px] font-medium inline-flex items-center gap-1.5 transition-colors max-w-[220px]",
                    on ? "border-sky-200 bg-sky-50 text-sky-700" : "border-slate-200 bg-white text-slate-600 hover:text-slate-900 hover:bg-slate-50",
                )}
            >
                <span className="truncate">{name}</span>
                <span className="tabular-nums opacity-70">{count.toLocaleString()}</span>
                {pickedHere > 0 && (
                    <span className="tabular-nums h-4 min-w-4 px-1 rounded bg-sky-600 text-white text-[10px] leading-4 text-center">
                        {pickedHere.toLocaleString()}
                    </span>
                )}
            </button>
        );
    };
    return (
        <div className="mb-2 rounded-md border border-slate-200 bg-slate-50/50 px-2.5 py-2">
            <div className="flex items-center gap-1.5 mb-1.5">
                <LayersIcon className="w-3 h-3 text-slate-400" />
                <span className="text-[10px] font-medium text-slate-400 uppercase tracking-[0.14em]">
                    {groups.length} {label.toLowerCase()}s
                </span>
                {current && pickable.length > 0 && (
                    <button
                        type="button"
                        onClick={() => onPickAll(current.rows, picked < pickable.length)}
                        className="ml-auto h-6 px-2 rounded-md text-[11.5px] font-medium text-sky-700 hover:bg-sky-50 inline-flex items-center gap-1 transition-colors"
                    >
                        {picked < pickable.length
                            ? `Select ${(pickable.length - picked).toLocaleString()} in ${current.name}`
                            : `Clear ${current.name}`}
                    </button>
                )}
            </div>
            <div className="flex items-center gap-1 overflow-x-auto overflow-y-hidden no-scrollbar">
                {chip(null, `All ${label.toLowerCase()}s`, total, 0)}
                {groups.map((g) => chip(g.name, g.name, g.rows.length, g.rows.filter((i) => selected.has(i.id)).length))}
            </div>
        </div>
    );
}
