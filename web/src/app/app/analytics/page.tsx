import { ArrowUpRightIcon } from "lucide-react";
import {
    EmptyBlock,
    Page,
    PageBody,
    PageTopbar,
    SectionBar,
    Stat,
    StatStrip,
} from "@/components/layout/Page";
import { useState } from "react";

const stats = [
    { label: "Total sent", value: "—", sub: "last 7 days" },
    { label: "Open rate", value: "—%", sub: "after delivery" },
    { label: "Reply rate", value: "—%", sub: "incl. positive" },
    { label: "Bounce rate", value: "—%", sub: "hard + soft" },
];

const breakdown = [
    { label: "Delivered", dot: "bg-emerald-500" },
    { label: "Opened", dot: "bg-sky-500" },
    { label: "Replied", dot: "bg-violet-500" },
    { label: "Bounced", dot: "bg-amber-500" },
    { label: "Spam", dot: "bg-red-500" },
];

export default function AnalyticsPage() {
    const [range, setRange] = useState<"7d" | "30d" | "90d">("7d");
    return (
        <Page>
            <PageTopbar
                eyebrow="Analytics"
                subtitle="Deliverability across the workspace"
            >
                <RangeTabs value={range} onChange={setRange} />
            </PageTopbar>

            <StatStrip cols={4}>
                {stats.map((s, i) => (
                    <Stat
                        key={s.label}
                        label={s.label}
                        value={s.value}
                        sub={s.sub}
                        last={i === stats.length - 1}
                    />
                ))}
            </StatStrip>

            <div className="grid lg:grid-cols-[1fr_320px] min-h-0 flex-1">
                <section className="flex flex-col min-h-0 lg:border-r lg:border-slate-200">
                    <SectionBar label="Email performance" />
                    <div className="flex-1 flex flex-col px-5 py-4">
                        <div className="h-52 flex items-end gap-0.5">
                            {[...Array(28)].map((_, i) => (
                                <div key={i} className="flex-1 flex items-end">
                                    <div
                                        className="w-full rounded-sm bg-sky-100"
                                        style={{ height: `${15 + Math.sin(i * 0.5) * 30 + Math.random() * 20}%` }}
                                    />
                                </div>
                            ))}
                        </div>
                        <div className="flex justify-between mt-2 font-mono text-[10px] text-slate-400">
                            <span>7d ago</span>
                            <span>today</span>
                        </div>
                    </div>
                </section>

                <aside className="flex flex-col min-h-0 bg-slate-50/40">
                    <SectionBar label="Breakdown" />
                    <div className="divide-y divide-slate-200/60">
                        {breakdown.map((q) => (
                            <div
                                key={q.label}
                                className="h-9 px-4 flex items-center gap-2"
                            >
                                <span className={`size-1.5 rounded-full ${q.dot}`} />
                                <span className="text-[12px] text-slate-700">{q.label}</span>
                                <span className="ml-auto font-mono text-[11px] text-slate-500 tabular-nums">
                                    —
                                </span>
                            </div>
                        ))}
                    </div>
                </aside>
            </div>

            <SectionBar label="Warmup">
                <a
                    href="/app/emails"
                    className="inline-flex items-center gap-1 text-[11px] text-slate-500 hover:text-slate-900 transition-colors"
                >
                    All accounts
                    <ArrowUpRightIcon className="w-3 h-3" />
                </a>
            </SectionBar>
            <PageBody>
                <EmptyBlock
                    title="Nothing to chart yet"
                    body="Once mailboxes start warming, daily progress will appear here."
                />
            </PageBody>
        </Page>
    );
}

function RangeTabs({ value, onChange }: { value: "7d" | "30d" | "90d"; onChange: (v: "7d" | "30d" | "90d") => void }) {
    const opts: ("7d" | "30d" | "90d")[] = ["7d", "30d", "90d"];
    return (
        <div className="inline-flex items-center gap-0.5 rounded-md border border-slate-200 bg-white p-0.5">
            {opts.map((o) => (
                <button
                    key={o}
                    onClick={() => onChange(o)}
                    className={`h-6 px-2 rounded text-[11px] font-medium tabular-nums transition-colors ${
                        value === o
                            ? "bg-slate-900 text-white"
                            : "text-slate-500 hover:text-slate-900"
                    }`}
                >
                    {o}
                </button>
            ))}
        </div>
    );
}
