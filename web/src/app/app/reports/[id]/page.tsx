// One client report: its in-dashboard preview, share link and email schedule.

import React from "react";
import { Link, useParams } from "@tanstack/react-router";
import { ArrowLeftIcon, CalendarClockIcon, EyeIcon, GlobeIcon, Link2Icon, LockIcon, PencilIcon } from "lucide-react";

import { EmptyBlock } from "@/components/layout/Page";
import { NoAccess } from "@/components/layout/NoAccess";
import ScrollStrip from "@/components/ui/scroll-strip";
import { Segmented } from "@/components/app/campaigns/preferences/components/CampaignPreferenceBoolBox";
import ResourceViewers from "@/components/app/presence/ResourceViewers";
import ReportDialog from "@/components/app/reports/ReportDialog";
import ReportPreview from "@/components/app/reports/ReportPreview";
import ReportSchedulePanel from "@/components/app/reports/ReportSchedulePanel";
import ReportSharePanel from "@/components/app/reports/ReportSharePanel";
import { dayRange, formatDateTime, linkState, RANGE_OPTIONS, scopeSummary } from "@/components/app/reports/reportText";
import { usePresenceResource } from "@/hooks/PresenceProvider";
import { usePermission, useWriteGuard } from "@/hooks/usePermission";
import { useSearchParams } from "@/hooks/useSearchParams";
import { useReport, useReportPreview } from "@/lib/api/hooks/app/reports";
import type ClientReport from "@/lib/api/models/app/reports/ClientReport";
import type { ClientReportRange } from "@/lib/api/models/app/reports/ClientReport";
import { cn } from "@/lib/utils";

type Tab = "preview" | "share" | "schedule";

const TABS: { key: Tab; label: string; icon: React.ComponentType<{ className?: string }> }[] = [
    { key: "preview", label: "Preview", icon: EyeIcon },
    { key: "share", label: "Share", icon: Link2Icon },
    { key: "schedule", label: "Schedule", icon: CalendarClockIcon },
];

export default function ReportDetailPage() {
    const canView = usePermission("VIEW_ANALYTICS");
    const { id } = useParams({ from: "/app/reports/$id" });
    const report = useReport(canView ? id : undefined);

    if (!canView) return <NoAccess feature="client reports" permissionLabel="View analytics" />;
    if (report.isPending) {
        return (
            <div className="p-6 space-y-3">
                <div className="h-8 w-64 rounded-md bg-slate-100 animate-pulse" />
                <div className="h-96 rounded-md bg-slate-100 animate-pulse" />
            </div>
        );
    }
    if (report.isError || !report.data) {
        return (
            <div className="flex flex-col">
                <BackLink />
                <EmptyBlock title="Report not found" body="It may have been deleted by a teammate." />
            </div>
        );
    }
    return <ReportDetail key={report.data.id} report={report.data} />;
}

function BackLink() {
    return (
        <div className="px-3 sm:px-5 pt-3 sm:pt-4">
            <Link
                to="/app/reports"
                className="inline-flex items-center gap-1 h-6 -ml-1.5 px-1.5 rounded-md text-[11.5px] text-slate-500 hover:text-slate-900 hover:bg-slate-100 transition-colors"
            >
                <ArrowLeftIcon className="w-3 h-3" />
                Reports
            </Link>
        </div>
    );
}

function ReportDetail({ report }: { report: ClientReport }) {
    const write = useWriteGuard("MANAGE_CAMPAIGNS");
    usePresenceResource(`report:${report.id}`);
    const [params, setParams] = useSearchParams();
    const rawTab = params.get("tab");
    const tab: Tab = TABS.some((t) => t.key === rawTab) ? (rawTab as Tab) : "preview";
    const rawRange = params.get("range");
    const picked = RANGE_OPTIONS.some((o) => o.value === rawRange) ? (rawRange as ClientReportRange) : null;
    // The report's own range asks for the default, so it shares the loader's cache entry.
    const range = picked && picked !== report.date_range ? picked : undefined;
    const preview = useReportPreview(tab === "preview" ? report.id : undefined, range);
    const [editing, setEditing] = React.useState(false);

    const setParam = (key: string, value: string | null) =>
        setParams(
            (p) => {
                const next = new URLSearchParams(p);
                if (value) next.set(key, value);
                else next.delete(key);
                return next;
            },
            { replace: true },
        );

    const restricted = report.access?.scope === "restricted";
    const state = linkState(report);

    return (
        <div className="flex flex-col min-h-full bg-white">
            <div className="px-3 sm:px-5 pt-3 sm:pt-4 pb-3 flex items-start gap-3">
                <div className="min-w-0">
                    <Link
                        to="/app/reports"
                        className="inline-flex items-center gap-1 h-6 -ml-1.5 px-1.5 mb-1 rounded-md text-[11.5px] text-slate-500 hover:text-slate-900 hover:bg-slate-100 transition-colors"
                    >
                        <ArrowLeftIcon className="w-3 h-3" />
                        Reports
                    </Link>
                    <div className="flex flex-wrap items-center gap-x-2 gap-y-1">
                        <h1 className="min-w-0 max-w-full text-[18px] font-semibold text-slate-900 truncate">{report.name}</h1>
                        <span
                            className={cn(
                                "shrink-0 inline-flex items-center gap-1 h-5 px-1.5 rounded border text-[11px] font-medium",
                                restricted ? "bg-amber-50 text-amber-800 border-amber-100" : "bg-slate-50 text-slate-600 border-slate-200",
                            )}
                        >
                            {restricted ? <LockIcon className="w-2.5 h-2.5" /> : <GlobeIcon className="w-2.5 h-2.5" />}
                            {scopeSummary(report)}
                        </span>
                        <ResourceViewers resource={`report:${report.id}`} className="shrink-0" />
                    </div>
                </div>
                <div className="ml-auto shrink-0 flex items-center gap-1.5">
                    <button
                        type="button"
                        onClick={write.guard(() => setEditing(true))}
                        className="h-7 px-2.5 rounded-md border border-slate-200 hover:border-slate-300 bg-white text-slate-700 text-[12px] font-medium inline-flex items-center gap-1.5 transition-colors"
                    >
                        <PencilIcon className="w-3 h-3" />
                        Edit
                    </button>
                </div>
            </div>

            <ScrollStrip activeKey={tab} className="shrink-0 border-b border-slate-200" innerClassName="px-3 flex items-center gap-1">
                {TABS.map((t) => {
                    const active = t.key === tab;
                    const Icon = t.icon;
                    const badge =
                        t.key === "share" ? (state === "shared" ? "On" : state === "expired" ? "Expired" : null) : t.key === "schedule" && report.schedule ? (report.schedule.paused ? "Paused" : "On") : null;
                    return (
                        <button
                            key={t.key}
                            type="button"
                            data-active={active ? "true" : undefined}
                            onClick={() => setParam("tab", t.key === "preview" ? null : t.key)}
                            className={cn(
                                "relative h-10 px-2.5 inline-flex items-center gap-1.5 text-[12.5px] shrink-0 transition-colors",
                                active ? "text-slate-900 font-medium" : "text-slate-500 hover:text-slate-900",
                            )}
                        >
                            <Icon className="w-3.5 h-3.5" />
                            {t.label}
                            {badge && <span className="text-[10.5px] text-slate-400 font-normal">{badge}</span>}
                            {active && <span className="absolute left-2 right-2 -bottom-px h-0.5 bg-sky-600 rounded-full" />}
                        </button>
                    );
                })}
            </ScrollStrip>

            <div className="flex-1 min-h-0 p-3 sm:p-5">
                {tab === "preview" && (
                    <div className="flex flex-col gap-3">
                        <div className="flex flex-wrap items-center gap-2">
                            <Segmented
                                value={picked ?? report.date_range}
                                onChange={(v) => setParam("range", v === report.date_range ? null : v)}
                                options={RANGE_OPTIONS}
                                className="flex-wrap"
                            />
                            {preview.data && (
                                <span className="text-[11.5px] text-slate-500 tabular-nums">
                                    {dayRange(preview.data.from, preview.data.to)}
                                    <span className="hidden sm:inline text-slate-400"> · generated {formatDateTime(preview.data.generated_at)}</span>
                                </span>
                            )}
                            <span className="ml-auto text-[11px] text-slate-400">
                                {range ? "Previewing another range. The shared page uses the report's default." : "What a client sees on the shared page."}
                            </span>
                        </div>
                        {preview.isPending ? (
                            <div className="space-y-3">
                                <div className="h-24 rounded-md bg-slate-100 animate-pulse" />
                                <div className="h-64 rounded-md bg-slate-100 animate-pulse" />
                            </div>
                        ) : preview.isError || !preview.data ? (
                            <EmptyBlock title="Couldn't load the preview" body="Try again in a moment." />
                        ) : (
                            <div className={cn("transition-opacity", preview.isPlaceholderData && "opacity-60")}>
                                <ReportPreview view={preview.data} />
                            </div>
                        )}
                    </div>
                )}
                {/* Kept mounted so a freshly minted link and an unsaved schedule survive a tab switch. */}
                <div hidden={tab !== "share"}>
                    <ReportSharePanel report={report} write={write} />
                </div>
                <div hidden={tab !== "schedule"}>
                    <ReportSchedulePanel report={report} write={write} />
                </div>
            </div>

            {editing && <ReportDialog report={report} onClose={() => setEditing(false)} />}
        </div>
    );
}
