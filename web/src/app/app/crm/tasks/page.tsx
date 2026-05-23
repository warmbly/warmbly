// Tasks — grouped-by-due preview.
//
// Distinct from Deals (kanban) and Pipelines (ribbon) — tasks are a
// time-grouped checklist. The preview shows the same shape: Overdue
// → Today → Tomorrow → This week, with placeholder rows that hint at
// what each task looks like.

import { CalendarClockIcon, CheckSquareIcon, PlusIcon } from "lucide-react";
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

const SECTIONS: Array<{
    label: string;
    tone: "red" | "slate" | "sky" | "muted";
    items: Array<{ title: string; meta: string; contact?: string }>;
}> = [
    {
        label: "Overdue",
        tone: "red",
        items: [
            { title: "Follow up on Acme cold reply", meta: "3d late", contact: "tim@acme.test" },
        ],
    },
    {
        label: "Today",
        tone: "sky",
        items: [
            { title: "Reply to inbound · Vector demo", meta: "due 4pm", contact: "anna@vector.test" },
            { title: "Send revised quote to Sift", meta: "due 5pm", contact: "leo@sift.test" },
        ],
    },
    {
        label: "Tomorrow",
        tone: "slate",
        items: [
            { title: "Confirm meeting with Brae", meta: "10am", contact: "ana@brae.test" },
        ],
    },
    {
        label: "This week",
        tone: "muted",
        items: [
            { title: "Refresh Q1 sequence subject lines", meta: "Fri" },
            { title: "Review bounce list", meta: "Thu" },
            { title: "Onboard Mindroot · 2026", meta: "Wed" },
        ],
    },
];

const TONE = {
    red: { dot: "bg-red-500", label: "text-red-600" },
    sky: { dot: "bg-sky-500", label: "text-sky-600" },
    slate: { dot: "bg-slate-400", label: "text-slate-700" },
    muted: { dot: "bg-slate-300", label: "text-slate-500" },
} as const;

export default function TasksPage() {
    return (
        <Page>
            <PageTopbar
                eyebrow="Tasks"
                subtitle="Follow-ups and reminders · grouped by due date"
            >
                <TopbarAction
                    icon={<PlusIcon className="w-3 h-3" />}
                    onClick={() => comingSoon("Tasks")}
                >
                    New task
                </TopbarAction>
            </PageTopbar>

            <StatStrip cols={4}>
                <Stat label="Overdue" value={0} sub="needs attention" />
                <Stat label="Today" value={0} sub="due today" accent={false} />
                <Stat label="This week" value={0} sub="next 7 days" />
                <Stat label="Completed" value={0} sub="last 7 days" last />
            </StatStrip>

            <SectionBar label="Schedule preview" />
            <PageBody className="px-5 py-5">
                <div className="rounded-md border border-dashed border-slate-300 bg-slate-50/40 p-4 mb-4">
                    <p className="text-[12px] text-slate-700 leading-relaxed">
                        Tasks attach to a contact or a deal. They appear here
                        grouped by due-date so a single glance tells you what's
                        late, what's today, and what's coming.
                    </p>
                </div>

                <div
                    aria-hidden
                    className="space-y-4 opacity-70 pointer-events-none select-none"
                >
                    {SECTIONS.map((s) => (
                        <div key={s.label} className="rounded-md border border-slate-200 bg-white overflow-hidden">
                            <div className="h-8 px-3 border-b border-slate-200 flex items-center gap-1.5">
                                <span className={`size-1.5 rounded-full ${TONE[s.tone].dot}`} />
                                <span
                                    className={`text-[11px] uppercase tracking-[0.1em] font-semibold ${TONE[s.tone].label}`}
                                >
                                    {s.label}
                                </span>
                                <span className="ml-auto font-mono text-[10.5px] text-slate-400 tabular-nums">
                                    {s.items.length}
                                </span>
                            </div>
                            <div className="divide-y divide-slate-200/60">
                                {s.items.map((it, i) => (
                                    <div key={i} className="h-10 px-3 flex items-center gap-2.5">
                                        <CheckSquareIcon className="w-3.5 h-3.5 text-slate-300 shrink-0" />
                                        <span className="text-[12px] text-slate-900 truncate">
                                            {it.title}
                                        </span>
                                        {it.contact && (
                                            <span className="font-mono text-[10.5px] text-slate-400 truncate hidden sm:inline">
                                                · {it.contact}
                                            </span>
                                        )}
                                        <span className="ml-auto inline-flex items-center gap-1 font-mono text-[10.5px] text-slate-400 tabular-nums">
                                            <CalendarClockIcon className="w-2.5 h-2.5" />
                                            {it.meta}
                                        </span>
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
