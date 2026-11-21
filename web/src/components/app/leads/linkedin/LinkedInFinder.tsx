// The lead finder's LinkedIn source: search LinkedIn, Sales Navigator and Recruiter with a connected account, the way you do on LinkedIn.
import { useQueryClient } from "@tanstack/react-query";
import { Link } from "@tanstack/react-router";
import { AnimatePresence, motion } from "framer-motion";
import { BookmarkIcon, CheckIcon, ChevronDownIcon, ClipboardPasteIcon, InfoIcon, Loader2Icon, LockIcon, MapPinnedIcon, PanelLeftCloseIcon, PanelLeftOpenIcon, SearchIcon, SlidersHorizontalIcon, SparklesIcon, TextQuoteIcon, UserPlusIcon, UsersIcon, XIcon } from "lucide-react";
import { useCallback, useEffect, useLayoutEffect, useMemo, useReducer, useRef, useState, type KeyboardEvent, type PointerEvent, type ReactNode } from "react";
import toast from "react-hot-toast/headless";
import ChannelMark from "@/components/app/social/ChannelMark";
import { SocialSetupLink } from "@/components/app/social/SocialSetup";
import { PopoverMenu, PopoverMenuContent, PopoverMenuItem, PopoverMenuLabel, PopoverMenuTrigger } from "@/components/ui/popover-menu";
import { usePermission } from "@/hooks/usePermission";
import { finderSearchQuery, useFinderCatalog, useFinderSearch, useSocialAccounts, useSocialStatus } from "@/lib/api/hooks/app/social/useSocial";
import type { FinderCatalog, FinderHit, FinderQuery, FinderSource, SavedSearch, SearchPage } from "@/lib/api/models/app/social/Social";
import { socialErrorCode, socialErrorMessage } from "@/lib/social/errors";
import { cn } from "@/lib/utils";
import { AiBox, AiReview, useFinderAi } from "./Ai";
import { ImportDialog, PasteUrlDialog, SavedSearchesMenu, SaveSearchDialog, type ImportTarget } from "./Dialogs";
import { FilterGrid } from "./FilterGrid";
import { readPrefs, writePrefs } from "./prefs";
import { PreviewPanel } from "./Preview";
import { CompanyGroupHeader, ResultCard, ResultSkeletons, TableRow } from "./Results";
import { ResultsHeader, type FirstNProgress } from "./ResultsHeader";
import { ListEnd, ResultsList } from "./ResultsList";
import {
    accountCan,
    activeFilterCount,
    apiAccount,
    AUTO_ACCOUNT,
    checkKeywords,
    finderReducer,
    firstNKeys,
    initialState,
    isEmpty,
    isSelectable,
    listTail,
    minPageCost,
    poolSummary,
    resolveAccount,
    sameQuery,
    selectFirstMax,
    toFinderAccounts,
    toggleKey,
    togglePage,
    toQuery,
} from "./state";
import { describeSearch } from "./summary";
import { FinderToolbar, sourceNeed } from "./Toolbar";
import {
    applyView,
    fitColumns,
    isView,
    listItems,
    normalizeColumns,
    notContactedKeys,
    pruneSelection,
    readViewFilters,
    SIDEBAR,
    sidebarWidth,
    topPerCompanyKeys,
    type ListItem,
    type ResultView,
    type TableColumn,
    type ViewFilters,
} from "./view";

const GROUP_COLUMNS: TableColumn[] = ["title", "location", "signals", "reach", "outreach"];

const EXAMPLES: { label: string; query: FinderQuery }[] = [
    {
        label: "Decision makers who changed jobs",
        query: {
            source: "sales_navigator_leads",
            filters: {
                seniority: [
                    { id: "CXO", text: "CXO" },
                    { id: "VICE_PRESIDENT", text: "Vice President" },
                    { id: "DIRECTOR", text: "Director" },
                ],
                changed_jobs: [{ id: "true", text: "Changed jobs" }],
            },
        },
    },
    {
        label: "Founders at 11-200 people, posting lately",
        query: {
            source: "sales_navigator_leads",
            filters: {
                seniority: [
                    { id: "OWNER/PARTNER", text: "Owner / Partner" },
                    { id: "CXO", text: "CXO" },
                ],
                company_headcount: [
                    { id: "11-50", text: "11-50" },
                    { id: "51-200", text: "51-200" },
                ],
                posted_on_linkedin: [{ id: "true", text: "Posted on LinkedIn" }],
            },
        },
    },
    { label: "Heads of growth, 2nd degree", query: { source: "classic_people", keywords: `"head of growth" OR "growth lead"`, filters: { connection: [{ id: "2", text: "2nd" }] } } },
];

function useDebouncedValue<T>(value: T, ms: number): T {
    const [v, setV] = useState(value);
    useEffect(() => {
        const t = window.setTimeout(() => setV(value), ms);
        return () => window.clearTimeout(t);
    }, [value, ms]);
    return v;
}

function useWidth(el: HTMLElement | null): number {
    const [w, setW] = useState(0);
    useLayoutEffect(() => {
        if (!el) return;
        const measure = () => setW(el.clientWidth);
        const ro = new ResizeObserver(measure);
        ro.observe(el);
        measure();
        return () => ro.disconnect();
    }, [el]);
    return w;
}

const flatHits = (pages: SearchPage[] | undefined): FinderHit[] => pages?.flatMap((p) => p.data) ?? [];
const fmt = (n: number) => n.toLocaleString("en-US");

export default function LinkedInFinder({ onExit }: { onExit: () => void }) {
    const status = useSocialStatus();
    const accounts = useSocialAccounts();
    const header = <FinderHeader onExit={onExit} />;
    if (status.isPending || (status.data?.available && accounts.isPending)) {
        return (
            <Shell header={header}>
                <div className="flex flex-1 items-center justify-center">
                    <Loader2Icon className="size-5 animate-spin text-slate-300" />
                </div>
            </Shell>
        );
    }
    if (status.isError || accounts.isError) {
        return (
            <Shell header={header}>
                <Gate icon={<InfoIcon className="size-4" />} title="LinkedIn did not load" body={socialErrorMessage(status.error ?? accounts.error)} />
            </Shell>
        );
    }
    if (!status.data?.available) {
        return (
            <Shell header={header}>
                <Gate icon={<LockIcon className="size-4" />} title="LinkedIn is not available here" body="This instance has no LinkedIn connection set up, so LinkedIn search is off." />
            </Shell>
        );
    }
    const list = (accounts.data?.data ?? []).filter((a) => a.status !== "removed" && a.channel === "linkedin");
    if (list.length === 0) {
        return (
            <Shell header={header}>
                <Gate
                    icon={<UsersIcon className="size-4" />}
                    title="Connect a LinkedIn account to search"
                    body="The LinkedIn finder searches LinkedIn, Sales Navigator and Recruiter with one of your connected accounts and its licence. Every search uses that account's daily search budget."
                    action={
                        <SocialSetupLink
                            channel="linkedin"
                            className="h-7 px-3 rounded-md bg-sky-600 text-white hover:text-white text-[12px] font-medium inline-flex items-center gap-1.5 hover:bg-sky-700 hover:no-underline"
                        >
                            <ChannelMark channel="linkedin" className="size-3.5" /> Connect LinkedIn
                        </SocialSetupLink>
                    }
                />
            </Shell>
        );
    }
    return <FinderBody onExit={onExit} />;
}

function Shell({ header, children }: { header: ReactNode; children: ReactNode }) {
    return (
        <div className="relative flex h-full flex-col bg-white">
            {header}
            {children}
        </div>
    );
}

function Gate({ icon, title, body, action }: { icon: ReactNode; title: string; body: string; action?: ReactNode }) {
    return (
        <div className="flex flex-1 items-center justify-center px-4">
            <div className="max-w-md text-center">
                <div className="mx-auto mb-3 size-9 rounded-md bg-slate-100 text-slate-500 flex items-center justify-center">{icon}</div>
                <h2 className="text-[13.5px] font-semibold text-slate-900">{title}</h2>
                <p className="mt-1 text-[12.5px] leading-relaxed text-slate-500">{body}</p>
                {action && <div className="mt-4 flex justify-center">{action}</div>}
            </div>
        </div>
    );
}

function FinderHeader({ onExit, actions }: { onExit: () => void; actions?: ReactNode }) {
    return (
        <header className="shrink-0 h-12 px-3 md:px-4 flex items-center gap-2 border-b border-slate-200">
            <MapPinnedIcon className="hidden md:block size-4 text-sky-600" />
            <h1 className="text-[13.5px] font-semibold text-slate-900">Lead finder</h1>
            <div className="h-4 w-px bg-slate-200" />
            <PopoverMenu>
                <PopoverMenuTrigger asChild>
                    <button type="button" className="h-6 px-1.5 rounded-md inline-flex items-center gap-1.5 text-[12px] text-slate-700 hover:bg-slate-100" aria-label="Lead source">
                        <ChannelMark channel="linkedin" className="size-3.5" />
                        LinkedIn
                        <ChevronDownIcon className="size-3 text-slate-400" />
                    </button>
                </PopoverMenuTrigger>
                <PopoverMenuContent minWidth={240}>
                    <PopoverMenuLabel>Find leads from</PopoverMenuLabel>
                    <PopoverMenuItem icon={<MapPinnedIcon className="size-3.5" />} onSelect={onExit} description="Places on a map, with their people and emails">
                        Businesses
                    </PopoverMenuItem>
                    <PopoverMenuItem icon={<ChannelMark channel="linkedin" className="size-3.5" />} selected trailing={<CheckIcon className="size-3.5 text-sky-600" />} description="People and companies on LinkedIn">
                        LinkedIn
                    </PopoverMenuItem>
                </PopoverMenuContent>
            </PopoverMenu>
            <div className="ml-auto flex items-center gap-1.5">{actions}</div>
        </header>
    );
}

function FinderBody({ onExit }: { onExit: () => void }) {
    const qc = useQueryClient();
    const canWrite = usePermission("MANAGE_CONTACTS");
    const catalogs = useFinderCatalog();
    const rawAccounts = useSocialAccounts();
    const accounts = useMemo(() => toFinderAccounts(rawAccounts.data?.data), [rawAccounts.data]);
    const [prefs] = useState(readPrefs);
    const [state, dispatch] = useReducer(finderReducer, undefined, () => initialState(accounts?.some((a) => a.sources.includes("sales_navigator_leads")) ? "sales_navigator_leads" : "classic_people"));
    // A specific account id, AUTO_ACCOUNT, or "" before one resolves.
    const [accountId, setAccountId] = useState(prefs.accountId ?? "");
    const [selection, setSelection] = useState<Set<string>>(() => new Set());
    const [added, setAdded] = useState<Set<string>>(() => new Set());
    const [viewFilters, setViewFiltersState] = useState<ViewFilters>(() => readViewFilters(prefs.viewFilters));
    const [view, setViewState] = useState<ResultView>(isView(prefs.view) ? prefs.view : "table");
    const [columns, setColumnsState] = useState<TableColumn[]>(() => normalizeColumns(prefs.columns));
    const [storedWidth, setStoredWidth] = useState(prefs.sidebarWidth ?? SIDEBAR.default);
    const [collapsed, setCollapsed] = useState(!!prefs.collapsed);
    const [filtersOpen, setFiltersOpen] = useState(false);
    const [pasteOpen, setPasteOpen] = useState(false);
    const [saveOpen, setSaveOpen] = useState(false);
    const [editing, setEditing] = useState<SavedSearch | null>(null);
    const [loadedSaved, setLoadedSaved] = useState<SavedSearch | null>(null);
    const [importKeys, setImportKeys] = useState<string[] | null>(null);
    const [previewKey, setPreviewKey] = useState<string | null>(null);
    const [firstN, setFirstN] = useState<FirstNProgress | null>(null);
    const [rowEl, setRowEl] = useState<HTMLDivElement | null>(null);
    const [scrollEl, setScrollEl] = useState<HTMLElement | null>(null);
    const resultsWidth = useWidth(scrollEl);
    const rowWidth = useWidth(rowEl);
    const keywordsRef = useRef<HTMLInputElement>(null);
    const aiInputRef = useRef<HTMLTextAreaElement>(null);
    const scrollToRef = useRef<((index: number) => void) | null>(null);
    const [wide, setWide] = useState(() => typeof window === "undefined" || window.matchMedia("(min-width: 768px)").matches);
    useEffect(() => {
        const mq = window.matchMedia("(min-width: 768px)");
        const on = () => setWide(mq.matches);
        mq.addEventListener("change", on);
        return () => mq.removeEventListener("change", on);
    }, []);

    const setView = (v: ResultView) => {
        setViewState(v);
        writePrefs({ view: v });
    };
    const setColumns = (c: TableColumn[]) => {
        setColumnsState(c);
        writePrefs({ columns: c });
    };
    const setViewFilters = (v: ViewFilters) => {
        setViewFiltersState(v);
        writePrefs({ viewFilters: v });
    };
    const pickAccount = (id: string) => {
        setAccountId(id);
        writePrefs({ accountId: id });
    };

    const asideWidth = sidebarWidth(storedWidth, rowWidth);
    const catalog: FinderCatalog | undefined = catalogs.data?.find((c) => c.source === state.source);
    const auto = accountId === AUTO_ACCOUNT;
    const account = auto ? undefined : accounts?.find((a) => a.id === accountId);
    const pool = poolSummary(accounts, state.source);
    const locked = !!accounts && pool.count === 0;
    const canUse = auto ? pool.count > 0 : accountCan(account, state.source);

    // The account follows the source: a picked one stays while it can search it.
    useEffect(() => {
        if (!accounts) return;
        const next = resolveAccount(accounts, state.source, accountId);
        if (next !== accountId) setAccountId(next);
    }, [accounts, state.source, accountId]);

    const query = useMemo(() => toQuery(state), [state]);
    const runQuery = useDebouncedValue(query, 550);
    const runKey = JSON.stringify(runQuery);
    const empty = isEmpty(state);
    const kw = checkKeywords(state.keywords, catalog?.product === "sales_navigator" || state.source.startsWith("sales_navigator"));
    const canSearch = canUse && kw.ok && !isEmpty({ source: runQuery.source, keywords: runQuery.keywords ?? "", filters: runQuery.filters ?? {}, url: runQuery.url ?? undefined });
    const searchAccount = apiAccount(accountId);
    // Pages load forward only and are never refetched: each costs search budget.
    const results = useFinderSearch(searchAccount, canSearch ? runQuery : null);
    const resultsKey = JSON.stringify(finderSearchQuery(searchAccount, runQuery).queryKey);
    const pages = results.data?.pages;
    const first = pages?.[0];
    const last = pages?.at(-1);
    const stale = JSON.stringify(query) !== runKey;

    // A changed search starts over: no selection, no "Added" marks, and a running "Select first" stops.
    const searchKey = useRef(resultsKey);
    const firstRun = useRef(0);
    const firstStop = useRef(false);
    useEffect(() => {
        if (searchKey.current === resultsKey) return;
        searchKey.current = resultsKey;
        firstRun.current++;
        setFirstN(null);
        setSelection(new Set());
        setAdded(new Set());
        setPreviewKey(null);
    }, [resultsKey]);

    // Each page spends budget: refresh the accounts' meters as pages arrive.
    useEffect(() => {
        if (pages?.length) void qc.invalidateQueries({ queryKey: ["linkedin", "accounts", "list"] });
    }, [pages?.length, qc]);

    // A busy account answers again in a few seconds; a stale cursor means the search starts over.
    const errCode = results.error ? socialErrorCode(results.error) : undefined;
    const fetchNextPage = results.fetchNextPage;
    const refetch = results.refetch;
    const nextFailed = results.isFetchNextPageError;
    useEffect(() => {
        if (errCode === "search_busy") {
            const t = window.setTimeout(() => (nextFailed ? void fetchNextPage({ cancelRefetch: false }) : void refetch()), 5000);
            return () => window.clearTimeout(t);
        }
        if (errCode === "stale_cursor") {
            toast("The search expired on LinkedIn, so it started over.");
            void qc.resetQueries({ queryKey: JSON.parse(resultsKey) as unknown[] });
        }
    }, [errCode, nextFailed, fetchNextPage, refetch, qc, resultsKey]);

    const hits = useMemo(() => flatHits(pages), [pages]);
    const hitByKey = useMemo(() => new Map(hits.map((h) => [h.key, h])), [hits]);
    const isAccounts = catalog?.category === "companies" || state.source === "sales_navigator_accounts";
    const visibleHits = useMemo(() => (isAccounts ? hits : applyView(hits, viewFilters)), [hits, viewFilters, isAccounts]);
    const hidden = hits.length - visibleHits.length;
    const effectiveView: ResultView = isAccounts ? "cards" : !wide && view === "table" ? "cards" : view;
    const items = useMemo(() => listItems(visibleHits, effectiveView), [visibleHits, effectiveView]);
    const tableColumns = fitColumns(columns, resultsWidth);
    const groupColumns = fitColumns(GROUP_COLUMNS, resultsWidth - 28).shown;
    const cap = first?.cap ?? catalog?.cap ?? 0;
    const total = first?.total ?? null;
    const pageSize = catalog?.page_size || (state.source === "classic_people" ? 10 : 25);
    const selectableKeys = useMemo(() => visibleHits.filter(isSelectable).map((h) => h.key), [visibleHits]);
    const nextError = results.isFetchNextPageError ? results.error : null;
    const tail = listTail({ last, loaded: hits.length, hasNext: results.hasNextPage, fetchingNext: results.isFetchingNextPage, nextError, source: state.source });
    const maxFirst = last ? selectFirstMax({ loaded: hits.length, cap, total, remaining: last.usage.remaining, hasNext: results.hasNextPage, source: state.source }) : 0;

    // Rows the view filters hide leave the selection too, so they are never imported.
    useEffect(() => {
        const visible = new Set(visibleHits.map((h) => h.key));
        setSelection((s) => pruneSelection(s, visible) ?? s);
    }, [visibleHits]);

    const loadMore = useCallback(() => void fetchNextPage({ cancelRefetch: false }), [fetchNextPage]);
    const viewFiltersRef = useRef(viewFilters);
    useEffect(() => {
        viewFiltersRef.current = viewFilters;
    }, [viewFilters]);

    // "Select the first N": load pages one after another until N rows show, then select them.
    const selectFirst = useCallback(
        async (n: number) => {
            const run = ++firstRun.current;
            firstStop.current = false;
            const shown = (rows: FinderHit[]) => (isAccounts ? rows : applyView(rows, viewFiltersRef.current));
            let rows = flatHits(results.data?.pages);
            let more = results.hasNextPage;
            let lastPage = results.data?.pages.at(-1);
            let failed: unknown = null;
            setFirstN({ target: n, loaded: shown(rows).length });
            while (run === firstRun.current && !firstStop.current && shown(rows).length < n && more && (lastPage?.usage.remaining ?? 0) >= minPageCost(state.source)) {
                const r = await fetchNextPage({ cancelRefetch: false });
                if (run !== firstRun.current) return;
                if (r.isFetchNextPageError) {
                    failed = r.error;
                    break;
                }
                rows = flatHits(r.data?.pages);
                more = r.hasNextPage;
                lastPage = r.data?.pages.at(-1);
                setFirstN({ target: n, loaded: shown(rows).length });
            }
            if (run !== firstRun.current) return;
            const stopped = firstStop.current;
            const visible = shown(rows);
            const keys = firstNKeys(visible, n);
            setSelection(new Set(keys));
            setFirstN(null);
            if (Math.min(visible.length, n) < n) {
                const why = failed ? "the next page did not load" : stopped ? "you stopped loading" : !more ? "the search has no more results" : "today's search budget ran out";
                toast(`Selected the first ${fmt(keys.length)}: ${why}.`);
            }
        },
        [results.data, results.hasNextPage, fetchNextPage, state.source, isAccounts],
    );

    const openSaved = (s: SavedSearch) => {
        dispatch({ type: "load", query: s.query });
        setLoadedSaved(s);
    };
    const loadQuery = (q: FinderQuery) => {
        dispatch({ type: "load", query: q });
        setLoadedSaved(null);
    };
    const clearAll = () => {
        dispatch({ type: "clearAll" });
        setLoadedSaved(null);
    };
    const onSource = (s: FinderSource) => {
        dispatch({ type: "source", source: s });
        setLoadedSaved(null);
    };

    const ai = useFinderAi({ state, accountId: canUse ? accountId : "" });
    const [aiNamed, setAiNamed] = useState<{ query: FinderQuery; summary: string } | null>(null);
    const takeAiSearch = (q: FinderQuery) => {
        loadQuery(q);
        if (ai.result?.summary) setAiNamed({ query: q, summary: ai.result.summary });
    };

    const toggleRow = (key: string) => {
        const h = hitByKey.get(key);
        if (!h || !isSelectable(h) || !canWrite || stale || firstN) return;
        setSelection((s) => toggleKey(s, key));
    };
    const findLeadsAt = (companies: { id: string; name: string }[]) => loadQuery({ source: "sales_navigator_leads", filters: { current_company: companies.map((c) => ({ id: c.id, text: c.name })) } });
    const openContact = (id: string) => {
        window.open(`/app/contacts?contact=${encodeURIComponent(id)}`, "_blank", "noopener");
    };

    // Selected rows import with the account that showed them; with "any account", the one the server picked.
    const importAccount = auto ? (first?.account?.id ?? null) : searchAccount;
    const keysToImport = importKeys ?? [...selection];
    const importTarget: ImportTarget = { accountId: importAccount, query: runQuery, keys: keysToImport, count: keysToImport.length };
    const sourceAvailable = (s: FinderSource) => accounts?.some((a) => accountCan(a, s)) ?? false;
    const filterCount = activeFilterCount(state);
    const summary = empty ? "" : describeSearch(query, catalog);
    const preview = previewKey ? (hitByKey.get(previewKey) ?? null) : null;
    const startAi = empty && !locked && !ai.unavailable;

    const filters = catalog ? (
        <FilterGrid accountId={canUse ? accountId : ""} catalog={catalog} state={state} dispatch={dispatch} viewFilters={viewFilters} onViewFilters={setViewFilters} onClearAll={clearAll} hidden={hidden} />
    ) : catalogs.isError ? (
        <p className="px-3 py-4 text-[12px] text-red-600">{socialErrorMessage(catalogs.error)}</p>
    ) : (
        <div className="grid gap-3 px-3 pt-4">
            {Array.from({ length: 8 }, (_, i) => (
                <span key={i} className="h-3 w-[70%] rounded bg-slate-100 animate-pulse" />
            ))}
        </div>
    );
    const aiPanel = (
        <>
            {!startAi && <AiBox ai={ai} hasSearch={!empty} inputRef={aiInputRef} />}
            {!empty && <AiReview ai={ai} state={state} dispatch={dispatch} onApplyAll={takeAiSearch} className="mx-3 my-3" />}
        </>
    );

    const renderItem = (item: ListItem) => {
        if (item.kind === "group") {
            const g = item.group;
            const groupKeys = g.hits.filter(isSelectable).map((h) => h.key);
            return (
                <CompanyGroupHeader
                    group={g}
                    selectedCount={groupKeys.filter((k) => selection.has(k)).length}
                    selectableCount={groupKeys.length}
                    canSelect={canWrite && !stale && !firstN}
                    onToggleGroup={() => setSelection((s) => togglePage(s, groupKeys))}
                    onSelectTop={(n) =>
                        setSelection((s) => {
                            const keys = new Set([...s].filter((k) => !groupKeys.includes(k)));
                            for (const k of topPerCompanyKeys(g.hits, n)) keys.add(k);
                            return keys;
                        })
                    }
                />
            );
        }
        const h = item.hit;
        const props = {
            hit: h,
            selected: selection.has(h.key),
            onToggle: () => toggleRow(h.key),
            disabled: !canWrite || !!firstN || stale || !isSelectable(h),
            added: added.has(h.key),
            active: previewKey === h.key,
            onPreview: () => setPreviewKey(h.key),
            onOpenContact: openContact,
        };
        if (effectiveView === "table") return <TableRow {...props} columns={tableColumns.shown} />;
        if (effectiveView === "companies") return <TableRow {...props} columns={groupColumns} indent />;
        return <ResultCard {...props} onPreview={h.company ? undefined : props.onPreview} onFindLeads={isAccounts ? (id, name) => findLeadsAt([{ id, name }]) : undefined} />;
    };

    const count = selection.size;
    const showBar = count > 0 && !empty && !!pages;

    return (
        <div className="relative flex h-full flex-col bg-white">
            <FinderHeader
                onExit={onExit}
                actions={
                    <>
                        <button type="button" onClick={() => setPasteOpen(true)} className="h-7 px-2 rounded-md border border-slate-200 bg-white text-[12px] font-medium text-slate-700 hover:bg-slate-50 inline-flex items-center gap-1.5" aria-label="Paste a LinkedIn URL">
                            <ClipboardPasteIcon className="size-3.5" />
                            <span className="max-sm:hidden">Paste URL</span>
                        </button>
                        <SavedSearchesMenu
                            currentId={loadedSaved?.id}
                            onOpen={openSaved}
                            onEdit={(s) => {
                                setEditing(s);
                                setSaveOpen(true);
                            }}
                        />
                    </>
                }
            />
            <FinderToolbar
                accounts={accounts}
                accountId={accountId}
                onAccount={pickAccount}
                state={state}
                dispatch={dispatch}
                onSource={onSource}
                catalog={catalog}
                kw={kw}
                keywordsRef={keywordsRef}
                linkedInUrl={first?.linkedin_url ?? null}
                filterCount={filterCount}
                onOpenFilters={() => setFiltersOpen(true)}
                sourceAvailable={sourceAvailable}
            />

            <div ref={setRowEl} className="relative flex min-h-0 flex-1">
                <Sidebar
                    width={asideWidth}
                    collapsed={collapsed}
                    filterCount={filterCount}
                    onResize={setStoredWidth}
                    onResizeEnd={(w) => writePrefs({ sidebarWidth: w })}
                    onCollapsedChange={(c) => {
                        setCollapsed(c);
                        writePrefs({ collapsed: c });
                    }}
                    onAi={
                        ai.unavailable
                            ? undefined
                            : () => {
                                  setCollapsed(false);
                                  writePrefs({ collapsed: false });
                                  requestAnimationFrame(() => aiInputRef.current?.focus());
                              }
                    }
                    title={catalog?.label}
                >
                    {aiPanel}
                    {filters}
                </Sidebar>
                <AnimatePresence>
                    {filtersOpen && (
                        <motion.div
                            key="sheet"
                            initial={{ x: -24, opacity: 0 }}
                            animate={{ x: 0, opacity: 1 }}
                            exit={{ x: -24, opacity: 0 }}
                            transition={{ duration: 0.16 }}
                            className="lg:hidden absolute inset-0 z-30 flex flex-col bg-white"
                        >
                            <div className="shrink-0 h-10 px-3 flex items-center gap-2 border-b border-slate-200">
                                <span className="text-[12.5px] font-semibold text-slate-900">Filters{filterCount ? ` (${filterCount})` : ""}</span>
                                <span className="text-[11.5px] text-slate-500 truncate">{catalog?.label}</span>
                                <button type="button" onClick={() => setFiltersOpen(false)} aria-label="Close filters" className="ml-auto size-7 rounded-md text-slate-500 hover:bg-slate-100 inline-flex items-center justify-center">
                                    <XIcon className="size-3.5" />
                                </button>
                            </div>
                            <div className="min-h-0 flex-1 overflow-y-auto">
                                {aiPanel}
                                {filters}
                            </div>
                            <div className="shrink-0 border-t border-slate-200 p-3">
                                <button type="button" onClick={() => setFiltersOpen(false)} className="h-8 w-full rounded-md bg-slate-900 text-white text-[12.5px] font-medium">
                                    Show results
                                </button>
                            </div>
                        </motion.div>
                    )}
                </AnimatePresence>

                {/* No scroll anchoring: the browser would keep the list end in view and load page after page. */}
                <section ref={setScrollEl} aria-label="Results" className="relative min-w-0 flex-1 overflow-y-auto pb-24 [overflow-anchor:none]">
                    {catalog?.note && (
                        <p className="flex items-start gap-1.5 border-b border-slate-100 bg-slate-50/60 px-4 py-2 text-[11.5px] text-slate-500">
                            <InfoIcon className="mt-px size-3.5 shrink-0" />
                            {catalog.note}
                        </p>
                    )}
                    {loadedSaved && (
                        <div className="flex items-center gap-2 border-b border-slate-100 px-4 py-2 text-[12px]">
                            <BookmarkIcon className="size-3.5 text-sky-600" />
                            <span className="min-w-0 flex-1 truncate text-slate-700">
                                Saved search <span className="font-medium text-slate-900">{loadedSaved.name}</span>
                            </span>
                            <button type="button" onClick={() => setLoadedSaved(null)} aria-label="Detach from the saved search" className="size-6 rounded text-slate-400 hover:text-slate-700 hover:bg-slate-100 inline-flex items-center justify-center">
                                <XIcon className="size-3" />
                            </button>
                        </div>
                    )}
                    {summary && !locked && (
                        <div className="flex items-start gap-2 border-b border-slate-100 px-4 py-2.5">
                            <TextQuoteIcon aria-hidden className="mt-0.5 size-3.5 shrink-0 text-slate-400" />
                            <p className="min-w-0 flex-1 text-[12.5px] leading-snug text-slate-700">{summary}</p>
                        </div>
                    )}
                    {locked ? (
                        <LockedSource source={state.source} />
                    ) : empty ? (
                        <StartPanel
                            onPaste={() => setPasteOpen(true)}
                            onExample={loadQuery}
                            available={sourceAvailable}
                            ai={
                                startAi ? (
                                    <div className="grid gap-3">
                                        <AiBox ai={ai} hasSearch={false} large inputRef={aiInputRef} />
                                        {ai.result && <AiReview ai={ai} state={state} dispatch={dispatch} onApplyAll={takeAiSearch} className="mx-auto w-full max-w-2xl" />}
                                    </div>
                                ) : undefined
                            }
                        />
                    ) : !canUse ? (
                        <Empty icon={<UsersIcon className="size-4" />} title={`No account can search ${catalog?.label ?? "this source"}`} body="Choose an account with the right LinkedIn licence, or resync it on the LinkedIn page if its plan changed." />
                    ) : !kw.ok ? (
                        <Empty icon={<SearchIcon className="size-4" />} title="Fix the keywords to search" body={kw.message ?? "These keywords cannot be searched on LinkedIn."} />
                    ) : results.isError && !pages ? (
                        <SearchError error={results.error} onRetry={() => void results.refetch()} />
                    ) : !pages ? (
                        <ResultSkeletons />
                    ) : (
                        <>
                            <ResultsHeader
                                loaded={hits.length}
                                total={total}
                                cap={cap}
                                usage={last?.usage}
                                pageSize={pageSize}
                                selectable={selectableKeys}
                                selection={selection}
                                onToggleAll={() => setSelection((s) => togglePage(s, selectableKeys))}
                                canSelect={canWrite && !stale}
                                busy={stale || (results.isFetching && !results.isFetchingNextPage)}
                                maxFirst={maxFirst}
                                onSelectFirst={(n) => void selectFirst(n)}
                                firstN={firstN}
                                onStopFirst={() => {
                                    firstStop.current = true;
                                }}
                                searchedWith={first?.account?.display_name}
                                hidden={hidden}
                                view={effectiveView}
                                onView={setView}
                                columns={columns}
                                shownColumns={tableColumns.shown}
                                onColumns={setColumns}
                                isAccounts={isAccounts}
                                notContacted={notContactedKeys(visibleHits).length}
                                onSelectNotContacted={() => {
                                    const keys = notContactedKeys(visibleHits);
                                    setSelection(new Set(keys));
                                    toast(`Selected ${fmt(keys.length)} people nobody has contacted yet.`);
                                }}
                                onSelectTopPerCompany={(n) => {
                                    const keys = topPerCompanyKeys(visibleHits, n);
                                    setSelection(new Set(keys));
                                    toast(`Selected ${fmt(keys.length)}: the top ${n} per company by reach.`);
                                }}
                            />
                            {hits.length === 0 ? (
                                <Empty
                                    icon={<SearchIcon className="size-4" />}
                                    title="No one matches this search"
                                    body="Remove a filter or an exclusion, widen the location or loosen the keywords. LinkedIn counts only people your account can see."
                                />
                            ) : (
                                <>
                                    {visibleHits.length === 0 && (
                                        <div className="mx-4 my-4 flex flex-wrap items-center gap-2 rounded-md border border-slate-200 bg-slate-50/60 px-4 py-3 text-[12px] text-slate-700">
                                            <span className="flex-1">Warmbly's filters hide all {fmt(hits.length)} loaded rows. More load as you scroll, or show everyone again.</span>
                                            <button type="button" onClick={() => setViewFilters(readViewFilters(null))} className="h-7 px-2.5 rounded-md border border-slate-200 bg-white text-[12px] font-medium text-slate-700 hover:bg-slate-50">
                                                Show everyone
                                            </button>
                                        </div>
                                    )}
                                    <ResultsList items={items} stale={stale} view={effectiveView} isAccounts={isAccounts} scrollEl={scrollEl} scrollToRef={scrollToRef} render={renderItem} />
                                    <ListEnd tail={tail} pageSize={pageSize} paused={stale || !!firstN} scrollEl={scrollEl} onLoadMore={loadMore} accountName={account?.display_name ?? first?.account?.display_name ?? ""} />
                                </>
                            )}
                        </>
                    )}
                </section>
            </div>

            <AnimatePresence>
                {showBar && (
                    <motion.div
                        initial={{ y: 16, opacity: 0 }}
                        animate={{ y: 0, opacity: 1 }}
                        exit={{ y: 16, opacity: 0 }}
                        transition={{ duration: 0.16 }}
                        className="fixed bottom-4 left-1/2 -translate-x-1/2 z-30 flex flex-wrap items-center justify-center gap-1.5 max-w-[calc(100vw-16px)] rounded-md border border-slate-200 bg-white px-2 py-1.5 shadow-[0_6px_20px_-4px_rgba(15,23,42,0.12),0_2px_4px_rgba(15,23,42,0.04)]"
                    >
                        <div className="inline-flex items-center gap-1.5 px-2 h-7 rounded bg-sky-50 text-sky-700 text-[12px] font-medium">
                            <CheckIcon className="size-3" />
                            {fmt(count)} selected
                        </div>
                        {isAccounts ? (
                            <BarAction
                                onClick={() =>
                                    findLeadsAt(
                                        hits
                                            .filter((h) => selection.has(h.key) && h.company?.provider_id)
                                            .map((h) => ({ id: h.company!.provider_id!, name: h.company!.name })),
                                    )
                                }
                            >
                                <UsersIcon className="size-3.5" /> Find leads at these
                            </BarAction>
                        ) : (
                            <BarAction disabled={!canWrite} title={canWrite ? undefined : "Adding contacts needs the Manage contacts permission"} onClick={() => setImportKeys([...selection])}>
                                <UserPlusIcon className="size-3.5" /> Add to contacts
                            </BarAction>
                        )}
                        <BarAction
                            onClick={() => {
                                setEditing(loadedSaved);
                                setSaveOpen(true);
                            }}
                        >
                            <BookmarkIcon className="size-3.5" /> {loadedSaved ? "Update search" : "Save search"}
                        </BarAction>
                        <button type="button" onClick={() => setSelection(new Set())} aria-label="Clear selection" className="size-7 rounded-md text-slate-500 hover:text-slate-900 hover:bg-slate-100 inline-flex items-center justify-center">
                            <XIcon className="size-3.5" />
                        </button>
                    </motion.div>
                )}
            </AnimatePresence>
            {!showBar && !empty && pages && (
                <button
                    type="button"
                    onClick={() => {
                        setEditing(loadedSaved);
                        setSaveOpen(true);
                    }}
                    className="fixed bottom-4 right-4 z-20 h-8 px-3 rounded-md border border-slate-200 bg-white text-[12px] font-medium text-slate-700 shadow-sm hover:bg-slate-50 inline-flex items-center gap-1.5"
                >
                    <BookmarkIcon className="size-3.5" /> {loadedSaved ? "Update search" : "Save search"}
                </button>
            )}

            <PreviewPanel
                hit={preview}
                accountId={accountId}
                selected={!!preview && selection.has(preview.key)}
                canSelect={canWrite && !stale && !firstN}
                onClose={() => setPreviewKey(null)}
                onToggle={() => preview && toggleRow(preview.key)}
                onAdd={() => preview && setImportKeys([preview.key])}
                onOpenContact={openContact}
                onFindAtCompany={
                    preview?.insights?.current_company
                        ? () => {
                              const ins = preview.insights!;
                              setPreviewKey(null);
                              loadQuery(
                                  ins.current_company_id
                                      ? { source: "sales_navigator_leads", filters: { current_company: [{ id: ins.current_company_id, text: ins.current_company }] } }
                                      : { source: "sales_navigator_leads", keywords: `"${(ins.current_company ?? "").replace(/"/g, "")}"` },
                              );
                          }
                        : undefined
                }
            />
            <PasteUrlDialog open={pasteOpen} onClose={() => setPasteOpen(false)} onLoad={loadQuery} />
            <ImportDialog
                open={importKeys !== null}
                onClose={() => setImportKeys(null)}
                target={importTarget}
                suggestedSegment={(loadedSaved?.name ?? summary).slice(0, 80)}
                onDone={() => {
                    setAdded((a) => new Set([...a, ...keysToImport]));
                    if (!importKeys || importKeys.length > 1) setSelection(new Set());
                }}
            />
            <SaveSearchDialog
                open={saveOpen}
                onClose={() => setSaveOpen(false)}
                query={editing && editing.id !== loadedSaved?.id ? editing.query : query}
                suggestedName={(!editing && aiNamed && sameQuery(aiNamed.query, query) ? aiNamed.summary : "") || describeSearch(query, catalog) || (catalog?.label ?? "LinkedIn search")}
                editing={editing}
                onSaved={(s) => setLoadedSaved(s)}
            />
        </div>
    );
}

function BarAction({ children, onClick, disabled, title }: { children: ReactNode; onClick: () => void; disabled?: boolean; title?: string }) {
    return (
        <button type="button" onClick={onClick} disabled={disabled} title={title} className="h-7 px-2.5 rounded-md text-[12px] font-medium text-slate-700 hover:text-slate-900 hover:bg-slate-100 inline-flex items-center gap-1.5 disabled:opacity-50">
            {children}
        </button>
    );
}

function Empty({ icon, title, body, action }: { icon: ReactNode; title: string; body: string; action?: ReactNode }) {
    return (
        <div className="px-4 py-12 text-center">
            <div className="mx-auto mb-3 size-9 rounded-md bg-slate-100 text-slate-500 flex items-center justify-center">{icon}</div>
            <h2 className="text-[13px] font-semibold text-slate-900">{title}</h2>
            <p className="mx-auto mt-1 max-w-md text-[12px] leading-relaxed text-slate-500">{body}</p>
            {action && <div className="mt-4 flex justify-center gap-2">{action}</div>}
        </div>
    );
}

function SearchError({ error, onRetry }: { error: unknown; onRetry: () => void }) {
    const code = socialErrorCode(error);
    const limit = code === "search_limit_reached";
    const busy = code === "search_busy";
    return (
        <Empty
            icon={limit ? <InfoIcon className="size-4" /> : busy ? <Loader2Icon className="size-4 animate-spin" /> : <XIcon className="size-4" />}
            title={limit ? "Today's search budget is used" : code === "licence_required" ? "No account can run this search" : busy ? "The account is busy, trying again" : "The search did not run"}
            body={socialErrorMessage(error)}
            action={
                limit ? (
                    <Link to="/app/settings/social" className="h-7 px-3 rounded-md border border-slate-200 bg-white text-[12px] font-medium text-slate-700 hover:bg-slate-50 inline-flex items-center">
                        Account limits
                    </Link>
                ) : code === "licence_required" || busy ? undefined : (
                    <button type="button" onClick={onRetry} className="h-7 px-3 rounded-md border border-slate-200 bg-white text-[12px] font-medium text-slate-700 hover:bg-slate-50">
                        Try again
                    </button>
                )
            }
        />
    );
}

function LockedSource({ source }: { source: FinderSource }) {
    const sn = source.startsWith("sales_navigator");
    return (
        <Empty
            icon={<LockIcon className="size-4" />}
            title={sn ? "Needs Sales Navigator" : source === "recruiter_people" ? "Needs LinkedIn Recruiter" : "Reconnect a LinkedIn account"}
            body={
                sn
                    ? "Sales Navigator search needs a Sales Navigator seat on one of your connected LinkedIn accounts. Add it on LinkedIn, then resync the account."
                    : source === "recruiter_people"
                      ? "Recruiter search needs a Recruiter seat on one of your connected LinkedIn accounts. Add it on LinkedIn, then resync the account."
                      : `None of your LinkedIn accounts is active. Searching needs ${sourceNeed(source)}.`
            }
            action={
                <Link to="/app/settings/social" className="h-7 px-3 rounded-md border border-slate-200 bg-white text-[12px] font-medium text-slate-700 hover:bg-slate-50 inline-flex items-center">
                    Open LinkedIn accounts
                </Link>
            }
        />
    );
}

function StartPanel({ onPaste, onExample, available, ai }: { onPaste: () => void; onExample: (q: FinderQuery) => void; available: (s: FinderSource) => boolean; ai?: ReactNode }) {
    return (
        <div className="grid gap-6 px-4 py-10 md:px-8">
            {ai}
            <div className="mx-auto max-w-lg text-center">
                <div className="mx-auto mb-3 size-9 rounded-md bg-sky-50 text-sky-600 flex items-center justify-center">
                    <SearchIcon className="size-4" />
                </div>
                <h2 className="text-[13.5px] font-semibold text-slate-900">{ai ? "Or build it yourself" : "Search LinkedIn the way you do on LinkedIn"}</h2>
                <p className="mt-1 text-[12.5px] leading-relaxed text-slate-500">
                    Add filters on the left, type keywords above, or paste a search you already built on LinkedIn or Sales Navigator. Results show who is already a contact, what your accounts already did with them and how easy they are to reach.
                </p>
                <button type="button" onClick={onPaste} className="mt-4 h-7 px-3 rounded-md border border-slate-200 bg-white text-[12px] font-medium text-slate-700 hover:bg-slate-50 inline-flex items-center gap-1.5">
                    <ClipboardPasteIcon className="size-3.5" /> Paste a LinkedIn search URL
                </button>
            </div>
            <div className="mx-auto grid max-w-xl gap-2">
                <p className="text-center text-[10px] uppercase tracking-[0.14em] text-slate-400 font-medium">Or start from</p>
                <div className="flex flex-wrap justify-center gap-2">
                    {EXAMPLES.filter((e) => available(e.query.source)).map((e) => (
                        <button key={e.label} type="button" onClick={() => onExample(e.query)} className="rounded-full border border-slate-200 bg-white px-3 py-1 text-[12px] text-slate-700 hover:border-slate-300 hover:bg-slate-50">
                            {e.label}
                        </button>
                    ))}
                </div>
            </div>
        </div>
    );
}

/** The filter sidebar on desktop: resizable by dragging or the arrow keys (double-click resets), collapsible to a rail. */
function Sidebar({
    width,
    collapsed,
    filterCount,
    onResize,
    onResizeEnd,
    onCollapsedChange,
    onAi,
    title,
    children,
}: {
    width: number;
    collapsed: boolean;
    filterCount: number;
    onResize: (w: number) => void;
    onResizeEnd: (w: number) => void;
    onCollapsedChange: (c: boolean) => void;
    onAi?: () => void;
    title?: string;
    children: ReactNode;
}) {
    const drag = useRef<{ x: number; w: number; last: number } | null>(null);
    if (collapsed) {
        return (
            <aside aria-label="Filters" className="hidden lg:flex shrink-0 flex-col items-center gap-1.5 border-r border-slate-200 bg-slate-50/50 py-2" style={{ width: SIDEBAR.rail }}>
                <button type="button" title="Show filters" aria-label="Show filters" onClick={() => onCollapsedChange(false)} className="size-8 rounded-md text-slate-500 hover:text-slate-900 hover:bg-slate-100 inline-flex items-center justify-center">
                    <PanelLeftOpenIcon className="size-4" />
                </button>
                <button type="button" title={filterCount ? `${filterCount} filters on` : "Filters"} aria-label="Filters" onClick={() => onCollapsedChange(false)} className="relative size-8 rounded-md text-slate-500 hover:text-slate-900 hover:bg-slate-100 inline-flex items-center justify-center">
                    <SlidersHorizontalIcon className="size-4" />
                    {filterCount > 0 && <span className="absolute -top-0.5 -right-0.5 rounded bg-slate-900 px-1 text-[10px] font-semibold text-white tabular-nums">{filterCount}</span>}
                </button>
                {onAi && (
                    <button type="button" title="Describe who you want to reach" aria-label="Describe who you want to reach" onClick={onAi} className="size-8 rounded-md text-sky-600 hover:bg-sky-50 inline-flex items-center justify-center">
                        <SparklesIcon className="size-4" />
                    </button>
                )}
            </aside>
        );
    }
    const clamp = (w: number) => Math.round(Math.min(Math.max(w, SIDEBAR.min), SIDEBAR.max));
    const onPointerDown = (e: PointerEvent<HTMLDivElement>) => {
        e.preventDefault();
        e.currentTarget.setPointerCapture(e.pointerId);
        drag.current = { x: e.clientX, w: width, last: width };
    };
    const onPointerMove = (e: PointerEvent<HTMLDivElement>) => {
        const d = drag.current;
        if (!d) return;
        d.last = clamp(d.w + e.clientX - d.x);
        onResize(d.last);
    };
    const onPointerUp = () => {
        const d = drag.current;
        drag.current = null;
        if (d) onResizeEnd(d.last);
    };
    const onKey = (e: KeyboardEvent<HTMLDivElement>) => {
        const step = e.shiftKey ? 64 : 16;
        let w: number | null = null;
        if (e.key === "ArrowLeft") w = width - step;
        else if (e.key === "ArrowRight") w = width + step;
        else if (e.key === "Home") w = SIDEBAR.min;
        else if (e.key === "End") w = SIDEBAR.max;
        if (w === null) return;
        e.preventDefault();
        w = clamp(w);
        onResize(w);
        onResizeEnd(w);
    };
    return (
        <aside aria-label="Filters" className="relative hidden lg:flex shrink-0 flex-col border-r border-slate-200" style={{ width }}>
            <div className="shrink-0 h-10 flex items-center gap-2 border-b border-slate-200 px-3">
                <h2 className="min-w-0 flex-1 truncate text-[12.5px] font-semibold text-slate-900">
                    Filters{filterCount > 0 && <span className="ml-1.5 text-slate-400 tabular-nums">{filterCount}</span>}
                    {title && <span className="ml-2 text-[11.5px] font-normal text-slate-500">{title}</span>}
                </h2>
                <button type="button" title="Hide filters" aria-label="Hide filters" onClick={() => onCollapsedChange(true)} className="size-7 rounded-md text-slate-500 hover:text-slate-900 hover:bg-slate-100 inline-flex items-center justify-center">
                    <PanelLeftCloseIcon className="size-3.5" />
                </button>
            </div>
            <div className="min-h-0 flex-1 overflow-y-auto overscroll-contain">{children}</div>
            <div
                role="separator"
                aria-orientation="vertical"
                aria-label="Resize filters"
                aria-valuemin={SIDEBAR.min}
                aria-valuemax={SIDEBAR.max}
                aria-valuenow={width}
                tabIndex={0}
                onPointerDown={onPointerDown}
                onPointerMove={onPointerMove}
                onPointerUp={onPointerUp}
                onPointerCancel={onPointerUp}
                onKeyDown={onKey}
                onDoubleClick={() => {
                    onResize(SIDEBAR.default);
                    onResizeEnd(SIDEBAR.default);
                }}
                className={cn(
                    "absolute inset-y-0 -right-1.5 z-[3] w-3 cursor-col-resize touch-none outline-none",
                    "after:absolute after:inset-y-0 after:left-1/2 after:w-px after:-translate-x-1/2 after:bg-transparent after:transition-colors hover:after:w-0.5 hover:after:bg-sky-400 focus-visible:after:w-0.5 focus-visible:after:bg-sky-500",
                )}
            />
        </aside>
    );
}
