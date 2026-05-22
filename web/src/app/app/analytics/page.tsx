import {
    AlertTriangleIcon,
    ArrowUpRightIcon,
    BarChart3Icon,
    MailIcon,
    MousePointerClickIcon,
    ReplyIcon,
    TrendingUpIcon,
} from "lucide-react";
import { EmptyState, Page, PageHeader, StatCard } from "@/components/layout/Page";

const stats: Array<{
    label: string;
    value: string;
    icon: React.ReactNode;
    tone: "blue" | "emerald" | "violet" | "amber";
}> = [
    { label: "Total sent", value: "--", icon: <MailIcon className="w-4 h-4" />, tone: "blue" },
    { label: "Open rate", value: "--%", icon: <MousePointerClickIcon className="w-4 h-4" />, tone: "emerald" },
    { label: "Reply rate", value: "--%", icon: <ReplyIcon className="w-4 h-4" />, tone: "violet" },
    { label: "Bounce rate", value: "--%", icon: <AlertTriangleIcon className="w-4 h-4" />, tone: "amber" },
];

const quickStats = [
    { label: "Delivered", dot: "bg-emerald-500" },
    { label: "Opened", dot: "bg-sky-500" },
    { label: "Replied", dot: "bg-violet-500" },
    { label: "Bounced", dot: "bg-amber-500" },
    { label: "Spam", dot: "bg-red-500" },
];

export default function AnalyticsPage() {
    return (
        <Page width="wide">
            <PageHeader
                title="Analytics"
                subtitle="Deliverability and engagement across the workspace."
            />

            <div className="grid gap-3 grid-cols-2 lg:grid-cols-4 mb-6">
                {stats.map((s) => (
                    <StatCard
                        key={s.label}
                        icon={s.icon}
                        iconTone={s.tone}
                        label={s.label}
                        value={s.value}
                    />
                ))}
            </div>

            <div className="grid lg:grid-cols-3 gap-4 mb-4">
                <div className="lg:col-span-2 bg-white rounded-xl border border-slate-200/80 p-5">
                    <div className="flex items-center justify-between mb-6">
                        <div>
                            <h2 className="text-[13.5px] font-semibold text-slate-900">Email performance</h2>
                            <p className="text-xs text-slate-400 mt-0.5">Daily sends and engagement over time</p>
                        </div>
                        <div className="flex items-center gap-1.5">
                            <button className="text-xs text-slate-700 bg-slate-100 rounded-md px-2.5 py-1 font-medium">7 days</button>
                            <button className="text-xs text-slate-400 hover:text-slate-700 rounded-md px-2.5 py-1 transition-colors">30 days</button>
                            <button className="text-xs text-slate-400 hover:text-slate-700 rounded-md px-2.5 py-1 transition-colors">90 days</button>
                        </div>
                    </div>
                    <div className="h-52 flex items-end gap-1 px-2">
                        {[...Array(28)].map((_, i) => (
                            <div key={i} className="flex-1 flex flex-col items-center gap-0.5">
                                <div
                                    className="w-full rounded-sm bg-sky-100"
                                    style={{ height: `${15 + Math.sin(i * 0.5) * 30 + Math.random() * 20}%` }}
                                />
                            </div>
                        ))}
                    </div>
                    <div className="flex justify-between mt-2 px-2">
                        <span className="text-[10px] text-slate-300">7 days ago</span>
                        <span className="text-[10px] text-slate-300">Today</span>
                    </div>
                </div>

                <div className="bg-white rounded-xl border border-slate-200/80 p-5">
                    <h2 className="text-[13.5px] font-semibold text-slate-900 mb-4">Quick stats</h2>
                    <div className="space-y-3">
                        {quickStats.map((q) => (
                            <div key={q.label} className="flex items-center justify-between">
                                <div className="flex items-center gap-2">
                                    <div className={`w-2 h-2 rounded-full ${q.dot}`} />
                                    <span className="text-[13px] text-slate-600">{q.label}</span>
                                </div>
                                <span className="text-[13px] font-medium text-slate-900 tabular-nums">--</span>
                            </div>
                        ))}
                    </div>
                    <div className="pt-3 mt-3 border-t border-slate-100 flex items-center gap-1.5 text-xs text-slate-400">
                        <TrendingUpIcon className="w-3.5 h-3.5" />
                        <span>Start sending to see analytics</span>
                    </div>
                </div>
            </div>

            <div className="bg-white rounded-xl border border-slate-200/80 p-5">
                <div className="flex items-center justify-between mb-4">
                    <h2 className="text-[13.5px] font-semibold text-slate-900">Warmup overview</h2>
                    <button className="flex items-center gap-1 text-xs text-slate-400 hover:text-slate-700 transition-colors">
                        <span>View all accounts</span>
                        <ArrowUpRightIcon className="w-3 h-3" />
                    </button>
                </div>
                <EmptyState
                    icon={<BarChart3Icon className="w-5 h-5" />}
                    title="Nothing to chart yet"
                    description="Once mailboxes start warming, daily progress will appear here."
                />
            </div>
        </Page>
    );
}
