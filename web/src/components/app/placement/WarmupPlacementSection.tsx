// Workspace warmup placement on the deliverability page: inbox vs spam over
// time, per recipient provider, and every mailbox ranked worst first.
import { useMemo, useState, type ReactNode } from "react";
import { useNavigate } from "react-router-dom";
import { ChevronRightIcon } from "lucide-react";
import { EmptyBlock, SectionBar, Stat, StatStrip } from "@/components/layout/Page";
import useWarmupPlacement from "@/lib/api/hooks/app/analytics/useWarmupPlacement";
import type { PlacementGroup, PlacementMailbox } from "@/lib/api/models/app/analytics/WarmupPlacement";
import { cn } from "@/lib/utils";
import { BAND, GROUP_LABEL, GROUP_ORDER, bandForRate, fmtNum, fmtPct, rateSentence, totals, utcWindow, viewDays, type GroupFilter } from "./placement";
import {
    BandChip,
    GroupFilterChips,
    LandedLegend,
    PlacementColumns,
    PlacementRateBadge,
    ProviderBreakdown,
    RateSpark,
    RateTrend,
} from "./PlacementCharts";

const PAGE = 25;

export default function WarmupPlacementSection({ days }: { days: number }) {
    const { from, to } = useMemo(() => utcWindow(days), [days]);
    const q = useWarmupPlacement(undefined, from, to);
    const report = q.data;
    const [group, setGroup] = useState<GroupFilter>("all");
    const [attentionOnly, setAttentionOnly] = useState(false);
    const [showAll, setShowAll] = useState(false);
    const navigate = useNavigate();

    const groups = useMemo<PlacementGroup[]>(
        () => GROUP_ORDER.filter((g) => report?.providers.some((p) => p.group === g && p.delivered > 0)),
        [report],
    );
    const activeGroup: GroupFilter = group !== "all" && !groups.includes(group) ? "all" : group;
    const view = useMemo(
        () => (report ? viewDays(report.daily, activeGroup, report.rate.min_sample, report.rate.window_days) : []),
        [report, activeGroup],
    );
    const t = useMemo(() => totals(view), [view]);

    const mailboxes = report?.mailboxes ?? [];
    const attention = mailboxes.filter((m) => m.rate.band === "poor" || m.rate.band === "fair");
    const listed = attentionOnly ? attention : mailboxes;
    const shown = showAll ? listed : listed.slice(0, PAGE);

    if (q.isPending) {
        return (
            <>
                <SectionBar label="Warmup inbox placement" />
                <div className="px-5 py-4">
                    <div className="h-[180px] rounded-md bg-slate-50 animate-pulse" />
                </div>
            </>
        );
    }
    if (!report || report.summary.delivered === 0) {
        return (
            <>
                <SectionBar label="Warmup inbox placement" />
                <EmptyBlock
                    title={q.isError ? "Couldn't load warmup placement" : "No warmup deliveries in this window"}
                    body="Every warmup email is checked in the partner's mailbox and recorded as inbox, another Gmail tab, or spam, per provider and per mailbox."
                />
            </>
        );
    }

    const rate = report.rate;
    const filtered = activeGroup !== "all";

    return (
        <>
            <SectionBar label="Warmup inbox placement">
                <GroupFilterChips value={activeGroup} onChange={setGroup} groups={groups} className="max-w-full" />
            </SectionBar>

            <StatStrip cols={5}>
                <Stat
                    label="Inbox rate · 7 days"
                    value={<span className={BAND[rate.band].text}>{rate.inbox_rate != null ? fmtPct(rate.inbox_rate) : "—"}</span>}
                    sub={rate.inbox_rate != null ? BAND[rate.band].label : rateSentence(rate)}
                />
                <Stat
                    label="Inbox rate · window"
                    value={<span className={BAND[bandForRate(t.inboxRate)].text}>{fmtPct(t.inboxRate)}</span>}
                    sub={filtered ? `at ${GROUP_LABEL[activeGroup]}` : `${fmtNum(t.inbox)} primary · ${fmtNum(t.tabs)} tabs`}
                />
                <Stat label="Delivered" value={t.delivered} sub={filtered ? "sends are not split by provider" : `of ${fmtNum(t.sent)} sent`} />
                <Stat
                    label="Spam"
                    value={<span className={t.spam > 0 ? "text-rose-600" : undefined}>{fmtNum(t.spam)}</span>}
                    sub={`${fmtPct(t.spamRate)} · ${fmtNum(t.rescued)} rescued`}
                />
                <Stat label="Unconfirmed" value={filtered ? "—" : t.unconfirmed} sub="sent 24h+ ago, not seen yet" last />
            </StatStrip>

            <div className="grid lg:grid-cols-2 border-b border-slate-200/60">
                <div className="px-5 py-4 lg:border-r lg:border-slate-200/60">
                    <div className="flex items-center justify-between gap-3 mb-3">
                        <Eyebrow>Where warmup mail landed</Eyebrow>
                        <LandedLegend />
                    </div>
                    <PlacementColumns days={view} height={140} />
                </div>
                <div className="px-5 py-4">
                    <div className="mb-3">
                        <Eyebrow>Inbox rate over time</Eyebrow>
                    </div>
                    <RateTrend days={view} windowDays={rate.window_days} height={130} />
                </div>
            </div>

            <SectionBar label="Warmup placement by provider" count={report.providers.length || undefined} />
            <ProviderBreakdown providers={report.providers} />
            <p className="px-5 py-3 text-[11.5px] text-slate-500 leading-relaxed border-t border-slate-200/60">
                Warmup partner selection reads these numbers per mail host: a mailbox landing in spam at one host is sent
                fewer partners there while the rate stays high, and more again once it recovers. It is never cut off
                entirely, because a sender that stops mailing a host can never find out that it recovered.
            </p>

            <SectionBar label="Mailboxes by inbox rate" count={listed.length || undefined}>
                <button
                    type="button"
                    onClick={() => setAttentionOnly((v) => !v)}
                    className={cn(
                        "h-6 px-2 rounded text-[11px] font-medium transition-colors border",
                        attentionOnly ? "bg-amber-50 text-amber-700 border-amber-200" : "text-slate-500 border-slate-200 hover:text-slate-900",
                    )}
                >
                    Below 90% only{attention.length > 0 ? ` · ${attention.length}` : ""}
                </button>
            </SectionBar>
            {listed.length === 0 ? (
                <EmptyBlock
                    title={attentionOnly ? "Every mailbox is at 90% or better" : "No mailbox had warmup deliveries in this window"}
                    body="Mailboxes are ranked by their inbox rate over the last 7 days, worst first."
                />
            ) : (
                <div className="divide-y divide-slate-200/60">
                    {shown.map((m) => (
                        <MailboxRow key={m.email_account_id} m={m} onOpen={() => navigate(`/app/emails?mailbox=${m.email_account_id}&tab=deliverability`)} />
                    ))}
                    {listed.length > PAGE && (
                        <button
                            type="button"
                            onClick={() => setShowAll((v) => !v)}
                            className="w-full h-9 text-[11.5px] text-slate-500 hover:text-slate-900 hover:bg-slate-50 transition-colors"
                        >
                            {showAll ? "Show fewer" : `Show all ${listed.length}`}
                        </button>
                    )}
                </div>
            )}
        </>
    );
}

function MailboxRow({ m, onOpen }: { m: PlacementMailbox; onOpen: () => void }) {
    return (
        <button type="button" onClick={onOpen} className="group w-full h-11 px-5 flex items-center gap-3 text-left hover:bg-slate-50/80 transition-colors">
            <span className={cn("size-1.5 rounded-full shrink-0", BAND[m.rate.band].dot)} />
            <span className="text-[12.5px] font-medium text-slate-900 truncate min-w-0 flex-1">{m.email}</span>
            <span className="hidden md:block" title="Daily inbox rate in this window">
                <RateSpark values={m.daily_inbox_rate} />
            </span>
            <span className="hidden sm:flex items-center gap-3 font-mono text-[11px] tabular-nums text-slate-500 shrink-0">
                <span title="Delivered in this window" className="w-16 text-right">{fmtNum(m.delivered)} dlv</span>
                <span title="Spam in this window" className={cn("w-14 text-right", m.spam > 0 ? "text-rose-600" : "text-slate-400")}>{fmtNum(m.spam)} spam</span>
            </span>
            <span className="w-12 text-right shrink-0">
                <PlacementRateBadge rate={m.rate} />
            </span>
            <BandChip band={m.rate.band} className="hidden lg:inline-flex w-[118px] justify-center" />
            <ChevronRightIcon className="w-3.5 h-3.5 text-slate-300 group-hover:text-slate-500 shrink-0" />
        </button>
    );
}

function Eyebrow({ children }: { children: ReactNode }) {
    return <span className="text-[10px] uppercase tracking-[0.14em] text-slate-400 font-medium">{children}</span>;
}
