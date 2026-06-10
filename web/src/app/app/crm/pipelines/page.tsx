// Pipelines — manage stage flows that deals advance through.
//
// Replaces the placeholder ribbon with a real CRUD interface:
//   - list every pipeline this workspace owns
//   - create a new pipeline + initial stages
//   - rename + recolor stages inline
//   - delete pipelines + stages
//
// Visual language matches the brae chrome (slate-900 / hairline / 12.5px).

import React from "react";
import {
    ArrowRightIcon,
    CheckIcon,
    Loader2Icon,
    MoreHorizontalIcon,
    PencilIcon,
    PlusIcon,
    Settings2Icon,
    TrashIcon,
    XIcon,
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
    PopoverMenuContent,
    PopoverMenuItem,
    PopoverMenuTrigger,
} from "@/components/ui/popover-menu";
import usePipelines from "@/lib/api/hooks/app/crm/pipelines/usePipelines";
import useCreatePipeline from "@/lib/api/hooks/app/crm/pipelines/useCreatePipeline";
import useDeletePipeline from "@/lib/api/hooks/app/crm/pipelines/useDeletePipeline";
import useCreateStage from "@/lib/api/hooks/app/crm/pipelines/useCreateStage";
import useUpdateStage from "@/lib/api/hooks/app/crm/pipelines/useUpdateStage";
import useDeleteStage from "@/lib/api/hooks/app/crm/pipelines/useDeleteStage";
import useUpdatePipeline from "@/lib/api/hooks/app/crm/pipelines/useUpdatePipeline";
import { useConfirm } from "@/hooks/context/confirm";
import type Pipeline from "@/lib/api/models/app/crm/Pipeline";
import type { Stage } from "@/lib/api/models/app/crm/Pipeline";
import type { AppError } from "@/lib/api/client/normalizeError";
import buildError from "@/lib/helper/buildError";

const STAGE_COLORS = [
    { id: "slate",   bg: "bg-slate-400",   hex: "#94a3b8" },
    { id: "sky",     bg: "bg-sky-500",     hex: "#0ea5e9" },
    { id: "violet",  bg: "bg-violet-500",  hex: "#8b5cf6" },
    { id: "amber",   bg: "bg-amber-500",   hex: "#f59e0b" },
    { id: "emerald", bg: "bg-emerald-500", hex: "#10b981" },
    { id: "rose",    bg: "bg-rose-500",    hex: "#f43f5e" },
    { id: "indigo",  bg: "bg-indigo-500",  hex: "#6366f1" },
    { id: "teal",    bg: "bg-teal-500",    hex: "#14b8a6" },
];

function colorForHex(hex: string) {
    const m = STAGE_COLORS.find((c) => c.hex.toLowerCase() === (hex ?? "").toLowerCase());
    return m ?? STAGE_COLORS[0];
}

export default function PipelinesPage() {
    const pipelines = usePipelines();
    const [newPipelineOpen, setNewPipelineOpen] = React.useState(false);

    const list = pipelines.data ?? [];
    const totalStages = list.reduce((acc, p) => acc + (p.stages?.length ?? 0), 0);

    return (
        <Page>
            <PageTopbar
                eyebrow="Pipelines"
                subtitle="Stages a deal moves through · custom per workflow"
            >
                <TopbarAction
                    icon={<PlusIcon className="w-3 h-3" />}
                    onClick={() => setNewPipelineOpen(true)}
                >
                    New pipeline
                </TopbarAction>
            </PageTopbar>

            <StatStrip cols={4}>
                <Stat label="Pipelines" value={list.length} sub="defined" />
                <Stat label="Stages" value={totalStages} sub="across pipelines" />
                <Stat label="Avg stages" value={list.length ? Math.round(totalStages / list.length) : 0} sub="per pipeline" />
                <Stat label="Last modified" value={lastModified(list)} sub="any pipeline" last />
            </StatStrip>

            <SectionBar label={pipelines.isPending ? "Loading…" : `${list.length} pipelines`} />
            <PageBody className="px-5 py-5">
                {pipelines.isPending ? (
                    <SkeletonStrip />
                ) : list.length === 0 ? (
                    <EmptyState onCreate={() => setNewPipelineOpen(true)} />
                ) : (
                    <div className="space-y-4">
                        {list.map((p) => (
                            <PipelineCard key={p.id} pipeline={p} />
                        ))}
                    </div>
                )}
            </PageBody>

            <NewPipelineDialog open={newPipelineOpen} onClose={() => setNewPipelineOpen(false)} />
        </Page>
    );
}

function lastModified(list: Pipeline[]) {
    if (list.length === 0) return "—";
    const max = list.reduce(
        (acc, p) => Math.max(acc, new Date(p.updated_at).getTime()),
        0,
    );
    if (!max) return "—";
    return new Date(max).toLocaleDateString("en-US", {
        month: "short",
        day: "numeric",
    });
}

function SkeletonStrip() {
    return (
        <div className="space-y-3">
            {[0, 1].map((i) => (
                <div key={i} className="h-32 rounded-md bg-slate-100 animate-pulse" />
            ))}
        </div>
    );
}

function EmptyState({ onCreate }: { onCreate: () => void }) {
    return (
        <div className="rounded-md border border-dashed border-slate-300 bg-slate-50/40 p-8 text-center">
            <div className="mx-auto size-9 rounded-md bg-white border border-slate-200 flex items-center justify-center mb-3">
                <Settings2Icon className="w-4 h-4 text-slate-400" />
            </div>
            <h3 className="text-[13px] font-semibold text-slate-900 mb-1">No pipelines yet</h3>
            <p className="text-[12px] text-slate-500 max-w-md mx-auto mb-4 leading-relaxed">
                A pipeline is a named sequence of stages a deal moves through. Start
                with one and add stages as you go.
            </p>
            <button
                type="button"
                onClick={onCreate}
                className="h-7 px-3 rounded-md bg-slate-900 hover:bg-slate-800 text-white text-[12px] font-medium inline-flex items-center gap-1.5 transition-colors"
            >
                <PlusIcon className="w-3 h-3" />
                Create pipeline
            </button>
        </div>
    );
}

function PipelineCard({ pipeline }: { pipeline: Pipeline }) {
    const updatePipeline = useUpdatePipeline();
    const deletePipeline = useDeletePipeline();
    const createStage = useCreateStage();
    const confirm = useConfirm();

    const [renaming, setRenaming] = React.useState(false);
    const [name, setName] = React.useState(pipeline.name);
    const [menuOpen, setMenuOpen] = React.useState(false);
    const [addStageOpen, setAddStageOpen] = React.useState(false);

    React.useEffect(() => setName(pipeline.name), [pipeline.name]);

    async function saveRename() {
        if (!name.trim() || name.trim() === pipeline.name) {
            setRenaming(false);
            return;
        }
        try {
            await toast.promise(
                updatePipeline.mutateAsync({ id: pipeline.id, data: { name: name.trim() } }),
                {
                    loading: "Renaming…",
                    success: "Pipeline renamed",
                    error: (e: AppError) => buildError(e),
                },
            );
            setRenaming(false);
        } catch {
            /* surfaced */
        }
    }

    function doDelete() {
        confirm?.show(`Delete pipeline "${pipeline.name}"? All stages will be removed.`, async () => {
            try {
                await toast.promise(deletePipeline.mutateAsync(pipeline.id), {
                    loading: "Deleting…",
                    success: "Pipeline deleted",
                    error: (e: AppError) => buildError(e),
                });
            } catch {
                /* surfaced */
            }
        });
    }

    const sorted = [...(pipeline.stages ?? [])].sort((a, b) => a.position - b.position);

    return (
        <div className="rounded-md border border-slate-200 bg-white overflow-hidden">
            <div className="h-10 px-3 border-b border-slate-200 flex items-center gap-2">
                <Settings2Icon className="w-3 h-3 text-slate-400 shrink-0" />
                {renaming ? (
                    <div className="flex items-center gap-1 flex-1 min-w-0">
                        <TextInput
                            value={name}
                            onChange={setName}
                            onKeyDown={(e) => {
                                if (e.key === "Enter") saveRename();
                                if (e.key === "Escape") {
                                    setName(pipeline.name);
                                    setRenaming(false);
                                }
                            }}
                            autoFocus
                            className="h-6 text-[12px] w-full"
                        />
                        <button
                            type="button"
                            onClick={saveRename}
                            disabled={updatePipeline.isPending}
                            aria-label="Save name"
                            className="size-6 rounded text-emerald-600 hover:bg-emerald-50 inline-flex items-center justify-center"
                        >
                            <CheckIcon className="w-3 h-3" />
                        </button>
                        <button
                            type="button"
                            onClick={() => {
                                setName(pipeline.name);
                                setRenaming(false);
                            }}
                            aria-label="Cancel rename"
                            className="size-6 rounded text-slate-400 hover:bg-slate-100 inline-flex items-center justify-center"
                        >
                            <XIcon className="w-3 h-3" />
                        </button>
                    </div>
                ) : (
                    <button
                        type="button"
                        onDoubleClick={() => setRenaming(true)}
                        className="text-[12.5px] font-semibold text-slate-900 truncate"
                    >
                        {pipeline.name}
                    </button>
                )}
                <span className="text-[10.5px] font-mono text-slate-400 tabular-nums">
                    {sorted.length} {sorted.length === 1 ? "stage" : "stages"}
                </span>
                <div className="ml-auto flex items-center gap-1">
                    <button
                        type="button"
                        onClick={() => setAddStageOpen(true)}
                        className="h-6 px-2 rounded-md border border-slate-200 hover:border-slate-300 text-[11px] text-slate-700 hover:text-slate-900 inline-flex items-center gap-1 transition-colors"
                    >
                        <PlusIcon className="w-2.5 h-2.5" />
                        Stage
                    </button>
                    <PopoverMenu open={menuOpen} onOpenChange={setMenuOpen} align="end">
                        <PopoverMenuTrigger asChild>
                            <button
                                type="button"
                                aria-label="Pipeline menu"
                                className="size-6 rounded-md text-slate-400 hover:text-slate-900 hover:bg-slate-100 inline-flex items-center justify-center"
                            >
                                <MoreHorizontalIcon className="w-3 h-3" />
                            </button>
                        </PopoverMenuTrigger>
                        <PopoverMenuContent minWidth={176}>
                            <PopoverMenuItem
                                onSelect={() => setRenaming(true)}
                                icon={<PencilIcon className="w-3 h-3" />}
                            >
                                Rename
                            </PopoverMenuItem>
                            <PopoverMenuItem
                                onSelect={doDelete}
                                icon={<TrashIcon className="w-3 h-3" />}
                                danger
                            >
                                Delete pipeline
                            </PopoverMenuItem>
                        </PopoverMenuContent>
                    </PopoverMenu>
                </div>
            </div>
            <div className="p-3 flex flex-wrap items-stretch gap-2">
                {sorted.length === 0 ? (
                    <div className="w-full text-[11.5px] text-slate-400 italic text-center py-3">
                        No stages yet — add the first one to start tracking deals.
                    </div>
                ) : (
                    sorted.map((s, idx) => (
                        <React.Fragment key={s.id}>
                            <StageCell stage={s} pipelineId={pipeline.id} />
                            {idx < sorted.length - 1 && (
                                <div className="flex items-center text-slate-300">
                                    <ArrowRightIcon className="w-3 h-3" />
                                </div>
                            )}
                        </React.Fragment>
                    ))
                )}
            </div>

            <AddStageDialog
                open={addStageOpen}
                onClose={() => setAddStageOpen(false)}
                onConfirm={async (data) => {
                    const nextPos = sorted.length > 0 ? sorted[sorted.length - 1].position + 1 : 0;
                    try {
                        await toast.promise(
                            createStage.mutateAsync({
                                pipelineId: pipeline.id,
                                data: { name: data.name, color: data.color, position: nextPos },
                            }),
                            {
                                loading: "Adding stage…",
                                success: "Stage added",
                                error: (e: AppError) => buildError(e),
                            },
                        );
                        setAddStageOpen(false);
                    } catch {
                        /* surfaced */
                    }
                }}
                pending={createStage.isPending}
            />
        </div>
    );
}

function StageCell({ stage, pipelineId: _pipelineId }: { stage: Stage; pipelineId: string }) {
    const updateStage = useUpdateStage();
    const deleteStage = useDeleteStage();
    const confirm = useConfirm();
    const [editing, setEditing] = React.useState(false);
    const [name, setName] = React.useState(stage.name);
    const [colorOpen, setColorOpen] = React.useState(false);
    const color = colorForHex(stage.color);

    React.useEffect(() => setName(stage.name), [stage.name]);

    async function saveName() {
        if (!name.trim() || name.trim() === stage.name) {
            setEditing(false);
            return;
        }
        try {
            await toast.promise(
                updateStage.mutateAsync({ pipelineId: _pipelineId, stageId: stage.id, data: { name: name.trim() } }),
                {
                    loading: "Saving…",
                    success: "Stage updated",
                    error: (e: AppError) => buildError(e),
                },
            );
            setEditing(false);
        } catch {
            /* surfaced */
        }
    }

    async function changeColor(hex: string) {
        try {
            await toast.promise(
                updateStage.mutateAsync({ pipelineId: _pipelineId, stageId: stage.id, data: { color: hex } }),
                { loading: "Saving…", success: "Stage recolored", error: (e: AppError) => buildError(e) },
            );
        } finally {
            setColorOpen(false);
        }
    }

    function doDelete() {
        confirm?.show(`Delete stage "${stage.name}"?`, async () => {
            try {
                await toast.promise(deleteStage.mutateAsync({ pipelineId: _pipelineId, stageId: stage.id }), {
                    loading: "Deleting…",
                    success: "Stage deleted",
                    error: (e: AppError) => buildError(e),
                });
            } catch {
                /* surfaced */
            }
        });
    }

    return (
        <div className="group min-w-[140px] rounded-md border border-slate-200 bg-slate-50 hover:bg-white hover:border-slate-300 px-2.5 py-2 transition-colors">
            <div className="flex items-center gap-1.5 mb-1">
                <PopoverMenu open={colorOpen} onOpenChange={setColorOpen} align="start">
                    <PopoverMenuTrigger asChild>
                        <button
                            type="button"
                            aria-label="Change color"
                            className={`size-1.5 rounded-full ${color.bg} relative max-md:after:absolute max-md:after:-inset-2.5 max-md:after:content-['']`}
                        />
                    </PopoverMenuTrigger>
                    <PopoverMenuContent minWidth={1} className="p-1.5">
                        <div className="flex gap-1">
                            {STAGE_COLORS.map((c) => (
                                <button
                                    key={c.id}
                                    type="button"
                                    onClick={() => changeColor(c.hex)}
                                    aria-label={c.id}
                                    className={`size-4 rounded-full ${c.bg} ring-offset-1 ring-1 ring-transparent hover:ring-slate-300 transition-shadow ${
                                        c.hex.toLowerCase() === (stage.color ?? "").toLowerCase()
                                            ? "ring-slate-900"
                                            : ""
                                    }`}
                                />
                            ))}
                        </div>
                    </PopoverMenuContent>
                </PopoverMenu>
                {editing ? (
                    <input
                        value={name}
                        onChange={(e) => setName(e.target.value)}
                        onBlur={saveName}
                        onKeyDown={(e) => {
                            if (e.key === "Enter") saveName();
                            if (e.key === "Escape") {
                                setName(stage.name);
                                setEditing(false);
                            }
                        }}
                        autoFocus
                        className="bg-transparent w-full text-[11.5px] font-medium text-slate-900 outline-none"
                    />
                ) : (
                    <button
                        type="button"
                        onDoubleClick={() => setEditing(true)}
                        className="text-[11.5px] font-medium text-slate-900 truncate text-left"
                    >
                        {stage.name}
                    </button>
                )}
                {!editing && (
                    <button
                        type="button"
                        onClick={() => setEditing(true)}
                        aria-label="Rename stage"
                        className="md:hidden size-4 rounded text-slate-300 hover:text-slate-600 hover:bg-slate-100 inline-flex items-center justify-center transition-colors shrink-0"
                    >
                        <PencilIcon className="w-2.5 h-2.5" />
                    </button>
                )}
                <button
                    type="button"
                    onClick={doDelete}
                    aria-label="Delete stage"
                    className="ml-auto size-4 rounded text-slate-300 hover:text-red-600 hover:bg-red-50 inline-flex items-center justify-center transition-colors opacity-100 md:opacity-0 md:group-hover:opacity-100"
                >
                    <XIcon className="w-2.5 h-2.5" />
                </button>
            </div>
            <div className="text-[10px] text-slate-400 tabular-nums font-mono">
                {stage.deal_count ?? 0} {stage.deal_count === 1 ? "deal" : "deals"}
            </div>
        </div>
    );
}

function NewPipelineDialog({ open, onClose }: { open: boolean; onClose: () => void }) {
    const create = useCreatePipeline();
    const createStage = useCreateStage();
    const [name, setName] = React.useState("");
    const [stages, setStages] = React.useState<{ name: string; color: string }[]>([
        { name: "Open",       color: STAGE_COLORS[0].hex },
        { name: "Qualified",  color: STAGE_COLORS[1].hex },
        { name: "Won",        color: STAGE_COLORS[4].hex },
    ]);

    React.useEffect(() => {
        if (!open) {
            setName("");
            setStages([
                { name: "Open",       color: STAGE_COLORS[0].hex },
                { name: "Qualified",  color: STAGE_COLORS[1].hex },
                { name: "Won",        color: STAGE_COLORS[4].hex },
            ]);
        }
    }, [open]);

    async function submit() {
        if (!name.trim()) {
            toast.error("Name required");
            return;
        }
        try {
            const p = await toast.promise(create.mutateAsync({ name: name.trim() }), {
                loading: "Creating pipeline…",
                success: "Pipeline created",
                error: (e: AppError) => buildError(e),
            });
            // Create stages serially after the pipeline exists.
            for (let i = 0; i < stages.length; i++) {
                const s = stages[i];
                if (!s.name.trim()) continue;
                await createStage.mutateAsync({
                    pipelineId: p.id,
                    data: { name: s.name.trim(), color: s.color, position: i },
                });
            }
            onClose();
        } catch {
            /* surfaced */
        }
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
                        className="w-full max-w-[520px] rounded-lg bg-white border border-slate-200 shadow-[0_24px_48px_-12px_rgba(15,23,42,0.18)] overflow-hidden"
                    >
                        <div className="h-12 px-4 border-b border-slate-200 flex items-center gap-2.5">
                            <div className="size-5 rounded bg-slate-100 text-slate-600 flex items-center justify-center">
                                <Settings2Icon className="w-3 h-3" />
                            </div>
                            <span className="text-[10px] uppercase tracking-[0.14em] text-slate-400 font-medium">
                                New
                            </span>
                            <div className="h-4 w-px bg-slate-200" />
                            <span className="text-[12.5px] text-slate-900 font-medium">Pipeline</span>
                            <button
                                type="button"
                                onClick={onClose}
                                aria-label="Close"
                                className="ml-auto size-7 rounded-md text-slate-500 hover:text-slate-900 hover:bg-slate-100 inline-flex items-center justify-center transition-colors"
                            >
                                <XIcon className="w-3.5 h-3.5" />
                            </button>
                        </div>

                        <div className="px-4 py-4 space-y-3 max-h-[60vh] overflow-y-auto">
                            <div>
                                <Label>Pipeline name</Label>
                                <TextInput
                                    value={name}
                                    onChange={setName}
                                    placeholder="Outbound · Sales"
                                    autoFocus
                                    className="w-full"
                                />
                            </div>

                            <div>
                                <div className="flex items-center justify-between mb-1.5">
                                    <Label className="!mb-0">Initial stages</Label>
                                    <button
                                        type="button"
                                        onClick={() =>
                                            setStages((s) => [
                                                ...s,
                                                { name: "", color: STAGE_COLORS[s.length % STAGE_COLORS.length].hex },
                                            ])
                                        }
                                        className="h-6 px-2 rounded-md border border-slate-200 hover:border-slate-300 text-[11px] text-slate-600 hover:text-slate-900 inline-flex items-center gap-1 transition-colors"
                                    >
                                        <PlusIcon className="w-3 h-3" />
                                        Add stage
                                    </button>
                                </div>
                                <div className="space-y-1.5">
                                    {stages.map((s, i) => (
                                        <div key={i} className="flex items-center gap-1.5">
                                            <ColorDot
                                                hex={s.color}
                                                onChange={(hex) =>
                                                    setStages((cur) => cur.map((c, ii) => (ii === i ? { ...c, color: hex } : c)))
                                                }
                                            />
                                            <TextInput
                                                value={s.name}
                                                onChange={(v) =>
                                                    setStages((cur) => cur.map((c, ii) => (ii === i ? { ...c, name: v } : c)))
                                                }
                                                placeholder="Stage name"
                                                className="flex-1"
                                            />
                                            <button
                                                type="button"
                                                onClick={() =>
                                                    setStages((cur) => cur.filter((_, ii) => ii !== i))
                                                }
                                                aria-label="Remove stage"
                                                className="size-7 rounded-md text-slate-400 hover:text-red-600 hover:bg-red-50 inline-flex items-center justify-center transition-colors shrink-0"
                                            >
                                                <TrashIcon className="w-3 h-3" />
                                            </button>
                                        </div>
                                    ))}
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
                                disabled={create.isPending || createStage.isPending}
                                className="h-7 px-3 rounded-md bg-slate-900 hover:bg-slate-800 text-white text-[12px] font-medium inline-flex items-center gap-1.5 transition-colors disabled:opacity-60"
                            >
                                {(create.isPending || createStage.isPending) && (
                                    <Loader2Icon className="w-3 h-3 animate-spin" />
                                )}
                                Create
                            </button>
                        </div>
                    </motion.div>
                </motion.div>
            )}
        </AnimatePresence>
    );
}

function AddStageDialog({
    open,
    onClose,
    onConfirm,
    pending,
}: {
    open: boolean;
    onClose: () => void;
    onConfirm: (data: { name: string; color: string }) => Promise<void>;
    pending: boolean;
}) {
    const [name, setName] = React.useState("");
    const [color, setColor] = React.useState(STAGE_COLORS[0].hex);

    React.useEffect(() => {
        if (!open) {
            setName("");
            setColor(STAGE_COLORS[0].hex);
        }
    }, [open]);

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
                        className="w-full max-w-[400px] rounded-lg bg-white border border-slate-200 shadow-[0_24px_48px_-12px_rgba(15,23,42,0.18)]"
                    >
                        <div className="h-11 px-4 border-b border-slate-200 flex items-center gap-2">
                            <span className="text-[10px] uppercase tracking-[0.14em] text-slate-400 font-medium">
                                New
                            </span>
                            <div className="h-4 w-px bg-slate-200" />
                            <span className="text-[12.5px] text-slate-900 font-medium">Stage</span>
                            <button
                                type="button"
                                onClick={onClose}
                                className="ml-auto size-7 rounded-md text-slate-500 hover:text-slate-900 hover:bg-slate-100 inline-flex items-center justify-center transition-colors"
                            >
                                <XIcon className="w-3.5 h-3.5" />
                            </button>
                        </div>
                        <div className="px-4 py-4 space-y-3">
                            <div>
                                <Label>Stage name</Label>
                                <TextInput
                                    value={name}
                                    onChange={setName}
                                    placeholder="e.g. Demo set"
                                    autoFocus
                                    className="w-full"
                                />
                            </div>
                            <div>
                                <Label>Color</Label>
                                <div className="flex gap-1.5">
                                    {STAGE_COLORS.map((c) => (
                                        <button
                                            key={c.id}
                                            type="button"
                                            onClick={() => setColor(c.hex)}
                                            aria-label={c.id}
                                            className={`size-5 rounded-full ${c.bg} ring-offset-1 ring-1 ring-transparent hover:ring-slate-300 transition-shadow ${
                                                c.hex === color ? "ring-slate-900" : ""
                                            }`}
                                        />
                                    ))}
                                </div>
                            </div>
                        </div>
                        <div className="px-3 h-11 border-t border-slate-200 flex items-center gap-1.5">
                            <button
                                type="button"
                                onClick={onClose}
                                className="ml-auto h-7 px-2.5 rounded-md text-[12px] text-slate-700 hover:text-slate-900 hover:bg-slate-100 transition-colors"
                            >
                                Cancel
                            </button>
                            <button
                                type="button"
                                onClick={() => name.trim() && onConfirm({ name: name.trim(), color })}
                                disabled={pending || !name.trim()}
                                className="h-7 px-3 rounded-md bg-slate-900 hover:bg-slate-800 text-white text-[12px] font-medium inline-flex items-center gap-1.5 transition-colors disabled:opacity-60"
                            >
                                {pending && <Loader2Icon className="w-3 h-3 animate-spin" />}
                                Add stage
                            </button>
                        </div>
                    </motion.div>
                </motion.div>
            )}
        </AnimatePresence>
    );
}

function ColorDot({ hex, onChange }: { hex: string; onChange: (hex: string) => void }) {
    const [open, setOpen] = React.useState(false);
    const color = colorForHex(hex);
    return (
        <PopoverMenu open={open} onOpenChange={setOpen} align="start">
            <PopoverMenuTrigger asChild>
                <button
                    type="button"
                    aria-label="Color"
                    className={`shrink-0 size-7 rounded-md border border-slate-200 hover:border-slate-300 transition-colors flex items-center justify-center bg-white`}
                >
                    <span className={`size-3 rounded-full ${color.bg}`} />
                </button>
            </PopoverMenuTrigger>
            <PopoverMenuContent minWidth={1} className="p-1.5">
                <div className="flex gap-1">
                    {STAGE_COLORS.map((c) => (
                        <button
                            key={c.id}
                            type="button"
                            onClick={() => {
                                onChange(c.hex);
                                setOpen(false);
                            }}
                            className={`size-4 rounded-full ${c.bg} ring-offset-1 ring-1 ring-transparent hover:ring-slate-300 transition-shadow ${
                                c.hex === hex ? "ring-slate-900" : ""
                            }`}
                        />
                    ))}
                </div>
            </PopoverMenuContent>
        </PopoverMenu>
    );
}
