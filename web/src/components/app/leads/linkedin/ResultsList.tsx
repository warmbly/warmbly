// One continuous virtual list for every view, loading the next page as its end comes into view, and the note that ends it.
import { Link } from "@tanstack/react-router";
import { useVirtualizer } from "@tanstack/react-virtual";
import { AlertTriangleIcon, CheckCircle2Icon, GaugeIcon, InfoIcon, RotateCwIcon } from "lucide-react";
import { useEffect, useLayoutEffect, useRef, useState, type ReactNode, type RefObject } from "react";
import { socialErrorMessage } from "@/lib/social/errors";
import { cn } from "@/lib/utils";
import { ResultSkeletons } from "./Results";
import type { ListTail } from "./state";
import type { ListItem, ResultView } from "./view";

const fmt = (n: number) => n.toLocaleString("en-US");

function estimateItem(item: ListItem, view: ResultView, isAccounts: boolean): number {
    if (item.kind === "group") return 53;
    if (isAccounts) return 84;
    return view === "cards" ? 132 : 53;
}

export function ResultsList({
    items,
    stale,
    view,
    isAccounts,
    scrollEl,
    scrollToRef,
    render,
}: {
    items: ListItem[];
    stale: boolean;
    view: ResultView;
    isAccounts: boolean;
    scrollEl: HTMLElement | null;
    scrollToRef: RefObject<((index: number) => void) | null>;
    render: (item: ListItem) => ReactNode;
}) {
    const listRef = useRef<HTMLUListElement>(null);
    const [margin, setMargin] = useState(0);
    // The list's distance from the top of the scroller; what sits above it changes height.
    useLayoutEffect(() => {
        const list = listRef.current;
        if (!list || !scrollEl) return;
        const measure = () => setMargin(Math.round(list.getBoundingClientRect().top - scrollEl.getBoundingClientRect().top + scrollEl.scrollTop));
        measure();
        const ro = new ResizeObserver(measure);
        for (const child of Array.from(scrollEl.children)) ro.observe(child);
        return () => ro.disconnect();
    }, [scrollEl]);
    const virtualizer = useVirtualizer({
        count: items.length,
        getScrollElement: () => scrollEl,
        estimateSize: (i) => (items[i] ? estimateItem(items[i], view, isAccounts) : 53),
        getItemKey: (i) => items[i]?.key ?? i,
        overscan: 6,
        scrollMargin: margin,
    });
    useEffect(() => {
        scrollToRef.current = (index: number) => virtualizer.scrollToIndex(index, { align: "auto" });
    });
    return (
        <ul ref={listRef} className={cn("relative transition-opacity", stale && "opacity-60")} style={{ height: virtualizer.getTotalSize() }} aria-busy={stale} aria-label="Search results">
            {virtualizer.getVirtualItems().map((v) => {
                const item = items[v.index];
                if (!item) return null;
                return (
                    <li key={v.key} data-index={v.index} ref={virtualizer.measureElement} className="absolute inset-x-0 top-0" style={{ transform: `translateY(${v.start - virtualizer.options.scrollMargin}px)` }}>
                        {render(item)}
                    </li>
                );
            })}
        </ul>
    );
}

export function ListEnd({
    tail,
    pageSize,
    paused,
    scrollEl,
    onLoadMore,
    accountName,
}: {
    tail: ListTail;
    pageSize: number;
    /** No automatic loading while the search is being edited or "Select first" is loading. */
    paused: boolean;
    scrollEl: HTMLElement | null;
    onLoadMore: () => void;
    accountName: string;
}) {
    const sentinel = useRef<HTMLDivElement>(null);
    const auto = tail.kind === "more" && !paused;
    // Loads the next page once the end is within 600px; recreated after every page so a short list keeps filling.
    useEffect(() => {
        const el = sentinel.current;
        if (!auto || !el || !scrollEl || typeof IntersectionObserver === "undefined") return;
        const io = new IntersectionObserver((entries) => entries.some((e) => e.isIntersecting) && onLoadMore(), { root: scrollEl, rootMargin: "0px 0px 600px 0px" });
        io.observe(el);
        return () => io.disconnect();
    }, [auto, scrollEl, onLoadMore]);

    return (
        <div ref={sentinel}>
            {tail.kind === "more" && (
                <div className="flex flex-col items-center gap-1.5 px-4 py-6">
                    <button type="button" onClick={onLoadMore} className="h-7 px-3 rounded-md border border-slate-200 bg-white text-[12px] font-medium text-slate-700 hover:bg-slate-50">
                        Load {pageSize} more
                    </button>
                    <p className="text-[11.5px] text-slate-500">More results load as you scroll.</p>
                </div>
            )}
            {tail.kind === "loading" && (
                <div role="status" aria-live="polite">
                    <ResultSkeletons rows={3} />
                    <p className="px-4 py-3 text-center text-[11.5px] text-slate-500">Loading the next {pageSize}… Searches are paced a few seconds apart to keep the account safe.</p>
                </div>
            )}
            {tail.kind === "error" && (
                <TailNote icon={<AlertTriangleIcon className="text-red-500" />} tone="danger" title="The next results did not load" body={`${socialErrorMessage(tail.error)} The results above stay.`}>
                    <button type="button" onClick={onLoadMore} className="h-7 px-2.5 rounded-md border border-slate-200 bg-white text-[12px] font-medium text-slate-700 hover:bg-slate-50 inline-flex items-center gap-1.5">
                        <RotateCwIcon className="size-3.5" /> Retry
                    </button>
                </TailNote>
            )}
            {tail.kind === "budget" && (
                <TailNote
                    icon={<GaugeIcon />}
                    title="Today's search budget is used"
                    body={`${accountName || "This account"} has fetched ${fmt(tail.usage.used)} of its ${fmt(tail.usage.limit)} search results today. More load tomorrow, or raise the account's daily search limit and continue.`}
                >
                    <Link to="/app/settings/social" className="h-7 px-2.5 rounded-md border border-slate-200 bg-white text-[12px] font-medium text-slate-700 hover:bg-slate-50 inline-flex items-center">
                        Account limits
                    </Link>
                    <button type="button" onClick={onLoadMore} className="h-7 px-2.5 rounded-md text-[12px] text-slate-600 hover:text-slate-900 hover:bg-slate-100">
                        Continue
                    </button>
                </TailNote>
            )}
            {tail.kind === "cap" && (
                <TailNote
                    icon={<InfoIcon />}
                    title={`That is LinkedIn's limit: ${fmt(tail.cap)} results per search`}
                    body={`${tail.total != null && tail.total > tail.cap ? `This search has about ${fmt(tail.total)}. ` : ""}Narrow the filters (location, seniority, company size) to reach different people.`}
                />
            )}
            {tail.kind === "end" && tail.loaded > 0 && <TailNote icon={<CheckCircle2Icon className="text-emerald-500" />} title="That is everyone" body={`All ${fmt(tail.loaded)} results of this search are loaded.`} />}
        </div>
    );
}

function TailNote({ icon, title, body, tone, children }: { icon: ReactNode; title: string; body: string; tone?: "danger"; children?: ReactNode }) {
    return (
        <div className={cn("mx-4 my-4 flex flex-wrap items-start gap-3 rounded-md border px-4 py-3", tone === "danger" ? "border-red-200 bg-red-50/50" : "border-slate-200 bg-slate-50/60")} role={tone === "danger" ? "alert" : "status"}>
            <span className="mt-0.5 text-slate-400 [&>svg]:size-4">{icon}</span>
            <div className="grid min-w-0 flex-1 basis-56 gap-0.5">
                <p className="text-[12.5px] font-medium text-slate-900">{title}</p>
                <p className="text-[12px] text-slate-500">{body}</p>
            </div>
            {children && <div className="flex shrink-0 items-center gap-2">{children}</div>}
        </div>
    );
}
