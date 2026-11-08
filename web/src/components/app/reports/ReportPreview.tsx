// The report as a client sees it, rendered from GET /reports/:id/preview.

import React from "react";

import { SectionBar, Stat, StatStrip } from "@/components/layout/Page";
import { MultiTrend, type TrendSeries } from "@/components/ui/charts";
import type { ClientReportReplyClass, ClientReportView } from "@/lib/api/models/app/reports/ClientReport";
import { formatDateTime } from "./reportText";

const pct = (n: number | undefined) => `${(n ?? 0).toFixed(1)}%`;
const num = (n: number | undefined) => (n ?? 0).toLocaleString();

const REPLY_PILL: Record<ClientReportReplyClass, { label: string; cls: string }> = {
    positive: { label: "Positive", cls: "bg-emerald-50 text-emerald-700" },
    neutral: { label: "Neutral", cls: "bg-slate-100 text-slate-600" },
    negative: { label: "Negative", cls: "bg-rose-50 text-rose-700" },
    unsubscribe: { label: "Unsubscribe", cls: "bg-amber-50 text-amber-700" },
    unknown: { label: "Other", cls: "bg-slate-100 text-slate-500" },
    unclassified: { label: "Unclassified", cls: "bg-slate-100 text-slate-500" },
};

export default function ReportPreview({ view }: { view: ClientReportView }) {
    const s = view.sections;
    const nothing = !view.overview && !view.daily && !view.campaigns && !view.replies && !view.mailboxes;
    return (
        <div className="flex flex-col gap-4">
            {view.overview && <Overview view={view} />}
            {s.daily_chart && view.daily && <Daily view={view} />}
            {view.campaigns && <Campaigns view={view} />}
            {view.replies && <Replies view={view} />}
            {view.mailboxes && <Mailboxes view={view} />}
            {nothing && (
                <div className="rounded-md border border-dashed border-slate-200 px-4 py-10 text-center text-[12px] text-slate-400">
                    Every section of this report is turned off.
                </div>
            )}
        </div>
    );
}

function Card({ label, count, children }: { label: string; count?: number; children: React.ReactNode }) {
    return (
        <section className="rounded-md border border-slate-200 overflow-hidden bg-white">
            <SectionBar label={label} count={count} />
            {children}
        </section>
    );
}

function Overview({ view }: { view: ClientReportView }) {
    const o = view.overview!;
    const positive = o.positive !== undefined;
    return (
        <section className="rounded-md border border-slate-200 overflow-hidden bg-white">
            <StatStrip cols={positive ? 6 : 5}>
                <Stat label="Sent" value={num(o.sent)} sub="emails" />
                <Stat label="Opened" value={pct(o.open_rate)} sub={`${num(o.opened)} opens`} />
                <Stat label="Clicked" value={num(o.clicked)} sub="clicks" />
                <Stat label="Replied" value={pct(o.reply_rate)} sub={`${num(o.replied)} replies`} />
                {positive && <Stat label="Positive" value={pct(o.positive_rate)} sub={`${num(o.positive)} interested`} />}
                <Stat label="Bounced" value={pct(o.bounce_rate)} sub={`${num(o.bounced)} bounces`} last />
            </StatStrip>
        </section>
    );
}

function Daily({ view }: { view: ClientReportView }) {
    const rows = view.daily ?? [];
    const series: TrendSeries[] = [
        { key: "sent", label: "Sent", tone: "sky", values: rows.map((d) => d.sent) },
        { key: "opens", label: "Opens", tone: "emerald", values: rows.map((d) => d.opens) },
        { key: "replies", label: "Replies", tone: "amber", values: rows.map((d) => d.replies) },
    ];
    return (
        <Card label="Daily activity">
            <div className="p-4">
                <MultiTrend
                    labels={rows.map((d) => d.date)}
                    series={series}
                    range={{ from: view.from, to: view.to }}
                    volumeKey="sent"
                    height={240}
                    emptyLabel="No sends in this window"
                />
            </div>
        </Card>
    );
}

function Campaigns({ view }: { view: ClientReportView }) {
    const rows = view.campaigns ?? [];
    const positive = rows.some((c) => c.positive !== undefined);
    return (
        <Card label="Campaigns" count={rows.length}>
            {rows.length === 0 ? (
                <p className="px-5 py-8 text-center text-[12px] text-slate-400">No campaign activity in this window.</p>
            ) : (
                <div className="overflow-x-auto">
                    <table className="w-full text-left min-w-[560px]">
                        <thead>
                            <tr className="border-b border-slate-200">
                                <Th className="pl-5">Campaign</Th>
                                <Th right>Sent</Th>
                                <Th right>Open rate</Th>
                                <Th right className="hidden sm:table-cell">Clicks</Th>
                                <Th right>Reply rate</Th>
                                {positive && <Th right className="hidden md:table-cell">Positive</Th>}
                                <Th right className="pr-5">Bounce rate</Th>
                            </tr>
                        </thead>
                        <tbody>
                            {rows.map((c, i) => (
                                <tr key={`${c.name}-${i}`} className="h-10 border-b border-slate-100 last:border-0">
                                    <td className="pl-5 pr-3 max-w-0 w-full">
                                        <div className="text-[12.5px] text-slate-900 truncate">{c.name}</div>
                                        <div className="text-[10.5px] uppercase tracking-[0.12em] text-slate-400">{c.status}</div>
                                    </td>
                                    <Td>{num(c.sent)}</Td>
                                    <Td muted>{pct(c.open_rate)}</Td>
                                    <Td muted className="hidden sm:table-cell">{num(c.clicks)}</Td>
                                    <Td>{pct(c.reply_rate)}</Td>
                                    {positive && <Td muted className="hidden md:table-cell">{num(c.positive)}</Td>}
                                    <Td muted className="pr-5">{pct(c.bounce_rate)}</Td>
                                </tr>
                            ))}
                        </tbody>
                    </table>
                </div>
            )}
        </Card>
    );
}

function Replies({ view }: { view: ClientReportView }) {
    const rows = view.replies ?? [];
    return (
        <Card label="Replies" count={rows.length}>
            {rows.length === 0 ? (
                <p className="px-5 py-8 text-center text-[12px] text-slate-400">No replies in this window.</p>
            ) : (
                <ul className="divide-y divide-slate-100">
                    {rows.map((r, i) => {
                        const pill = REPLY_PILL[r.classification] ?? REPLY_PILL.unknown;
                        return (
                            <li key={`${r.email}-${i}`} className="px-5 py-2.5">
                                <div className="flex items-center gap-2 min-w-0">
                                    <span className="text-[12.5px] font-medium text-slate-900 truncate">{r.name || r.email}</span>
                                    {r.name && <span className="text-[11px] text-slate-400 truncate hidden sm:inline">{r.email}</span>}
                                    <span className={`shrink-0 inline-flex items-center h-4 px-1.5 rounded text-[10px] font-medium ${pill.cls}`}>
                                        {pill.label}
                                    </span>
                                    <span className="ml-auto shrink-0 text-[11px] text-slate-400 tabular-nums">{formatDateTime(r.replied_at)}</span>
                                </div>
                                <div className="text-[11.5px] text-slate-500 truncate">
                                    {r.campaign}
                                    {r.subject ? ` · ${r.subject}` : ""}
                                </div>
                                {r.snippet && (
                                    <p className="mt-1 text-[12px] text-slate-600 leading-relaxed line-clamp-2">{r.snippet}</p>
                                )}
                            </li>
                        );
                    })}
                </ul>
            )}
        </Card>
    );
}

function Mailboxes({ view }: { view: ClientReportView }) {
    const m = view.mailboxes!;
    return (
        <section className="rounded-md border border-slate-200 overflow-hidden bg-white">
            <SectionBar label="Mailboxes" count={m.total} />
            <StatStrip cols={6}>
                <Stat label="Healthy" value={num(m.healthy)} />
                <Stat label="Warning" value={num(m.warning)} />
                <Stat label="Error" value={num(m.error)} />
                <Stat label="Warmup sent" value={num(m.warmup_sent)} />
                <Stat label="Warmup received" value={num(m.warmup_received)} />
                <Stat label="Warmup replied" value={num(m.warmup_replied)} last />
            </StatStrip>
        </section>
    );
}

function Th({ children, className, right = false }: { children: React.ReactNode; className?: string; right?: boolean }) {
    return (
        <th
            className={`px-3 py-2 text-[10px] font-medium text-slate-400 uppercase tracking-[0.14em] whitespace-nowrap ${right ? "text-right" : ""} ${className ?? ""}`}
        >
            {children}
        </th>
    );
}

function Td({ children, muted = false, className }: { children: React.ReactNode; muted?: boolean; className?: string }) {
    return (
        <td
            className={`px-3 text-right font-mono text-[12px] tabular-nums whitespace-nowrap ${muted ? "text-slate-500" : "text-slate-900"} ${className ?? ""}`}
        >
            {children}
        </td>
    );
}
