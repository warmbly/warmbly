// DealStagePicker — a paired pipeline + stage selector for CRM deal automation.
// Mirrors the inline StagePicker pattern used in the unibox ContactContextPanel
// (PopoverMenu trigger + coloured stage dots), but adds a pipeline selector and
// loads the org's pipelines itself. Used by the campaign "Create deal" /
// "Move deal stage" action editors.
//
// The pipeline + stage are persisted as the action's deal_pipeline_id /
// deal_stage_id. When the pipeline changes, the stage resets to that pipeline's
// first stage so the pair always stays consistent.

import React from "react";
import { ChevronDownIcon } from "lucide-react";

import {
    PopoverMenu,
    PopoverMenuTrigger,
    PopoverMenuContent,
    PopoverMenuItem,
} from "@/components/ui/popover-menu";
import usePipelines from "@/lib/api/hooks/app/crm/pipelines/usePipelines";
import type { Stage } from "@/lib/api/models/app/crm/Pipeline";

function sortedStages(stages: Stage[] | undefined): Stage[] {
    return stages ? [...stages].sort((a, b) => a.position - b.position) : [];
}

export default function DealStagePicker({
    pipelineId,
    stageId,
    onChange,
}: {
    pipelineId?: string;
    stageId?: string;
    onChange: (next: { pipelineId: string; stageId: string }) => void;
}) {
    const { data: pipelines = [], isPending } = usePipelines();

    const pipeline = pipelines.find((p) => p.id === pipelineId);
    const stages = sortedStages(pipeline?.stages);

    const pickPipeline = (id: string) => {
        const p = pipelines.find((x) => x.id === id);
        const first = sortedStages(p?.stages)[0];
        onChange({ pipelineId: id, stageId: first?.id ?? "" });
    };

    if (!isPending && pipelines.length === 0) {
        return (
            <p className="rounded-md border border-slate-200 bg-slate-50/60 px-3 py-2.5 text-[11.5px] leading-relaxed text-slate-600">
                You don't have a CRM pipeline yet. Create one under CRM, then come back to choose where deals land.
            </p>
        );
    }

    return (
        <div className="grid grid-cols-2 gap-2">
            <div>
                <p className="mb-1 text-[10px] font-medium uppercase tracking-[0.14em] text-slate-400">Pipeline</p>
                <PipelineSelect
                    pipelines={pipelines.map((p) => ({ id: p.id, name: p.name }))}
                    value={pipelineId}
                    onChange={pickPipeline}
                />
            </div>
            <div>
                <p className="mb-1 text-[10px] font-medium uppercase tracking-[0.14em] text-slate-400">Stage</p>
                <StageSelect
                    stages={stages}
                    value={stageId}
                    disabled={!pipeline}
                    onChange={(id) => onChange({ pipelineId: pipelineId ?? "", stageId: id })}
                />
            </div>
        </div>
    );
}

function PipelineSelect({
    pipelines,
    value,
    onChange,
}: {
    pipelines: { id: string; name: string }[];
    value?: string;
    onChange: (id: string) => void;
}) {
    const [open, setOpen] = React.useState(false);
    const cur = pipelines.find((p) => p.id === value);
    return (
        <PopoverMenu open={open} onOpenChange={setOpen} align="start">
            <PopoverMenuTrigger asChild>
                <button
                    type="button"
                    className="h-7 w-full px-2 rounded-md border border-slate-200 hover:border-slate-300 bg-white text-[12px] text-slate-700 inline-flex items-center gap-1.5 transition-colors"
                >
                    <span className="truncate flex-1 text-left">{cur?.name ?? "Pick a pipeline…"}</span>
                    <ChevronDownIcon className="w-3 h-3 text-slate-400 shrink-0" />
                </button>
            </PopoverMenuTrigger>
            <PopoverMenuContent minWidth={180} className="max-h-56 overflow-y-auto">
                {pipelines.map((p) => (
                    <PopoverMenuItem key={p.id} onSelect={() => onChange(p.id)} selected={p.id === value}>
                        {p.name}
                    </PopoverMenuItem>
                ))}
            </PopoverMenuContent>
        </PopoverMenu>
    );
}

function StageSelect({
    stages,
    value,
    disabled,
    onChange,
}: {
    stages: Stage[];
    value?: string;
    disabled?: boolean;
    onChange: (id: string) => void;
}) {
    const [open, setOpen] = React.useState(false);
    const cur = stages.find((s) => s.id === value);
    return (
        <PopoverMenu open={open} onOpenChange={setOpen} align="start">
            <PopoverMenuTrigger asChild>
                <button
                    type="button"
                    disabled={disabled || stages.length === 0}
                    className="h-7 w-full px-2 rounded-md border border-slate-200 hover:border-slate-300 bg-white text-[12px] text-slate-700 inline-flex items-center gap-1.5 transition-colors disabled:cursor-not-allowed disabled:opacity-60"
                >
                    <span className="size-1.5 rounded-full shrink-0" style={{ backgroundColor: cur?.color || "#cbd5e1" }} />
                    <span className="truncate flex-1 text-left">{cur?.name ?? "Pick a stage…"}</span>
                    <ChevronDownIcon className="w-3 h-3 text-slate-400 shrink-0" />
                </button>
            </PopoverMenuTrigger>
            <PopoverMenuContent minWidth={180} className="max-h-56 overflow-y-auto">
                {stages.map((s) => (
                    <PopoverMenuItem
                        key={s.id}
                        onSelect={() => onChange(s.id)}
                        selected={s.id === value}
                        icon={
                            <span
                                className="size-2 rounded-full block"
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
