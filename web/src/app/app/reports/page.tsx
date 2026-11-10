// Client reports: analytics a workspace shares outside itself, by link or on a schedule.

import React from "react";
import { useNavigate } from "@tanstack/react-router";
import { CalendarClockIcon, FileBarChartIcon, Link2Icon, MoreHorizontalIcon, PlusIcon } from "lucide-react";
import toast from "react-hot-toast/headless";

import { EmptyBlock, Page, PageBody, PageTopbar, SectionBar, TopbarAction } from "@/components/layout/Page";
import { NoAccess } from "@/components/layout/NoAccess";
import { SearchInput } from "@/components/ui/field";
import { PopoverMenu, PopoverMenuContent, PopoverMenuItem, PopoverMenuSeparator, PopoverMenuTrigger } from "@/components/ui/popover-menu";
import { useConfirm } from "@/hooks/context/confirm";
import { usePermission, useWriteGuard } from "@/hooks/usePermission";
import { useDeleteReport, useReports } from "@/lib/api/hooks/app/reports";
import type ClientReport from "@/lib/api/models/app/reports/ClientReport";
import type { AppError } from "@/lib/api/client/normalizeError";
import buildError from "@/lib/helper/buildError";
import timeAgo from "@/lib/helper/timeAgo";
import { cn } from "@/lib/utils";
import ReportDialog from "@/components/app/reports/ReportDialog";
import { linkExpiry, linkState, rangeLabel, scheduleSummary, scopeSummary } from "@/components/app/reports/reportText";

const LINK_PILL = {
    shared: { label: "Shared", cls: "bg-emerald-50 text-emerald-700" },
    expired: { label: "Link expired", cls: "bg-amber-50 text-amber-700" },
    off: { label: "Not shared", cls: "bg-slate-100 text-slate-500" },
} as const;

export default function ReportsPage() {
    const canView = usePermission("VIEW_ANALYTICS");
    if (!canView) return <NoAccess feature="client reports" permissionLabel="View analytics" />;
    return <ReportsList />;
}

function ReportsList() {
    const navigate = useNavigate();
    const confirm = useConfirm();
    const write = useWriteGuard("MANAGE_CAMPAIGNS");
    const reports = useReports();
    const remove = useDeleteReport();
    const [query, setQuery] = React.useState("");
    const [dialog, setDialog] = React.useState<{ report: ClientReport | null } | null>(null);

    const list = React.useMemo(() => {
        const q = query.trim().toLowerCase();
        return q ? reports.reports.filter((r) => r.name.toLowerCase().includes(q)) : reports.reports;
    }, [reports.reports, query]);

    const open = (r: ClientReport, tab?: "share" | "schedule") =>
        navigate({ to: "/app/reports/$id", params: { id: r.id }, search: tab ? { tab } : {} });

    const guarded = (fn: () => void) => () => write.guard(fn)({});
    const create = guarded(() => setDialog({ report: null }));

    function askDelete(r: ClientReport) {
        confirm.show(`Delete the report "${r.name}"? Its link and scheduled emails stop working.`, async () => {
            try {
                await remove.mutateAsync(r.id);
                toast.success("Report deleted");
            } catch (err) {
                toast.error(buildError(err as AppError));
            }
        });
    }

    return (
        <Page>
            <PageTopbar eyebrow="Reports" subtitle="Share campaign results with clients by link or by email">
                <TopbarAction icon={<PlusIcon className="w-3 h-3" />} onClick={create}>
                    New report
                </TopbarAction>
            </PageTopbar>

            <SectionBar label="All reports" count={list.length}>
                <SearchInput value={query} onChange={setQuery} placeholder="Search reports…" className="w-full sm:w-56" />
            </SectionBar>

            <PageBody>
                {reports.isPending ? (
                    <div className="p-3 space-y-1.5">
                        {[...Array(4)].map((_, i) => (
                            <div key={i} className="h-12 rounded-md bg-slate-100 animate-pulse" />
                        ))}
                    </div>
                ) : reports.isError ? (
                    <EmptyBlock title="Couldn't load reports" body="Try again in a moment." />
                ) : list.length === 0 ? (
                    <EmptyBlock
                        title={query ? "No reports match" : "No reports yet"}
                        body={
                            query
                                ? "Try a different search."
                                : "A report shows a client how their campaigns are doing: share it as a link or email it every week or month."
                        }
                        cta={
                            query ? undefined : (
                                <TopbarAction icon={<PlusIcon className="w-3 h-3" />} onClick={create}>
                                    New report
                                </TopbarAction>
                            )
                        }
                    />
                ) : (
                    <table className="w-full text-left">
                        <thead className="sticky top-0 bg-white z-[1]">
                            <tr className="border-b border-slate-200">
                                <Th className="pl-5 max-w-0 w-full md:max-w-none md:w-auto">Report</Th>
                                <Th className="w-36 hidden md:table-cell">Covers</Th>
                                <Th className="w-32">Link</Th>
                                <Th className="w-56 hidden lg:table-cell">Schedule</Th>
                                <Th className="w-24 hidden xl:table-cell">Updated</Th>
                                <th className="w-12" />
                            </tr>
                        </thead>
                        <tbody>
                            {list.map((r) => {
                                const state = linkState(r);
                                const pill = LINK_PILL[state];
                                return (
                                    <tr
                                        key={r.id}
                                        onClick={() => open(r)}
                                        className="h-12 border-b border-slate-100 cursor-pointer transition-colors hover:bg-slate-50/80"
                                    >
                                        <td className="pl-5 pr-3 max-w-0 w-full md:max-w-none md:w-auto">
                                            <div className="flex items-center gap-2.5 min-w-0">
                                                <div className="w-6 h-6 rounded-md bg-slate-100 flex items-center justify-center shrink-0">
                                                    <FileBarChartIcon className="w-3 h-3 text-slate-500" />
                                                </div>
                                                <div className="min-w-0">
                                                    <div className="text-[12.5px] font-medium text-slate-900 truncate leading-tight">{r.name}</div>
                                                    <div className="text-[11px] text-slate-500 truncate">
                                                        {rangeLabel(r.date_range)}
                                                        <span className="md:hidden"> · {scopeSummary(r)}</span>
                                                    </div>
                                                </div>
                                            </div>
                                        </td>
                                        <td className="px-3 text-[12px] text-slate-600 whitespace-nowrap hidden md:table-cell">{scopeSummary(r)}</td>
                                        <td className="px-3">
                                            <span
                                                className={cn("inline-flex items-center gap-1 h-5 px-1.5 rounded text-[10.5px] font-medium whitespace-nowrap", pill.cls)}
                                                title={linkExpiry(r) || undefined}
                                            >
                                                {state !== "off" && <Link2Icon className="w-2.5 h-2.5" />}
                                                {pill.label}
                                            </span>
                                        </td>
                                        <td className="px-3 text-[12px] text-slate-600 hidden lg:table-cell">
                                            {r.schedule ? (
                                                <span className="inline-flex items-center gap-1.5 min-w-0">
                                                    <CalendarClockIcon className={cn("w-3 h-3 shrink-0", r.schedule.paused ? "text-amber-500" : "text-slate-400")} />
                                                    <span className="truncate">
                                                        {r.schedule.paused ? "Paused · " : ""}
                                                        {scheduleSummary(r.schedule)}
                                                    </span>
                                                </span>
                                            ) : (
                                                <span className="text-slate-300">Not scheduled</span>
                                            )}
                                        </td>
                                        <td className="px-3 text-[12px] text-slate-500 whitespace-nowrap hidden xl:table-cell">{timeAgo(r.updated_at)}</td>
                                        <td className="pr-3" onClick={(e) => e.stopPropagation()}>
                                            <PopoverMenu align="end">
                                                <PopoverMenuTrigger asChild>
                                                    <button
                                                        type="button"
                                                        aria-label="More"
                                                        className="size-7 rounded-md text-slate-400 hover:text-slate-900 hover:bg-slate-100 inline-flex items-center justify-center transition-colors"
                                                    >
                                                        <MoreHorizontalIcon className="w-3.5 h-3.5" />
                                                    </button>
                                                </PopoverMenuTrigger>
                                                <PopoverMenuContent minWidth={180}>
                                                    <PopoverMenuItem onSelect={() => open(r)}>Open preview</PopoverMenuItem>
                                                    <PopoverMenuItem onSelect={() => open(r, "share")}>Share link</PopoverMenuItem>
                                                    <PopoverMenuItem onSelect={() => open(r, "schedule")}>Schedule email</PopoverMenuItem>
                                                    <PopoverMenuItem onSelect={guarded(() => setDialog({ report: r }))}>Edit</PopoverMenuItem>
                                                    <PopoverMenuSeparator />
                                                    <PopoverMenuItem danger onSelect={guarded(() => askDelete(r))}>
                                                        Delete
                                                    </PopoverMenuItem>
                                                </PopoverMenuContent>
                                            </PopoverMenu>
                                        </td>
                                    </tr>
                                );
                            })}
                        </tbody>
                    </table>
                )}
            </PageBody>

            {dialog && (
                <ReportDialog
                    report={dialog.report}
                    onClose={() => setDialog(null)}
                    onCreated={(r) => navigate({ to: "/app/reports/$id", params: { id: r.id } })}
                />
            )}
        </Page>
    );
}

function Th({ children, className }: { children?: React.ReactNode; className?: string }) {
    return <th className={`px-3 py-2 text-[10px] font-medium text-slate-400 uppercase tracking-[0.14em] ${className ?? ""}`}>{children}</th>;
}
