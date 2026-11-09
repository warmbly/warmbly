import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import toast from "react-hot-toast/headless";
import { motion } from "framer-motion";
import {
    CheckIcon,
    FocusIcon,
    HandIcon,
    LassoIcon,
    LayersIcon,
    ListIcon,
    Loader2Icon,
    MapIcon,
    MapPinnedIcon,
    MinusIcon,
    PanelLeftIcon,
    PentagonIcon,
    PlusIcon,
    RotateCw,
    SquareIcon,
    CircleIcon,
    XIcon,
    Columns2Icon,
    BoxIcon,
} from "lucide-react";
import { PopoverMenu, PopoverMenuContent, PopoverMenuItem, PopoverMenuLabel, PopoverMenuSeparator, PopoverMenuTrigger } from "@/components/ui/popover-menu";
import AddToContactsDialog from "@/components/app/leads/AddToContactsDialog";
import EnrichmentProgress from "@/components/app/leads/EnrichmentProgress";
import ExportDialog from "@/components/app/leads/ExportDialog";
import LeadFilterBar from "@/components/app/leads/LeadFilterBar";
import { EMPTY_REFINE, refineFilter, type Refine } from "@/components/app/leads/refine";
import { LEGEND } from "@/components/app/leads/legend";
import LeadDrawer from "@/components/app/leads/LeadDrawer";
import LeadMap, { type ColorBy, type FlyTarget, type MapControls, type MapLayerMode, type MapTool, type MapView } from "@/components/app/leads/LeadMap";
import LeadSelectionBar from "@/components/app/leads/LeadSelectionBar";
import LeadsTable from "@/components/app/leads/LeadsTable";
import SavedSearches from "@/components/app/leads/SavedSearches";
import SearchPanel from "@/components/app/leads/SearchPanel";
import { areaKm2, boundsOf, inPolygon } from "@/components/app/leads/geo";
import { useConfirm } from "@/hooks/context/confirm";
import type { AppError } from "@/lib/api/client/normalizeError";
import {
    useDeleteLeads,
    useEnrichLeads,
    useLeadEnrichments,
    runActive,
    useLeadFacets,
    useLeadFinderConfig,
    useLeadPoints,
    useLeadSearches,
    useLeadsList,
    useSearchLeads,
    useUpdateLeads,
} from "@/lib/api/hooks/app/leads/useLeads";
import type { GeocodeResult, LatLng, LeadArea, LeadBounds, LeadFinderConfig, LeadProvider, LeadSearch, LeadSelection, LeadSort, LeadStatus } from "@/lib/api/models/app/leads/Lead";
import buildError from "@/lib/helper/buildError";
import { cn } from "@/lib/utils";
import { useAppStore } from "@/stores";
import { fmtUsdCost } from "@/lib/money";

type ViewMode = "split" | "map" | "list";
type StyleChoice = "auto" | "light" | "dark" | "streets";

interface Prefs {
    mode: ViewMode;
    colorBy: ColorBy;
    layer: MapLayerMode;
    style: StyleChoice;
    tilt: boolean;
    view: { center: LatLng; zoom: number };
    // The source last picked in the dropdown; a saved search opening its own does not change it.
    provider?: LeadProvider;
    // Find people and emails may search the web; on unless turned off.
    webSearch?: boolean;
}

const PREFS_KEY = "wb.leadFinder.prefs";
const DEFAULT_PREFS: Prefs = {
    mode: "split",
    colorBy: "reach",
    layer: "pins",
    style: "auto",
    tilt: false,
    view: { center: { lat: 39.5, lng: -98.35 }, zoom: 3.4 },
};
const OSM_MAX_KM2 = 2500;

// The bounds a map the size of v shows around center at zoom.
function boundsAt(v: MapView, center: LatLng, zoom: number): LeadBounds {
    const k = 2 ** (v.zoom - zoom);
    const cos = (lat: number) => Math.cos((lat * Math.PI) / 180);
    const halfLng = ((v.bounds.east - v.bounds.west) / 2) * k;
    const halfLat = ((v.bounds.north - v.bounds.south) / 2) * k * (cos(center.lat) / cos(v.center.lat));
    return { south: center.lat - halfLat, north: center.lat + halfLat, west: center.lng - halfLng, east: center.lng + halfLng };
}

function loadPrefs(): Prefs {
    try {
        const raw = localStorage.getItem(PREFS_KEY);
        return raw ? { ...DEFAULT_PREFS, ...(JSON.parse(raw) as Partial<Prefs>) } : DEFAULT_PREFS;
    } catch {
        return DEFAULT_PREFS;
    }
}

export default function LeadFinderPage() {
    const config = useLeadFinderConfig();
    if (!config.data) {
        return (
            <div className="flex h-full items-center justify-center bg-white">
                {config.isError ? (
                    <p className="text-[12.5px] text-slate-500">{buildError(config.error as unknown as AppError)}</p>
                ) : (
                    <Loader2Icon className="size-5 animate-spin text-slate-300" />
                )}
            </div>
        );
    }
    return <Finder config={config.data} />;
}

function Finder({ config }: { config: LeadFinderConfig }) {
    const resolvedTheme = useAppStore((s) => s.resolvedTheme);
    const confirm = useConfirm();
    const [prefs, setPrefs] = useState<Prefs>(loadPrefs);
    const patchPrefs = useCallback((p: Partial<Prefs>) => setPrefs((cur) => ({ ...cur, ...p })), []);
    useEffect(() => {
        const t = window.setTimeout(() => localStorage.setItem(PREFS_KEY, JSON.stringify(prefs)), 400);
        return () => window.clearTimeout(t);
    }, [prefs]);

    // Search form.
    const [query, setQuery] = useState("");
    const [category, setCategory] = useState("");
    const [location, setLocation] = useState("");
    const [provider, setProvider] = useState<LeadProvider>(() =>
        prefs.provider && config.providers.some((p) => p.id === prefs.provider && p.available) ? prefs.provider : config.default_provider,
    );
    const pickProvider = (p: LeadProvider) => {
        setProvider(p);
        patchPrefs({ provider: p });
    };
    const [area, setArea] = useState<LeadArea | null>(null);
    // The last search's outline; drawn on the map, never searched again on its own.
    const [searched, setSearched] = useState<LeadArea | null>(null);
    const [tool, setTool] = useState<MapTool>("pan");
    const [view, setView] = useState<MapView | null>(null);
    const [fly, setFly] = useState<FlyTarget | null>(null);
    const flyTo = (t: Omit<FlyTarget, "key">) => setFly({ ...t, key: Date.now() });
    const controls = useRef<MapControls | null>(null);

    // What the list and the map show.
    const [searchId, setSearchId] = useState<string | null>(null);
    const [refine, setRefine] = useState<Refine>(EMPTY_REFINE);
    const [text, setText] = useState("");
    const [sort, setSort] = useState<LeadSort>("score");
    const [sideOpen, setSideOpen] = useState(false);

    // Selection and detail.
    const [selected, setSelected] = useState<Set<string>>(new Set());
    const [allMatching, setAllMatching] = useState(false);
    const [activeId, setActiveId] = useState<string | null>(null);
    const [hoverId, setHoverId] = useState<string | null>(null);
    const [dialog, setDialog] = useState<{ kind: "contacts" | "export"; selection: LeadSelection; count: number } | null>(null);
    const lastClicked = useRef<string | null>(null);

    const searches = useLeadSearches();
    const currentSearch = searches.data?.find((s) => s.id === searchId) ?? null;

    const viewBounds = view?.bounds;
    const roundedBounds = useMemo(() => {
        if (!refine.inView || !viewBounds) return undefined;
        // Rounded outward, so a deep zoom never collapses the box to zero width.
        const down = (n: number) => Math.floor(n * 1000) / 1000;
        const up = (n: number) => Math.ceil(n * 1000) / 1000;
        return { south: down(viewBounds.south), west: down(viewBounds.west), north: up(viewBounds.north), east: up(viewBounds.east) };
    }, [refine.inView, viewBounds]);

    const filter = useMemo(() => {
        const f = refineFilter(refine);
        if (searchId) f.search_id = searchId;
        if (text.trim()) f.q = text.trim();
        if (roundedBounds) f.bounds = roundedBounds;
        return f;
    }, [refine, searchId, text, roundedBounds]);

    const runs = useLeadEnrichments();
    const runList = useMemo(() => runs.data ?? [], [runs.data]);
    const list = useLeadsList(filter, sort, runList.some(runActive));
    const points = useLeadPoints(filter);
    const facets = useLeadFacets(searchId ? { search_id: searchId } : {});
    const leads = useMemo(() => list.data?.pages.flatMap((p) => p.data) ?? [], [list.data]);
    const total = list.data?.pages[0]?.pagination.total ?? 0;

    // A new filter starts a new selection.
    useEffect(() => {
        setSelected(new Set());
        setAllMatching(false);
    }, [filter]);

    const search = useSearchLeads();
    const enrich = useEnrichLeads();
    const updateMany = useUpdateLeads();
    const remove = useDeleteLeads();

    const providerInfo = config.providers.find((p) => p.id === provider);
    const lookingFor = query.trim() || config.categories.find((c) => c.id === category)?.label || "businesses";
    const searchingLabel = search.isPending ? `Searching ${providerInfo?.name ?? "the map"} for ${lookingFor.toLowerCase()}…` : null;
    const shownArea = area ?? searched;
    const searchArea: LeadArea | null = area ?? (view ? { kind: "bbox", bounds: view.bounds } : null);
    const blockedReason = !query.trim() && !category
        ? "Pick a category or type what to look for."
        : !providerInfo?.available
          ? `Connect ${providerInfo?.name ?? "this source"} under Integrations to search with it.`
          : searchArea && provider === "osm" && areaKm2(searchArea.bounds) > OSM_MAX_KM2
            ? "Zoom in or draw a smaller area. OpenStreetMap searches up to 2,500 km² at a time."
            : !searchArea
              ? "Move the map to where you want to search."
              : null;

    const runSearch = async () => {
        if (blockedReason || !searchArea || search.isPending) return;
        try {
            const res = await search.mutateAsync({
                provider,
                query: query.trim(),
                category,
                location_label: location.trim(),
                area: searchArea,
                language: navigator.language.slice(0, 2),
            });
            setSearchId(res.search.id);
            setSearched(res.search.area);
            setSideOpen(false);
            if (res.leads.length) {
                flyTo({ bounds: boundsOf(res.leads.map((l) => ({ lat: l.latitude, lng: l.longitude }))) });
            }
            const charged = res.charged_usd > 0 ? ` · ${fmtUsdCost(res.charged_usd)}` : "";
            if (res.found === 0) toast(`Nothing found in this area${res.outside_area ? `; ${res.outside_area} results fell just outside it` : ""}`);
            else toast.success(`Found ${res.found.toLocaleString()} business${res.found === 1 ? "" : "es"}${charged}`);
        } catch (e) {
            toast.error(buildError(e as AppError));
        }
    };

    const loadMore = async () => {
        if (!currentSearch) return;
        try {
            const res = await search.mutateAsync({ search_id: currentSearch.id, language: navigator.language.slice(0, 2) });
            toast.success(res.found ? `${res.found.toLocaleString()} more found` : "No more results in this area");
        } catch (e) {
            toast.error(buildError(e as AppError));
        }
    };

    const openSearch = (s: LeadSearch) => {
        setSearchId(s.id);
        setArea(s.area);
        setSearched(null);
        setQuery(s.query);
        setCategory(s.category);
        setLocation(s.location_label);
        if (config.providers.find((p) => p.id === s.provider)?.available) setProvider(s.provider);
        flyTo({ bounds: s.area.bounds });
        setSideOpen(false);
    };

    const clearSearch = () => {
        setSearchId(null);
        setArea(null);
        setSearched(null);
    };

    // Until the flight lands, a search covers where the map is going, not where it was.
    const onPlace = (r: GeocodeResult) => {
        setArea(null);
        const bounds = r.bounds;
        if (bounds) {
            setView((v) => ({ center: r.center, zoom: v?.zoom ?? 14, bounds }));
            flyTo({ bounds });
        } else {
            setView((v) => v && { center: r.center, zoom: 14, bounds: boundsAt(v, r.center, 14) });
            flyTo({ center: r.center, zoom: 14 });
        }
    };

    // Selection.
    const selection: LeadSelection = allMatching ? { all: true, filter } : { lead_ids: [...selected] };
    const selectedCount = allMatching ? Math.min(total, 5000) : selected.size;
    const toggle = (id: string, range: boolean) => {
        setAllMatching(false);
        setSelected((cur) => {
            const next = new Set(cur);
            if (range && lastClicked.current) {
                const a = leads.findIndex((l) => l.id === lastClicked.current);
                const b = leads.findIndex((l) => l.id === id);
                if (a >= 0 && b >= 0) {
                    for (const l of leads.slice(Math.min(a, b), Math.max(a, b) + 1)) next.add(l.id);
                    return next;
                }
            }
            if (next.has(id)) next.delete(id);
            else next.add(id);
            return next;
        });
        lastClicked.current = id;
    };
    const clearSelection = () => {
        setSelected(new Set());
        setAllMatching(false);
    };
    const onLasso = (poly: LatLng[]) => {
        const inside = (points.data?.data ?? []).filter((p) => inPolygon(poly, { lat: p.latitude, lng: p.longitude })).map((p) => p.id);
        setAllMatching(false);
        setSelected((cur) => new Set([...cur, ...inside]));
        toast(inside.length ? `Selected ${inside.length} lead${inside.length === 1 ? "" : "s"} in the shape` : "No leads inside that shape");
    };

    const webSearch = prefs.webSearch !== false;
    const webSearchChoice = config.web_search.available
        ? { on: webSearch, costUsd: config.web_search.cost_usd, onChange: (on: boolean) => patchPrefs({ webSearch: on }) }
        : undefined;
    const startEnrich = async (sel: LeadSelection) => {
        try {
            const run = await enrich.mutateAsync({ ...sel, verify: true, people: true, web_search: webSearch });
            toast.success(`Reading ${run.total.toLocaleString()} website${run.total === 1 ? "" : "s"} for people and contact details`);
        } catch (e) {
            toast.error(buildError(e as AppError));
        }
    };
    const setStatus = async (status: LeadStatus) => {
        try {
            const res = await updateMany.mutateAsync({ ...selection, status });
            toast.success(`${res.updated.toLocaleString()} updated`);
            clearSelection();
        } catch (e) {
            toast.error(buildError(e as AppError));
        }
    };
    const addTag = async (tag: string) => {
        try {
            await updateMany.mutateAsync({ ...selection, add_tags: [tag] });
            toast.success(`Tagged ${selectedCount.toLocaleString()} with ${tag}`);
        } catch (e) {
            toast.error(buildError(e as AppError));
        }
    };
    const deleteSelected = () =>
        confirm?.show(
            `Delete ${selectedCount.toLocaleString()} lead${selectedCount === 1 ? "" : "s"}? Contacts already made from them stay.`,
            async () => {
                try {
                    await remove.mutateAsync(selection);
                    clearSelection();
                    setActiveId(null);
                } catch (e) {
                    toast.error(buildError(e as AppError));
                }
            },
        );

    const pickPoint = (id: string, additive: boolean) => {
        if (additive) return toggle(id, false);
        setActiveId(id);
    };

    const styleUrl = useMemo(() => {
        const dark = prefs.style === "dark" || (prefs.style === "auto" && resolvedTheme === "dark");
        if (prefs.style === "streets" && config.map.style_light.includes("tiles.openfreemap.org/styles/positron")) {
            return config.map.style_light.replace("/positron", "/liberty");
        }
        return dark ? config.map.style_dark : config.map.style_light;
    }, [prefs.style, resolvedTheme, config.map]);
    const darkMap = prefs.style === "dark" || (prefs.style === "auto" && resolvedTheme === "dark");
    const highlight = useMemo(() => {
        const s = new Set(selected);
        if (hoverId) s.add(hoverId);
        return s;
    }, [selected, hoverId]);

    const showMap = prefs.mode !== "list";
    const showList = prefs.mode !== "map";

    // One column, no switching: the search form, then every past search under it.
    const sidebar = (
        <div className="flex h-full flex-col">
            <div className="md:hidden shrink-0 h-11 px-3 flex items-center border-b border-slate-200">
                <span className="text-[13px] font-semibold text-slate-900">Find businesses</span>
                <button
                    type="button"
                    aria-label="Close panel"
                    onClick={() => setSideOpen(false)}
                    className="ml-auto size-7 rounded-md text-slate-500 hover:bg-slate-100 inline-flex items-center justify-center"
                >
                    <XIcon className="size-3.5" />
                </button>
            </div>
            <div className="flex-1 min-h-0 overflow-y-auto">
                <motion.section
                    initial={{ opacity: 0, y: 6 }}
                    animate={{ opacity: 1, y: 0 }}
                    transition={{ duration: 0.24, ease: [0.22, 1, 0.36, 1] }}
                    className="px-3 pt-3 pb-4"
                >
                        <SearchPanel
                            config={config}
                            query={query}
                            onQuery={setQuery}
                            category={category}
                            onCategory={setCategory}
                            location={location}
                            onLocation={setLocation}
                            onPlace={onPlace}
                            near={view && view.zoom >= 9 ? view.center : null}
                            provider={provider}
                            onProvider={pickProvider}
                            area={area}
                            onClearArea={() => {
                                setArea(null);
                                setSearched(null);
                            }}
                            onTool={(t) => {
                                setTool(t);
                                setSideOpen(false);
                            }}
                            searching={search.isPending}
                            onSearch={runSearch}
                            blockedReason={blockedReason}
                        />
                </motion.section>
                <motion.section
                    initial={{ opacity: 0, y: 6 }}
                    animate={{ opacity: 1, y: 0 }}
                    transition={{ duration: 0.24, delay: 0.06, ease: [0.22, 1, 0.36, 1] }}
                    className="border-t border-slate-100 px-3 pt-3 pb-4"
                >
                    <SavedSearches
                        config={config}
                        activeId={searchId}
                        onOpen={(s) => {
                            openSearch(s);
                            setSideOpen(false);
                        }}
                        onDelete={(id) => id === searchId && clearSearch()}
                    />
                </motion.section>
            </div>
        </div>
    );

    return (
        <div className="relative flex h-full flex-col bg-white">
            <header className="shrink-0 h-12 px-3 md:px-4 flex items-center gap-2 border-b border-slate-200">
                <button
                    type="button"
                    aria-label="Find businesses"
                    onClick={() => setSideOpen(true)}
                    className="md:hidden size-7 rounded-md text-slate-600 hover:bg-slate-100 inline-flex items-center justify-center"
                >
                    <PanelLeftIcon className="size-4" />
                </button>
                <MapPinnedIcon className="hidden md:block size-4 text-sky-600" />
                <h1 className="text-[13.5px] font-semibold text-slate-900">Lead finder</h1>
                <div className="h-4 w-px bg-slate-200" />
                {currentSearch ? (
                    <span className="min-w-0 inline-flex h-6 items-center gap-1 rounded-md bg-sky-50 pl-2 pr-1 text-[12px] text-sky-800">
                        <span className="truncate max-w-[220px]">{currentSearch.name}</span>
                        <button
                            type="button"
                            aria-label="Show every lead"
                            title="Show every lead"
                            onClick={clearSearch}
                            className="size-4 rounded hover:bg-sky-100 inline-flex items-center justify-center"
                        >
                            <XIcon className="size-3" />
                        </button>
                    </span>
                ) : (
                    <span className="text-[12px] text-slate-500">All leads</span>
                )}
                {currentSearch?.has_more && (
                    <button
                        type="button"
                        onClick={loadMore}
                        disabled={search.isPending}
                        className="hidden sm:inline-flex h-6 items-center gap-1 rounded-md border border-slate-200 px-2 text-[11.5px] text-slate-600 hover:text-slate-900 disabled:opacity-50"
                    >
                        {search.isPending ? <Loader2Icon className="size-3 animate-spin" /> : <PlusIcon className="size-3" />}
                        Load more
                    </button>
                )}
                <div className="ml-auto inline-flex rounded-md border border-slate-200 p-0.5">
                    {(
                        [
                            ["map", "Map", MapIcon],
                            ["split", "Split", Columns2Icon],
                            ["list", "List", ListIcon],
                        ] as const
                    ).map(([key, label, Icon]) => (
                        <button
                            key={key}
                            type="button"
                            aria-label={label}
                            aria-pressed={prefs.mode === key}
                            onClick={() => patchPrefs({ mode: key })}
                            className={cn("h-6 px-2 rounded inline-flex items-center gap-1 text-[11.5px]", prefs.mode === key ? "bg-slate-100 text-slate-900 font-medium" : "text-slate-500 hover:text-slate-900")}
                        >
                            <Icon className="size-3.5" />
                            <span className="hidden lg:inline">{label}</span>
                        </button>
                    ))}
                </div>
            </header>

            <div className="relative flex flex-1 min-h-0">
                <aside className="hidden md:flex w-[316px] shrink-0 flex-col border-r border-slate-200 bg-white">{sidebar}</aside>
                {sideOpen && <div className="md:hidden absolute inset-0 z-30 bg-white">{sidebar}</div>}

                <div className="relative flex min-w-0 flex-1 flex-col">
                    <LeadFilterBar text={text} onText={setText} value={refine} onChange={setRefine} facets={facets.data} />
                    {!showList && <EnrichmentProgress runs={runList} searching={searchingLabel} />}
                    {showMap && (
                        <div className={cn("relative min-h-0", showList ? "h-[48%] lg:h-[56%] border-b border-slate-200" : "flex-1")}>
                            <LeadMap
                                styleUrl={styleUrl}
                                dark={darkMap}
                                themed={prefs.style !== "streets"}
                                controls={controls}
                                points={points.data?.data ?? []}
                                selected={highlight}
                                activeId={activeId}
                                colorBy={prefs.colorBy}
                                layer={prefs.layer}
                                area={shownArea}
                                tool={tool}
                                tilt={prefs.tilt}
                                initialView={prefs.view}
                                fly={fly}
                                onToolDone={() => setTool("pan")}
                                onArea={setArea}
                                onLasso={onLasso}
                                onPick={pickPoint}
                                onView={(v) => {
                                    setView(v);
                                    patchPrefs({ view: { center: v.center, zoom: v.zoom } });
                                }}
                            />
                            <MapToolbar tool={tool} onTool={setTool} />
                            <MapControlsBar
                                prefs={prefs}
                                onPrefs={patchPrefs}
                                onZoom={(d) => controls.current?.zoomBy(d)}
                                onFit={() => {
                                    const pts = points.data?.data ?? [];
                                    if (pts.length) flyTo({ bounds: boundsOf(pts.map((p) => ({ lat: p.latitude, lng: p.longitude }))) });
                                    else if (shownArea) flyTo({ bounds: shownArea.bounds });
                                }}
                            />
                            {tool !== "pan" && <ToolHint tool={tool} onCancel={() => setTool("pan")} />}
                            {tool === "pan" && !blockedReason && (
                                <div className="absolute left-1/2 top-3 z-10 -translate-x-1/2">
                                    <button
                                        type="button"
                                        onClick={runSearch}
                                        disabled={search.isPending}
                                        className="h-8 px-3.5 rounded-full bg-white/95 border border-slate-200 text-[12px] font-medium text-slate-800 shadow-[0_6px_18px_-8px_rgba(15,23,42,0.35)] backdrop-blur inline-flex items-center gap-1.5 hover:text-sky-700 disabled:opacity-60"
                                    >
                                        {search.isPending ? <Loader2Icon className="size-3.5 animate-spin text-sky-600" /> : <RotateCw className="size-3.5 text-sky-600" />}
                                        {area ? "Search this area" : "Search this view"}
                                    </button>
                                </div>
                            )}
                            <Legend colorBy={prefs.colorBy} layer={prefs.layer} count={points.data?.data.length ?? 0} truncated={!!points.data?.truncated} />
                            {points.isFetching && (
                                <div className="absolute left-3 bottom-12 z-10 inline-flex items-center gap-1.5 rounded-full bg-white/90 px-2 py-1 text-[11px] text-slate-500 shadow-sm">
                                    <Loader2Icon className="size-3 animate-spin" />
                                    Updating pins
                                </div>
                            )}
                            {!searchId && (points.data?.data.length ?? 0) === 0 && !points.isPending && <EmptyHint />}
                        </div>
                    )}
                    {showList && (
                        <div className="relative min-h-0 flex-1">
                            <LeadsTable
                                leads={leads}
                                total={total}
                                loading={list.isPending || (list.isError && list.isFetching && !leads.length)}
                                error={list.isError && !leads.length ? buildError(list.error as unknown as AppError) : null}
                                onRetry={() => list.refetch()}
                                hasMore={!!list.hasNextPage}
                                loadingMore={list.isFetchingNextPage}
                                onMore={() => list.fetchNextPage()}
                                sort={sort}
                                onSort={setSort}
                                selected={selected}
                                allMatching={allMatching}
                                onToggle={toggle}
                                onTogglePage={(on) => {
                                    setAllMatching(false);
                                    setSelected(on ? new Set(leads.map((l) => l.id)) : new Set());
                                }}
                                onSelectAllMatching={() => setAllMatching(true)}
                                activeId={activeId}
                                onOpen={(id) => {
                                    setActiveId(id);
                                    const l = leads.find((x) => x.id === id);
                                    if (l && showMap) flyTo({ center: { lat: l.latitude, lng: l.longitude }, zoom: 15 });
                                }}
                                onHover={setHoverId}
                                banner={<EnrichmentProgress runs={runList} searching={searchingLabel} />}
                            />
                        </div>
                    )}
                    <LeadDrawer
                        id={activeId}
                        config={config}
                        webSearch={webSearchChoice}
                        onClose={() => setActiveId(null)}
                        onAddToContacts={(id) => setDialog({ kind: "contacts", selection: { lead_ids: [id] }, count: 1 })}
                    />
                </div>
            </div>

            <LeadSelectionBar
                count={selectedCount}
                enrichCost={config.enrich_cost_usd}
                enriching={enrich.isPending}
                webSearch={webSearchChoice}
                onEnrich={() => startEnrich(selection)}
                onAddToContacts={() => setDialog({ kind: "contacts", selection, count: selectedCount })}
                onExport={() => setDialog({ kind: "export", selection, count: selectedCount })}
                onStatus={setStatus}
                onTag={addTag}
                onDelete={deleteSelected}
                onClear={clearSelection}
            />
            <AddToContactsDialog
                open={dialog?.kind === "contacts"}
                onClose={() => setDialog(null)}
                selection={dialog?.selection ?? {}}
                count={dialog?.count ?? 0}
                suggestedSegment={currentSearch?.name}
            />
            <ExportDialog open={dialog?.kind === "export"} onClose={() => setDialog(null)} selection={dialog?.selection ?? {}} count={dialog?.count ?? 0} config={config} />
        </div>
    );
}

const TOOLS: { id: MapTool; label: string; icon: React.ReactNode }[] = [
    { id: "pan", label: "Move the map", icon: <HandIcon className="size-3.5" /> },
    { id: "rect", label: "Draw a rectangle to search", icon: <SquareIcon className="size-3.5" /> },
    { id: "circle", label: "Draw a circle to search", icon: <CircleIcon className="size-3.5" /> },
    { id: "polygon", label: "Draw a shape to search", icon: <PentagonIcon className="size-3.5" /> },
    { id: "lasso", label: "Lasso leads to select them", icon: <LassoIcon className="size-3.5" /> },
];

function MapToolbar({ tool, onTool }: { tool: MapTool; onTool: (t: MapTool) => void }) {
    return (
        <div className="absolute left-3 top-3 z-10 flex flex-col gap-0.5 rounded-lg border border-slate-200 bg-white/95 p-0.5 shadow-[0_6px_18px_-8px_rgba(15,23,42,0.3)] backdrop-blur">
            {TOOLS.map((t) => (
                <button
                    key={t.id}
                    type="button"
                    title={t.label}
                    aria-label={t.label}
                    aria-pressed={tool === t.id}
                    onClick={() => onTool(t.id)}
                    className={cn("size-7 rounded-md inline-flex items-center justify-center transition-colors", tool === t.id ? "bg-sky-600 text-white" : "text-slate-600 hover:bg-slate-100 hover:text-slate-900")}
                >
                    {t.icon}
                </button>
            ))}
        </div>
    );
}

const HINTS: Record<Exclude<MapTool, "pan">, string> = {
    rect: "Drag to draw a rectangle",
    circle: "Drag from the center outwards",
    polygon: "Click to add corners; double-click or Enter to finish",
    lasso: "Drag around the leads you want to select",
};

function ToolHint({ tool, onCancel }: { tool: Exclude<MapTool, "pan">; onCancel: () => void }) {
    return (
        <div className="absolute left-1/2 top-3 z-10 -translate-x-1/2 inline-flex items-center gap-2 rounded-full bg-slate-900/90 py-1 pl-3 pr-1 text-[12px] text-white shadow-lg">
            {HINTS[tool]}
            <button type="button" onClick={onCancel} className="h-6 rounded-full bg-white/15 px-2 text-[11.5px] hover:bg-white/25">
                Esc
            </button>
        </div>
    );
}

function MapControlsBar({ prefs, onPrefs, onZoom, onFit }: { prefs: Prefs; onPrefs: (p: Partial<Prefs>) => void; onZoom: (d: number) => void; onFit: () => void }) {
    const btn = "size-7 rounded-md inline-flex items-center justify-center text-slate-600 hover:bg-slate-100 hover:text-slate-900";
    const check = (on: boolean) => (on ? <CheckIcon className="size-3.5 text-sky-600" /> : undefined);
    return (
        <div className="absolute right-3 top-3 z-10 flex flex-col gap-1.5">
            <div className="flex flex-col gap-0.5 rounded-lg border border-slate-200 bg-white/95 p-0.5 shadow-[0_6px_18px_-8px_rgba(15,23,42,0.3)] backdrop-blur">
                <PopoverMenu align="end">
                    <PopoverMenuTrigger asChild>
                        <button type="button" title="Map layers" aria-label="Map layers" className={btn}>
                            <LayersIcon className="size-3.5" />
                        </button>
                    </PopoverMenuTrigger>
                    <PopoverMenuContent minWidth={220}>
                        <PopoverMenuLabel>Show leads as</PopoverMenuLabel>
                        <PopoverMenuItem onSelect={() => onPrefs({ layer: "pins" })} trailing={check(prefs.layer === "pins")}>
                            Pins
                        </PopoverMenuItem>
                        <PopoverMenuItem onSelect={() => onPrefs({ layer: "heat" })} trailing={check(prefs.layer === "heat")}>
                            Heatmap
                        </PopoverMenuItem>
                        <PopoverMenuSeparator />
                        <PopoverMenuLabel>Color pins by</PopoverMenuLabel>
                        {(
                            [
                                ["reach", "Reachability"],
                                ["status", "Status"],
                                ["score", "Score"],
                                ["rating", "Rating"],
                            ] as const
                        ).map(([id, label]) => (
                            <PopoverMenuItem key={id} onSelect={() => onPrefs({ colorBy: id })} trailing={check(prefs.colorBy === id)}>
                                {label}
                            </PopoverMenuItem>
                        ))}
                        <PopoverMenuSeparator />
                        <PopoverMenuLabel>Map style</PopoverMenuLabel>
                        {(
                            [
                                ["auto", "Match the dashboard"],
                                ["light", "Light"],
                                ["dark", "Dark"],
                                ["streets", "Streets"],
                            ] as const
                        ).map(([id, label]) => (
                            <PopoverMenuItem key={id} onSelect={() => onPrefs({ style: id })} trailing={check(prefs.style === id)}>
                                {label}
                            </PopoverMenuItem>
                        ))}
                    </PopoverMenuContent>
                </PopoverMenu>
                <button type="button" title={prefs.tilt ? "Flat view" : "Tilted 3D view"} aria-label="Toggle tilted view" aria-pressed={prefs.tilt} onClick={() => onPrefs({ tilt: !prefs.tilt })} className={cn(btn, prefs.tilt && "text-sky-700 bg-sky-50")}>
                    <BoxIcon className="size-3.5" />
                </button>
                <button type="button" title="Fit to the leads" aria-label="Fit to the leads" onClick={onFit} className={btn}>
                    <FocusIcon className="size-3.5" />
                </button>
            </div>
            <div className="flex flex-col gap-0.5 rounded-lg border border-slate-200 bg-white/95 p-0.5 shadow-[0_6px_18px_-8px_rgba(15,23,42,0.3)] backdrop-blur">
                <button type="button" aria-label="Zoom in" onClick={() => onZoom(1)} className={btn}>
                    <PlusIcon className="size-3.5" />
                </button>
                <button type="button" aria-label="Zoom out" onClick={() => onZoom(-1)} className={btn}>
                    <MinusIcon className="size-3.5" />
                </button>
            </div>
        </div>
    );
}

function Legend({ colorBy, layer, count, truncated }: { colorBy: ColorBy; layer: MapLayerMode; count: number; truncated: boolean }) {
    return (
        <div className="absolute left-3 bottom-3 z-10 flex flex-wrap items-center gap-x-2.5 gap-y-1 rounded-lg border border-slate-200 bg-white/95 px-2.5 py-1.5 text-[11px] text-slate-600 shadow-sm backdrop-blur max-w-[calc(100%-300px)] min-w-[180px]">
            <span className="font-medium tabular-nums text-slate-900">
                {count.toLocaleString()}
                {truncated ? "+" : ""} on map
            </span>
            {layer === "pins" &&
                LEGEND[colorBy].map((l) => (
                    <span key={l.label} className="inline-flex items-center gap-1">
                        <span className="size-2 rounded-full" style={{ background: l.color }} />
                        {l.label}
                    </span>
                ))}
            {layer === "heat" && <span>Brighter where leads are denser and better</span>}
        </div>
    );
}

function EmptyHint() {
    return (
        <div className="pointer-events-none absolute inset-0 z-[5] flex items-center justify-center p-6">
            <div className="max-w-[340px] rounded-xl border border-slate-200 bg-white/95 px-5 py-4 text-center shadow-[0_16px_40px_-20px_rgba(15,23,42,0.4)] backdrop-blur">
                <MapPinnedIcon className="mx-auto size-6 text-sky-600" />
                <p className="mt-2 text-[13px] font-semibold text-slate-900">Find businesses on the map</p>
                <p className="mt-1 text-[12px] leading-relaxed text-slate-500">
                    Pick a category, go to a city, then search the view or draw the exact area. Find people and emails reads each business&apos;s own website for who runs it and how to reach them.
                </p>
            </div>
        </div>
    );
}
