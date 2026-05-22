import {
    AlertTriangleIcon,
    ArrowUpRightIcon,
    BarChart3Icon,
    MailIcon,
    MousePointerClickIcon,
    ReplyIcon,
    TrendingUpIcon,
} from "lucide-react";
import { EmptyState, Page, PageHeader, StatCard, StatRow } from "@/components/layout/Page";

const stats: Array<{
    label: string;
    value: string;
    icon: React.ReactNode;
}> = [
    { label: "Total sent", value: "--", icon: <MailIcon className="w-3 h-3" /> },
    { label: "Open rate", value: "--%", icon: <MousePointerClickIcon className="w-3 h-3" /> },
    { label: "Reply rate", value: "--%", icon: <ReplyIcon className="w-3 h-3" /> },
    { label: "Bounce rate", value: "--%", icon: <AlertTriangleIcon className="w-3 h-3" /> },
];

const quickStats = [
    { label: "Delivered", dot: "bg-emerald-500" },
    { label: "Opened", dot: "bg-slate-700" },
    { label: "Replied", dot: "bg-slate-500" },
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

            <StatRow>
                {stats.map((s) => (
                    <StatCard key={s.label} icon={s.icon} label={s.label} value={s.value} />
                ))}
            </StatRow>

            <div className="grid lg:grid-cols-3 gap-3 mb-3">
                <div className="lg:col-span-2 bg-white rounded-md border border-slate-200 p-3">
                    <div className="flex items-center justify-between mb-3">
                        <h2 className="text-[12.5px] font-semibold text-slate-700">Email performance</h2>
                        <div className="flex items-center gap-0.5">
                            <button className="text-[11.5px] text-slate-900 bg-slate-100 rounded px-1.5 h-6 font-medium">7d</button>
                            <button className="text-[11.5px] text-slate-500 hover:text-slate-900 hover:bg-slate-50 rounded px-1.5 h-6 transition-colors">30d</button>
                            <button className="text-[11.5px] text-slate-500 hover:text-slate-900 hover:bg-slate-50 rounded px-1.5 h-6 transition-colors">90d</button>
                        </div>
                    </div>
                    <div className="h-40 flex items-end gap-0.5">
                        {[...Array(28)].map((_, i) => (
                            <div key={i} className="flex-1 flex flex-col items-center">
                                <div
                                    className="w-full rounded-sm bg-slate-200"
                                    style={{ height: `${15 + Math.sin(i * 0.5) * 30 + Math.random() * 20}%` }}
                                />
                            </div>
                        ))}
                    </div>
                    <div className="flex justify-between mt-1.5">
                        <span className="text-[10px] text-slate-400">7d ago</span>
                        <span className="text-[10px] text-slate-400">Today</span>
                    </div>
                </div>

                <div className="bg-white rounded-md border border-slate-200 p-3">
                    <h2 className="text-[12.5px] font-semibold text-slate-700 mb-2.5">Breakdown</h2>
                    <div className="space-y-1.5">
                        {quickStats.map((q) => (
                            <div key={q.label} className="flex items-center justify-between">
                                <div className="flex items-center gap-1.5">
                                    <div className={`w-1.5 h-1.5 rounded-full ${q.dot}`} />
                                    <span className="text-[12px] text-slate-600">{q.label}</span>
                                </div>
                                <span className="text-[12px] font-medium text-slate-900 tabular-nums">--</span>
                            </div>
                        ))}
                    </div>
                    <div className="pt-2 mt-2.5 border-t border-slate-100 flex items-center gap-1.5 text-[11px] text-slate-400">
                        <TrendingUpIcon className="w-3 h-3" />
                        <span>Start sending to see data</span>
                    </div>
                </div>
            </div>

            <div className="bg-white rounded-md border border-slate-200 p-3">
                <div className="flex items-center justify-between mb-3 h-6">
                    <h2 className="text-[12.5px] font-semibold text-slate-700">Warmup overview</h2>
                    <button className="flex items-center gap-1 text-[11.5px] text-slate-500 hover:text-slate-900 transition-colors">
                        <span>All accounts</span>
                        <ArrowUpRightIcon className="w-3 h-3" />
                    </button>
                </div>
                <EmptyState
                    icon={<BarChart3Icon className="w-4 h-4" />}
                    title="Nothing to chart yet"
                    description="Once mailboxes start warming, daily progress appears here."
                />
            </div>
        </Page>
    );
}
