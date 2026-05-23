// Pipelines — stage flow preview.
//
// The page reads as a left-to-right ribbon of stages with arrows
// between them. Distinct from Deals (which is a kanban board) — same
// data, different shape. The ribbon makes "what flow does a deal take
// before it closes?" the obvious question this page answers.

import { ArrowRightIcon, PlusIcon, Settings2Icon } from "lucide-react";
import {
    Page,
    PageBody,
    PageTopbar,
    SectionBar,
    Stat,
    StatStrip,
    TopbarAction,
} from "@/components/layout/Page";
import { comingSoon } from "@/lib/helper/comingSoon";

interface SamplePipeline {
    name: string;
    description: string;
    stages: Array<{ label: string; pct: number; color: string }>;
}

const SAMPLE_PIPELINES: SamplePipeline[] = [
    {
        name: "Outbound · Sales",
        description: "Standard cold outreach pipeline",
        stages: [
            { label: "Open", pct: 100, color: "bg-slate-400" },
            { label: "Engaged", pct: 38, color: "bg-sky-500" },
            { label: "Demo set", pct: 14, color: "bg-violet-500" },
            { label: "Quote sent", pct: 9, color: "bg-amber-500" },
            { label: "Won", pct: 5, color: "bg-emerald-500" },
        ],
    },
    {
        name: "Inbound · Trial",
        description: "Self-signup leads from the marketing site",
        stages: [
            { label: "Signed up", pct: 100, color: "bg-slate-400" },
            { label: "Activated", pct: 62, color: "bg-sky-500" },
            { label: "Paid", pct: 24, color: "bg-emerald-500" },
        ],
    },
];

export default function PipelinesPage() {
    return (
        <Page>
            <PageTopbar
                eyebrow="Pipelines"
                subtitle="Stages a deal moves through · custom per workflow"
            >
                <TopbarAction
                    icon={<PlusIcon className="w-3 h-3" />}
                    onClick={() => comingSoon("Pipelines")}
                >
                    New pipeline
                </TopbarAction>
            </PageTopbar>

            <StatStrip cols={4}>
                <Stat label="Pipelines" value={0} sub="defined" />
                <Stat label="Stages" value={0} sub="across pipelines" />
                <Stat label="Avg cycle" value="—d" sub="open → close" />
                <Stat label="Avg drop-off" value="—%" sub="stage to stage" last />
            </StatStrip>

            <SectionBar label="Flow preview" />
            <PageBody className="px-5 py-5">
                <div className="rounded-md border border-dashed border-slate-300 bg-slate-50/40 p-4 mb-4">
                    <p className="text-[12px] text-slate-700 leading-relaxed">
                        A pipeline is just a named sequence of stages. Each stage
                        shows the conversion rate from "Open" → that stage, so
                        you can see where deals fall out at a glance.
                    </p>
                </div>

                <div
                    aria-hidden
                    className="space-y-4 opacity-70 pointer-events-none select-none"
                >
                    {SAMPLE_PIPELINES.map((p, i) => (
                        <div key={i} className="rounded-md border border-slate-200 bg-white">
                            <div className="h-9 px-3 border-b border-slate-200 flex items-center gap-2">
                                <Settings2Icon className="w-3 h-3 text-slate-400" />
                                <span className="text-[12px] font-semibold text-slate-900">
                                    {p.name}
                                </span>
                                <span className="text-[11px] text-slate-500 truncate">
                                    · {p.description}
                                </span>
                            </div>
                            <div className="p-4 flex items-stretch gap-2 overflow-x-auto">
                                {p.stages.map((s, j) => (
                                    <div key={j} className="flex items-stretch gap-2">
                                        <div className="min-w-[120px] rounded-md border border-slate-200 bg-slate-50 px-2.5 py-2 flex flex-col gap-1.5">
                                            <div className="flex items-center gap-1.5">
                                                <span className={`size-1.5 rounded-full ${s.color}`} />
                                                <span className="text-[11.5px] font-medium text-slate-900">
                                                    {s.label}
                                                </span>
                                            </div>
                                            <div className="font-mono text-[10px] text-slate-400 tabular-nums">
                                                {s.pct}% reach this
                                            </div>
                                            <div className="h-1 rounded-full bg-slate-200 overflow-hidden">
                                                <div
                                                    className={`h-full ${s.color}`}
                                                    style={{ width: `${s.pct}%` }}
                                                />
                                            </div>
                                        </div>
                                        {j < p.stages.length - 1 && (
                                            <div className="flex items-center text-slate-300">
                                                <ArrowRightIcon className="w-3 h-3" />
                                            </div>
                                        )}
                                    </div>
                                ))}
                            </div>
                        </div>
                    ))}
                </div>
            </PageBody>
        </Page>
    );
}
