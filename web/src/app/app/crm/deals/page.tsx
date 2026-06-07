// Deals — real kanban board.
//
// One pipeline at a time (selected via topbar). Columns are stages
// ordered by position. Cards can be dragged between stages — drop
// triggers a PATCH to /crm/deals/{id} with the new stage_id.
//
// "New deal" opens a slate-themed dialog scoped to the current
// pipeline. Cards click-open into the same dialog for editing.

import React from "react";
import {
    CircleDollarSignIcon,
    LayoutGridIcon,
    Loader2Icon,
    PlusIcon,
    SearchIcon,
    Table2Icon,
    TrashIcon,
    TrophyIcon,
    UserIcon,
    XIcon,
    CalendarIcon,
} from "lucide-react";
import toast from "react-hot-toast";
import { AnimatePresence, motion } from "framer-motion";
import {
    Page,
    PageBody,
    PageTopbar,
    SectionBar,
    Stat,
    StatStrip,
    TopbarAction,
} from "@/components/layout/Page";
import { Label, TextInput } from "@/components/ui/field";
import {
    PopoverMenu,
    PopoverMenuTrigger,
    PopoverMenuContent,
    PopoverMenuItem,
} from "@/components/ui/popover-menu";
import usePipelines from "@/lib/api/hooks/app/crm/pipelines/usePipelines";
import useSearchDeals from "@/lib/api/hooks/app/crm/deals/useSearchDeals";
import useDealsSummary from "@/lib/api/hooks/app/crm/deals/useDealsSummary";
import { EMPTY_DEAL_SEARCH } from "@/lib/api/models/app/crm/SearchDeals";
import type DealsSummary from "@/lib/api/models/app/crm/DealsSummary";
import useCreateDeal from "@/lib/api/hooks/app/crm/deals/useCreateDeal";
import useUpdateDeal from "@/lib/api/hooks/app/crm/deals/useUpdateDeal";
import useDeleteDeal from "@/lib/api/hooks/app/crm/deals/useDeleteDeal";
import { useConfirm } from "@/hooks/context/confirm";
import type Deal from "@/lib/api/models/app/crm/Deal";
import type { Stage } from "@/lib/api/models/app/crm/Pipeline";
import type { AppError } from "@/lib/api/client/normalizeError";
import buildError from "@/lib/helper/buildError";
import DealsTable from "@/components/app/crm/DealsTable";

const STATUS_LABEL = {
    open: { label: "Open",  tone: "text-slate-700",   dot: "bg-slate-400" },
    won:  { label: "Won",   tone: "text-emerald-700", dot: "bg-emerald-500" },
    lost: { label: "Lost",  tone: "text-red-700",     dot: "bg-red-500" },
} as const;

export default function DealsPage() {
    const pipelines = usePipelines();
    // Memoised so it's a stable dependency for the effect + memos below
    // (a fresh `?? []` each render would re-fire them every time).
    const list = React.useMemo(() => pipelines.data ?? [], [pipelines.data]);

    const [pipelineId, setPipelineId] = React.useState<string | undefined>(undefined);
    React.useEffect(() => {
        if (!pipelineId && list.length > 0) setPipelineId(list[0].id);
    }, [list, pipelineId]);

    const currentPipeline = list.find((p) => p.id === pipelineId);
    const stages = [...(currentPipeline?.stages ?? [])].sort((a, b) => a.position - b.position);
    const updateDeal = useUpdateDeal();

    const [search, setSearch] = React.useState("");
    const [newOpen, setNewOpen] = React.useState(false);
    const [editing, setEditing] = React.useState<Deal | null>(null);
    // Table = the cross-pipeline, server-driven "see everything" view (default).
    // Board = the focused single-pipeline kanban for moving deals through stages.
    const [view, setView] = React.useState<"table" | "board">("table");

    // When editing a deal opened from the cross-pipeline table, scope the stage
    // picker to that deal's OWN pipeline, not whichever pipeline the board has
    // selected — otherwise the picker shows the wrong pipeline's stages.
    const editingStages = React.useMemo(() => {
        if (!editing) return stages;
        const p = list.find((x) => x.id === editing.pipeline_id);
        return p ? [...(p.stages ?? [])].sort((a, b) => a.position - b.position) : stages;
    }, [editing, list, stages]);

    // The board reads totals + per-column counts/values from the server summary
    // (computed over the whole pipeline, not the loaded cards), and each column
    // pages its own deals — so it stays accurate and fast at any size.
    const boardFilters = React.useMemo(
        () => ({ ...EMPTY_DEAL_SEARCH, query: search, pipeline_ids: pipelineId ? [pipelineId] : [] }),
        [search, pipelineId],
    );
    const boardSummary = useDealsSummary(boardFilters, view === "board" && !!pipelineId).data;

    async function moveDeal(dealId: string, newStageId: string) {
        try {
            await toast.promise(
                updateDeal.mutateAsync({ id: dealId, data: { stage_id: newStageId } as Partial<Deal> }),
                { loading: "Moving…", success: "Moved", error: (e: AppError) => buildError(e) },
            );
        } catch {
            /* surfaced */
        }
    }

    return (
        <Page>
            <PageTopbar
                eyebrow="Deals"
                subtitle={
                    list.length === 0
                        ? "Create a pipeline first to start tracking deals."
                        : view === "board"
                          ? (currentPipeline?.name ?? "—")
                          : "Every deal, across all pipelines"
                }
            >
                {list.length > 0 && (
                    <>
                        <ViewToggle view={view} onChange={setView} />
                        {view === "board" && (
                            <>
                                <PipelinePicker
                                    pipelines={list}
                                    currentId={pipelineId}
                                    onChange={setPipelineId}
                                />
                                <SearchPill value={search} onChange={setSearch} />
                            </>
                        )}
                        <TopbarAction
                            icon={<PlusIcon className="w-3 h-3" />}
                            onClick={() => currentPipeline && setNewOpen(true)}
                        >
                            New deal
                        </TopbarAction>
                    </>
                )}
            </PageTopbar>

            {pipelines.isPending ? (
                <PageBody className="px-5 py-5">
                    <BoardSkeleton />
                </PageBody>
            ) : list.length === 0 ? (
                <PageBody className="px-5 py-5">
                    <NoPipelinesYet />
                </PageBody>
            ) : view === "table" ? (
                <DealsTable pipelines={list} onOpenDeal={(d) => setEditing(d)} />
            ) : (
                <>
                    <StatStrip cols={4}>
                        <Stat label="Open" value={boardSummary ? boardSummary.open_count : "—"} sub="open deals" />
                        <Stat
                            label="Pipeline value"
                            value={boardSummary ? formatMoney(boardSummary.open_value, boardSummary.currency) : "—"}
                            sub={boardSummary?.mixed_currency ? "mixed currencies" : "open · server total"}
                        />
                        <Stat
                            label="Won"
                            value={boardSummary ? formatMoney(boardSummary.won_value, boardSummary.currency) : "—"}
                            sub="closed won"
                        />
                        <Stat label="Stages" value={stages.length} sub="on this pipeline" last />
                    </StatStrip>

                    <SectionBar label={`${stages.length} stages`} />
                    <PageBody className="px-5 py-5">
                        {!currentPipeline || stages.length === 0 ? (
                            <NoStagesYet />
                        ) : (
                            <Board
                                pipelineId={pipelineId}
                                stages={stages}
                                query={search}
                                summary={boardSummary}
                                onMove={moveDeal}
                                onOpen={(d) => setEditing(d)}
                            />
                        )}
                    </PageBody>
                </>
            )}

            <DealDialog
                open={newOpen}
                onClose={() => setNewOpen(false)}
                pipelineId={pipelineId}
                stages={stages}
            />
            <DealDialog
                open={!!editing}
                onClose={() => setEditing(null)}
                pipelineId={editing?.pipeline_id ?? pipelineId}
                stages={editingStages}
                editing={editing ?? undefined}
            />
        </Page>
    );
}

function ViewToggle({
    view,
    onChange,
}: {
    view: "table" | "board";
    onChange: (v: "table" | "board") => void;
}) {
    return (
        <div className="inline-flex rounded-md bg-slate-100 p-0.5 gap-0.5">
            {(
                [
                    ["table", "Table", Table2Icon],
                    ["board", "Board", LayoutGridIcon],
                ] as const
            ).map(([id, label, Icon]) => (
                <button
                    key={id}
                    type="button"
                    onClick={() => onChange(id)}
                    className={`h-6 px-2 rounded text-[11px] font-medium inline-flex items-center gap-1.5 transition-colors ${
                        view === id
                            ? "bg-white text-slate-900 shadow-sm"
                            : "text-slate-500 hover:text-slate-900"
                    }`}
                >
                    <Icon className="w-3 h-3" />
                    {label}
                </button>
            ))}
        </div>
    );
}

function Board({
    pipelineId,
    stages,
    query,
    summary,
    onMove,
    onOpen,
}: {
    pipelineId?: string;
    stages: Stage[];
    query: string;
    summary?: DealsSummary;
    onMove: (dealId: string, stageId: string) => void | Promise<void>;
    onOpen: (deal: Deal) => void;
}) {
    return (
        <div className="grid gap-3 grid-flow-col auto-cols-[280px] overflow-x-auto pb-2">
            {stages.map((stage) => (
                <BoardColumn
                    key={stage.id}
                    pipelineId={pipelineId}
                    stage={stage}
                    query={query}
                    stat={summary?.stages.find((s) => s.stage_id === stage.id)}
                    onDrop={(dealId) => onMove(dealId, stage.id)}
                    onOpen={onOpen}
                />
            ))}
        </div>
    );
}

// Each column is its own server-paginated query scoped to its stage, so a
// 5,000-deal pipeline renders ~15 cards per column (with "load more") instead
// of every node at once. The header count + open value come from the server
// summary, so they stay accurate no matter how many are loaded.
function BoardColumn({
    pipelineId,
    stage,
    query,
    stat,
    onDrop,
    onOpen,
}: {
    pipelineId?: string;
    stage: Stage;
    query: string;
    stat?: { count: number; value: number };
    onDrop: (dealId: string) => void;
    onOpen: (deal: Deal) => void;
}) {
    const [hover, setHover] = React.useState(false);
    const filters = React.useMemo(
        () => ({
            ...EMPTY_DEAL_SEARCH,
            query,
            pipeline_ids: pipelineId ? [pipelineId] : [],
            stage_ids: [stage.id],
            sort_by: "updated_at" as const,
        }),
        [query, pipelineId, stage.id],
    );
    const q = useSearchDeals({ filters, limit: 15 });
    const deals = q.deals ?? [];
    const count = stat?.count ?? q.total;
    const value = stat?.value ?? 0;

    return (
        <div
            onDragOver={(e) => {
                e.preventDefault();
                setHover(true);
            }}
            onDragLeave={() => setHover(false)}
            onDrop={(e) => {
                e.preventDefault();
                setHover(false);
                const dealId = e.dataTransfer.getData("text/deal");
                if (dealId) onDrop(dealId);
            }}
            className={`flex flex-col rounded-md min-h-[300px] max-h-[calc(100vh-230px)] transition-colors ${
                hover ? "bg-sky-50 border-sky-300" : "bg-slate-50 border-slate-200"
            } border`}
        >
            <div className="h-9 px-3 flex items-center gap-2 border-b border-slate-200 shrink-0">
                <span
                    className="size-1.5 rounded-full shrink-0"
                    style={{ backgroundColor: stage.color || "#94a3b8" }}
                />
                <span className="text-[11px] uppercase tracking-[0.1em] font-semibold text-slate-700 truncate">
                    {stage.name}
                </span>
                <span className="ml-auto font-mono text-[10.5px] text-slate-400 tabular-nums">{count}</span>
            </div>
            {value > 0 && (
                <div className="px-3 py-1 border-b border-slate-200/60 bg-white shrink-0">
                    <span className="text-[10.5px] text-emerald-700 font-mono tabular-nums">
                        {formatMoney(value)} open
                    </span>
                </div>
            )}
            <div className="p-2 space-y-1.5 flex-1 overflow-y-auto">
                {q.isPending ? (
                    <>
                        <div className="h-16 rounded-md bg-white border border-slate-200 animate-pulse" />
                        <div className="h-16 rounded-md bg-white border border-slate-200 animate-pulse" />
                    </>
                ) : deals.length === 0 ? (
                    <div className="h-20 rounded-md border border-dashed border-slate-200 flex items-center justify-center text-[10.5px] text-slate-400">
                        Drop deals here
                    </div>
                ) : (
                    <>
                        {deals.map((d) => (
                            <DealCard key={d.id} deal={d} onOpen={onOpen} />
                        ))}
                        {q.hasNextPage && (
                            <button
                                type="button"
                                onClick={() => q.fetchNextPage()}
                                disabled={q.isFetchingNextPage}
                                className="w-full h-7 rounded-md border border-slate-200 hover:border-slate-300 text-[11px] text-slate-600 hover:text-slate-900 inline-flex items-center justify-center gap-1 transition-colors disabled:opacity-50"
                            >
                                {q.isFetchingNextPage ? (
                                    <Loader2Icon className="w-3 h-3 animate-spin" />
                                ) : (
                                    `Load ${Math.max(0, count - deals.length)} more`
                                )}
                            </button>
                        )}
                    </>
                )}
            </div>
        </div>
    );
}

function DealCard({ deal, onOpen }: { deal: Deal; onOpen: (d: Deal) => void }) {
    const status = STATUS_LABEL[deal.status];
    return (
        <div
            draggable
            onDragStart={(e) => {
                e.dataTransfer.effectAllowed = "move";
                e.dataTransfer.setData("text/deal", deal.id);
            }}
            onClick={() => onOpen(deal)}
            className="cursor-pointer rounded-md bg-white border border-slate-200 hover:border-slate-300 hover:shadow-sm transition-all px-2.5 py-2"
        >
            <div className="flex items-center gap-1.5 mb-1">
                <div className="text-[12px] font-medium text-slate-900 truncate flex-1">
                    {deal.name}
                </div>
                {deal.status !== "open" && (
                    <span className={`text-[9.5px] uppercase tracking-[0.08em] font-semibold ${status.tone}`}>
                        {status.label}
                    </span>
                )}
            </div>
            {deal.contact?.email && (
                <div className="flex items-center gap-1 text-[10.5px] text-slate-500 truncate mb-1.5">
                    <UserIcon className="w-2.5 h-2.5 shrink-0" />
                    <span className="truncate">{deal.contact.email}</span>
                </div>
            )}
            <div className="flex items-center gap-2 text-[10.5px]">
                {deal.value !== undefined && deal.value !== null ? (
                    <span className="inline-flex items-center gap-1 text-emerald-600 font-mono tabular-nums">
                        <CircleDollarSignIcon className="w-2.5 h-2.5" />
                        {formatMoney(deal.value, deal.currency)}
                    </span>
                ) : (
                    <span className="text-slate-300">—</span>
                )}
                {deal.expected_close_date && (
                    <span className="inline-flex items-center gap-1 text-slate-400 ml-auto">
                        <CalendarIcon className="w-2.5 h-2.5" />
                        {fmtDate(deal.expected_close_date)}
                    </span>
                )}
            </div>
        </div>
    );
}

function PipelinePicker({
    pipelines,
    currentId,
    onChange,
}: {
    pipelines: { id: string; name: string }[];
    currentId: string | undefined;
    onChange: (id: string) => void;
}) {
    const [open, setOpen] = React.useState(false);
    const cur = pipelines.find((p) => p.id === currentId);

    return (
        <PopoverMenu open={open} onOpenChange={setOpen} align="end">
            <PopoverMenuTrigger asChild>
                <button
                    type="button"
                    className="h-7 px-2.5 rounded-md border border-slate-200 hover:border-slate-300 text-[12px] text-slate-700 hover:text-slate-900 transition-colors inline-flex items-center gap-1.5"
                >
                    {cur?.name ?? "Pick pipeline"}
                    <span className="text-slate-400">▾</span>
                </button>
            </PopoverMenuTrigger>
            <PopoverMenuContent minWidth={200}>
                {pipelines.map((p) => (
                    <PopoverMenuItem
                        key={p.id}
                        onSelect={() => onChange(p.id)}
                        selected={p.id === currentId}
                    >
                        {p.name}
                    </PopoverMenuItem>
                ))}
            </PopoverMenuContent>
        </PopoverMenu>
    );
}

function SearchPill({ value, onChange }: { value: string; onChange: (v: string) => void }) {
    return (
        <div className="h-7 px-2 rounded-md border border-slate-200 bg-white flex items-center gap-1.5 focus-within:border-sky-400 transition-colors">
            <SearchIcon className="w-3 h-3 text-slate-400" />
            <input
                value={value}
                onChange={(e) => onChange(e.target.value)}
                placeholder="Search deals…"
                className="w-[160px] h-5 bg-transparent text-[12px] text-slate-900 placeholder:text-slate-400 outline-none"
            />
            {value && (
                <button type="button" onClick={() => onChange("")} aria-label="Clear" className="text-slate-400 hover:text-slate-700">
                    <XIcon className="w-3 h-3" />
                </button>
            )}
        </div>
    );
}

function BoardSkeleton() {
    return (
        <div className="grid gap-3 grid-flow-col auto-cols-[280px]">
            {[0, 1, 2, 3].map((i) => (
                <div key={i} className="h-72 rounded-md border border-slate-200 bg-slate-50">
                    <div className="h-9 border-b border-slate-200 px-3 flex items-center">
                        <div className="h-3 w-20 bg-slate-200 rounded animate-pulse" />
                    </div>
                    <div className="p-2 space-y-1.5">
                        <div className="h-16 rounded-md bg-white border border-slate-200 animate-pulse" />
                        <div className="h-16 rounded-md bg-white border border-slate-200 animate-pulse" />
                    </div>
                </div>
            ))}
        </div>
    );
}

function NoPipelinesYet() {
    return (
        <div className="rounded-md border border-dashed border-slate-300 bg-slate-50/40 p-8 text-center">
            <div className="mx-auto size-9 rounded-md bg-white border border-slate-200 flex items-center justify-center mb-3">
                <TrophyIcon className="w-4 h-4 text-slate-400" />
            </div>
            <h3 className="text-[13px] font-semibold text-slate-900 mb-1">No pipelines yet</h3>
            <p className="text-[12px] text-slate-500 max-w-md mx-auto mb-4 leading-relaxed">
                Deals live inside pipelines. Head to the Pipelines tab to define one first.
            </p>
            <a
                href="/app/crm/pipelines"
                className="h-7 px-3 rounded-md bg-slate-900 hover:bg-slate-800 text-white text-[12px] font-medium inline-flex items-center gap-1.5 transition-colors"
            >
                Open Pipelines
            </a>
        </div>
    );
}

function NoStagesYet() {
    return (
        <div className="rounded-md border border-dashed border-slate-300 bg-slate-50/40 p-6 text-center">
            <p className="text-[12px] text-slate-600 leading-relaxed">
                This pipeline has no stages yet. Add at least one in the Pipelines tab.
            </p>
        </div>
    );
}

function DealDialog({
    open,
    onClose,
    pipelineId,
    stages,
    editing,
}: {
    open: boolean;
    onClose: () => void;
    pipelineId: string | undefined;
    stages: Stage[];
    editing?: Deal;
}) {
    const create = useCreateDeal();
    const update = useUpdateDeal();
    const del = useDeleteDeal();
    const confirm = useConfirm();

    const [name, setName] = React.useState("");
    const [stageId, setStageId] = React.useState<string>("");
    const [value, setValue] = React.useState<string>("");
    const [currency, setCurrency] = React.useState("USD");
    const [closeDate, setCloseDate] = React.useState<string>("");
    const [status, setStatus] = React.useState<"open" | "won" | "lost">("open");
    const [contactEmail, setContactEmail] = React.useState("");

    React.useEffect(() => {
        if (!open) return;
        if (editing) {
            setName(editing.name);
            setStageId(editing.stage_id);
            setValue(editing.value !== undefined && editing.value !== null ? String(editing.value) : "");
            setCurrency(editing.currency || "USD");
            setCloseDate(editing.expected_close_date ? String(editing.expected_close_date).split("T")[0] : "");
            setStatus(editing.status);
            setContactEmail(editing.contact?.email ?? "");
        } else {
            setName("");
            setStageId(stages[0]?.id ?? "");
            setValue("");
            setCurrency("USD");
            setCloseDate("");
            setStatus("open");
            setContactEmail("");
        }
    }, [open, editing, stages]);

    async function submit() {
        if (!name.trim()) {
            toast.error("Deal name required");
            return;
        }
        if (!stageId) {
            toast.error("Pick a stage");
            return;
        }
        const data: Partial<Deal> = {
            pipeline_id: pipelineId,
            stage_id: stageId,
            name: name.trim(),
            currency: currency || "USD",
        };
        if (value.trim()) {
            const num = Number(value);
            if (!Number.isFinite(num)) {
                toast.error("Value must be a number");
                return;
            }
            data.value = num;
        }
        if (closeDate) data.expected_close_date = new Date(closeDate).toISOString();
        if (editing) data.status = status;

        try {
            if (editing) {
                await toast.promise(update.mutateAsync({ id: editing.id, data }), {
                    loading: "Saving…",
                    success: "Deal updated",
                    error: (e: AppError) => buildError(e),
                });
            } else {
                await toast.promise(create.mutateAsync(data), {
                    loading: "Creating deal…",
                    success: "Deal created",
                    error: (e: AppError) => buildError(e),
                });
            }
            onClose();
        } catch {
            /* surfaced */
        }
    }

    function doDelete() {
        if (!editing) return;
        confirm?.show(`Delete deal "${editing.name}"?`, async () => {
            try {
                await toast.promise(del.mutateAsync(editing.id), {
                    loading: "Deleting…",
                    success: "Deal deleted",
                    error: (e: AppError) => buildError(e),
                });
                onClose();
            } catch {
                /* surfaced */
            }
        });
    }

    return (
        <AnimatePresence>
            {open && (
                <motion.div
                    key="overlay"
                    initial={{ opacity: 0 }}
                    animate={{ opacity: 1 }}
                    exit={{ opacity: 0 }}
                    transition={{ duration: 0.15 }}
                    onClick={onClose}
                    className="fixed inset-0 z-[110] flex items-center justify-center bg-slate-900/30 backdrop-blur-[2px] px-4"
                >
                    <motion.div
                        key="card"
                        initial={{ y: 8, opacity: 0 }}
                        animate={{ y: 0, opacity: 1 }}
                        exit={{ y: 8, opacity: 0 }}
                        transition={{ duration: 0.16 }}
                        onClick={(e) => e.stopPropagation()}
                        className="w-full max-w-[480px] rounded-lg bg-white border border-slate-200 shadow-[0_24px_48px_-12px_rgba(15,23,42,0.18)] overflow-hidden"
                    >
                        <div className="h-12 px-4 border-b border-slate-200 flex items-center gap-2.5">
                            <div className="size-5 rounded bg-slate-100 text-slate-600 flex items-center justify-center">
                                <TrophyIcon className="w-3 h-3" />
                            </div>
                            <span className="text-[10px] uppercase tracking-[0.14em] text-slate-400 font-medium">
                                {editing ? "Edit" : "New"}
                            </span>
                            <div className="h-4 w-px bg-slate-200" />
                            <span className="text-[12.5px] text-slate-900 font-medium">Deal</span>
                            {editing && (
                                <button
                                    type="button"
                                    onClick={doDelete}
                                    className="ml-2 size-7 rounded-md text-slate-400 hover:text-red-600 hover:bg-red-50 inline-flex items-center justify-center transition-colors"
                                    aria-label="Delete deal"
                                >
                                    <TrashIcon className="w-3 h-3" />
                                </button>
                            )}
                            <button
                                type="button"
                                onClick={onClose}
                                aria-label="Close"
                                className="ml-auto size-7 rounded-md text-slate-500 hover:text-slate-900 hover:bg-slate-100 inline-flex items-center justify-center transition-colors"
                            >
                                <XIcon className="w-3.5 h-3.5" />
                            </button>
                        </div>

                        <div className="px-4 py-4 space-y-3">
                            <div>
                                <Label>Deal name</Label>
                                <TextInput value={name} onChange={setName} placeholder="e.g. Q1 outbound · Acme" autoFocus className="w-full" />
                            </div>
                            <div className="grid grid-cols-1 sm:grid-cols-2 gap-2">
                                <div>
                                    <Label>Stage</Label>
                                    <StagePill stages={stages} value={stageId} onChange={setStageId} />
                                </div>
                                {editing && (
                                    <div>
                                        <Label>Status</Label>
                                        <div className="inline-flex rounded-md bg-slate-100 p-0.5 gap-0.5 w-full">
                                            {(["open", "won", "lost"] as const).map((s) => (
                                                <button
                                                    key={s}
                                                    type="button"
                                                    onClick={() => setStatus(s)}
                                                    className={`flex-1 h-6 px-2 rounded text-[11px] font-medium transition-colors ${
                                                        status === s
                                                            ? "bg-white text-slate-900 shadow-sm"
                                                            : "text-slate-500 hover:text-slate-900"
                                                    }`}
                                                >
                                                    {STATUS_LABEL[s].label}
                                                </button>
                                            ))}
                                        </div>
                                    </div>
                                )}
                            </div>
                            <div className="grid grid-cols-1 sm:grid-cols-3 gap-2">
                                <div className="col-span-2">
                                    <Label>Value</Label>
                                    <TextInput value={value} onChange={setValue} placeholder="12000" className="w-full" />
                                </div>
                                <div>
                                    <Label>Currency</Label>
                                    <TextInput value={currency} onChange={setCurrency} placeholder="USD" className="w-full uppercase" />
                                </div>
                            </div>
                            <div className="grid grid-cols-2 gap-2">
                                <div>
                                    <Label>Expected close</Label>
                                    <TextInput value={closeDate} onChange={setCloseDate} type="date" className="w-full" />
                                </div>
                                <div>
                                    <Label>Contact (read-only)</Label>
                                    <TextInput value={contactEmail} onChange={setContactEmail} disabled placeholder="—" className="w-full" />
                                </div>
                            </div>
                        </div>

                        <div className="px-3 h-12 border-t border-slate-200 flex items-center gap-1.5">
                            <button
                                type="button"
                                onClick={onClose}
                                className="ml-auto h-7 px-2.5 rounded-md text-[12px] text-slate-700 hover:text-slate-900 hover:bg-slate-100 transition-colors"
                            >
                                Cancel
                            </button>
                            <button
                                type="button"
                                onClick={submit}
                                disabled={create.isPending || update.isPending}
                                className="h-7 px-3 rounded-md bg-slate-900 hover:bg-slate-800 text-white text-[12px] font-medium inline-flex items-center gap-1.5 transition-colors disabled:opacity-60"
                            >
                                {(create.isPending || update.isPending) && (
                                    <Loader2Icon className="w-3 h-3 animate-spin" />
                                )}
                                {editing ? "Save deal" : "Create deal"}
                            </button>
                        </div>
                    </motion.div>
                </motion.div>
            )}
        </AnimatePresence>
    );
}

function StagePill({
    stages,
    value,
    onChange,
}: {
    stages: Stage[];
    value: string;
    onChange: (id: string) => void;
}) {
    const [open, setOpen] = React.useState(false);
    const cur = stages.find((s) => s.id === value);

    return (
        <PopoverMenu open={open} onOpenChange={setOpen} align="start">
            <PopoverMenuTrigger asChild>
                <button
                    type="button"
                    className="h-7 w-full px-2.5 rounded-md border border-slate-200 hover:border-slate-300 text-[12px] text-slate-700 hover:text-slate-900 transition-colors inline-flex items-center gap-1.5"
                >
                    {cur ? (
                        <>
                            <span
                                className="size-1.5 rounded-full shrink-0"
                                style={{ backgroundColor: cur.color || "#94a3b8" }}
                            />
                            <span className="truncate">{cur.name}</span>
                        </>
                    ) : (
                        <span className="text-slate-400">Select…</span>
                    )}
                    <span className="ml-auto text-slate-400">▾</span>
                </button>
            </PopoverMenuTrigger>
            <PopoverMenuContent minWidth={200} className="max-h-56 overflow-y-auto">
                {stages.map((s) => (
                    <PopoverMenuItem
                        key={s.id}
                        onSelect={() => onChange(s.id)}
                        selected={s.id === value}
                        icon={
                            <span
                                className="size-1.5 rounded-full shrink-0 block"
                                style={{ backgroundColor: s.color || "#94a3b8" }}
                            />
                        }
                    >
                        {s.name}
                    </PopoverMenuItem>
                ))}
            </PopoverMenuContent>
        </PopoverMenu>
    );
}

function formatMoney(n: number | undefined, currency = "USD") {
    if (n === undefined || n === null) return "—";
    try {
        return new Intl.NumberFormat("en-US", {
            style: "currency",
            currency: currency || "USD",
            maximumFractionDigits: 0,
        }).format(n);
    } catch {
        return `$${n.toLocaleString()}`;
    }
}

function fmtDate(d: string | undefined) {
    if (!d) return "—";
    try {
        return new Date(d).toLocaleDateString("en-US", { month: "short", day: "numeric" });
    } catch {
        return "—";
    }
}
