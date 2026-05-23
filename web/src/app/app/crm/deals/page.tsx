// Deals — kanban-board preview.
//
// The page renders 4 stage columns with greyed-out placeholder cards.
// Visually distinct from any other tab (no other surface is column-
// oriented) so the user immediately sees "this is a pipeline view"
// even before they have data.

import { CalendarIcon, CircleDollarSignIcon, PlusIcon } from "lucide-react";
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

const STAGES = [
    { id: "open", label: "Open", color: "bg-slate-400", count: 0 },
    { id: "qualified", label: "Qualified", color: "bg-sky-500", count: 0 },
    { id: "negotiation", label: "Negotiation", color: "bg-amber-500", count: 0 },
    { id: "won", label: "Closed · Won", color: "bg-emerald-500", count: 0 },
];

const SAMPLE_DEALS: Array<{
    stage: string;
    title: string;
    company: string;
    amount: string;
    nextStep: string;
}> = [
    { stage: "open", title: "Q1 outbound · Acme", company: "Acme Inc.", amount: "$12,000", nextStep: "Send intro" },
    { stage: "open", title: "Cold list · Brae", company: "Brae", amount: "$4,500", nextStep: "Verify list" },
    { stage: "qualified", title: "Demo · Vector", company: "Vector Labs", amount: "$24,000", nextStep: "Demo Thu" },
    { stage: "negotiation", title: "Renewal · Sift", company: "Sift", amount: "$48,000", nextStep: "Send revised quote" },
    { stage: "won", title: "Mindroot · 2026", company: "Mindroot Ltd", amount: "$36,000", nextStep: "Onboard" },
];

export default function DealsPage() {
    return (
        <Page>
            <PageTopbar
                eyebrow="Deals"
                subtitle="Opportunities by stage · drag to advance"
            >
                <TopbarAction
                    icon={<PlusIcon className="w-3 h-3" />}
                    onClick={() => comingSoon("Deals")}
                >
                    New deal
                </TopbarAction>
            </PageTopbar>

            <StatStrip cols={4}>
                <Stat label="Open" value={0} sub="active deals" />
                <Stat label="Pipeline value" value="—" sub="weighted" />
                <Stat label="Closed (mo)" value={0} sub="won this month" />
                <Stat label="Win rate" value="—%" sub="last 90d" last />
            </StatStrip>

            <SectionBar label="Board preview" />
            <PageBody className="px-5 py-5">
                <div className="rounded-md border border-dashed border-slate-300 bg-slate-50/40 p-4 mb-4">
                    <p className="text-[12px] text-slate-700 leading-relaxed">
                        Deals will render as draggable cards across these stages.
                        Each card shows the company, the amount, and the next
                        step so the board doubles as your todo list.
                    </p>
                </div>

                <div
                    aria-hidden
                    className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-4 gap-3 opacity-70 pointer-events-none select-none"
                >
                    {STAGES.map((stage) => (
                        <div key={stage.id} className="flex flex-col rounded-md bg-slate-50 border border-slate-200 min-h-[280px]">
                            <div className="h-9 px-3 flex items-center gap-2 border-b border-slate-200">
                                <span className={`size-1.5 rounded-full ${stage.color}`} />
                                <span className="text-[11px] uppercase tracking-[0.1em] font-semibold text-slate-700">
                                    {stage.label}
                                </span>
                                <span className="ml-auto font-mono text-[10.5px] text-slate-400 tabular-nums">
                                    {SAMPLE_DEALS.filter((d) => d.stage === stage.id).length}
                                </span>
                            </div>
                            <div className="p-2 space-y-2 flex-1">
                                {SAMPLE_DEALS.filter((d) => d.stage === stage.id).map((d, i) => (
                                    <div
                                        key={i}
                                        className="rounded-md bg-white border border-slate-200 px-2.5 py-2"
                                    >
                                        <div className="text-[12px] font-medium text-slate-900 truncate">
                                            {d.title}
                                        </div>
                                        <div className="text-[10.5px] text-slate-500 truncate mt-0.5">
                                            {d.company}
                                        </div>
                                        <div className="flex items-center gap-2 mt-2 text-[10.5px]">
                                            <span className="inline-flex items-center gap-1 text-emerald-600 font-mono tabular-nums">
                                                <CircleDollarSignIcon className="w-2.5 h-2.5" />
                                                {d.amount}
                                            </span>
                                            <span className="inline-flex items-center gap-1 text-slate-400 ml-auto">
                                                <CalendarIcon className="w-2.5 h-2.5" />
                                                {d.nextStep}
                                            </span>
                                        </div>
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
