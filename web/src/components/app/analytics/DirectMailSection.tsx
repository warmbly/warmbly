// Analytics for mail written by hand, as opposed to sent by a campaign.
//
// The page above this is entirely campaign-shaped: every number on it comes
// from campaign_contact_progress, so a workspace that mostly answers its inbox
// saw an empty dashboard and no sign that anything else had been measured.
//
// Two cards, never blended. Volume comes from the synced mailbox, so it covers
// everything the mailbox sent, including mail written elsewhere, and it covers
// history. Opens and clicks come from the send records, so they only cover mail
// sent through Warmbly by a mailbox that opted in, from the moment it opted in.
// One combined "open rate" would divide opens we can see by sends we never
// measured, so the denominator is stated on the card instead.

import { Link } from "@tanstack/react-router";
import { EyeIcon, InboxIcon, MailIcon, ReplyIcon } from "lucide-react";

import { EmptyBlock, SectionBar, Stat, StatStrip } from "@/components/layout/Page";
import { MultiTrend, type TrendSeries } from "@/components/ui/charts";
import { utcDay } from "@/lib/campaignPeriod";
import useDirectMail from "@/lib/api/hooks/app/analytics/useDirectMail";
import type DirectMailAnalytics from "@/lib/api/models/app/analytics/DirectMailAnalytics";

function num(v: number | undefined): string {
    return (v ?? 0).toLocaleString();
}
function pct(v: number | undefined): string {
    return v == null ? "—" : `${v.toFixed(1)}%`;
}

// Reply turnaround reads better in the largest unit that still says something:
// "2.4 h" beats "144 min", and "—" beats "0 min" when nothing has been answered.
function duration(minutes: number | undefined): string {
    if (!minutes || minutes <= 0) return "—";
    if (minutes < 90) return `${Math.round(minutes)} min`;
    const hours = minutes / 60;
    if (hours < 48) return `${hours.toFixed(1)} h`;
    return `${(hours / 24).toFixed(1)} d`;
}

function SkeletonRows({ rows = 3 }: { rows?: number }) {
    return (
        <div className="divide-y divide-slate-200/60">
            {Array.from({ length: rows }).map((_, i) => (
                <div key={i} className="h-11 px-5 flex items-center gap-3">
                    <div className="size-1.5 rounded-full bg-slate-200" />
                    <div className="h-3 w-40 bg-slate-100 rounded animate-pulse" />
                    <div className="ml-auto h-3 w-24 bg-slate-100 rounded animate-pulse" />
                </div>
            ))}
        </div>
    );
}

// unfiltered marks the section while the page is narrowed to campaigns: direct mail belongs to none.
export default function DirectMailSection({ period, unfiltered = false }: { period: string; unfiltered?: boolean }) {
    const q = useDirectMail(period);
    const d: DirectMailAnalytics | undefined = q.data;
    const vol = d?.volume;
    const tr = d?.tracking;

    const series: TrendSeries[] = [
        { key: "sent", label: "Sent", tone: "sky", values: (d?.daily_trend ?? []).map((x) => x.sent) },
        { key: "received", label: "Received", tone: "emerald", values: (d?.daily_trend ?? []).map((x) => x.received) },
    ];
    const labels = (d?.daily_trend ?? []).map((x) => utcDay(x.date));

    if (q.isError) {
        return (
            <>
                <SectionBar label="Direct mail" />
                <EmptyBlock
                    title="Couldn't load direct mail"
                    body="The volume and reply figures are read from your synced mailboxes. Refresh to try again."
                />
            </>
        );
    }

    return (
        <>
            <SectionBar label="Direct mail">
                {unfiltered && (
                    <span className="text-[10.5px] text-slate-400" title="Direct mail belongs to no campaign, so the campaign filter does not apply">
                        Not filtered by campaign
                    </span>
                )}
                <Link
                    to="/app/unibox/{-$scope}/{-$threadId}"
                    params={{ scope: "sent", threadId: undefined }}
                    className="inline-flex items-center gap-1 text-[11px] text-slate-500 hover:text-slate-900 transition-colors"
                >
                    Open sent mail
                </Link>
            </SectionBar>

            {/* Volume: measured from the mailbox itself, so it needs no opt-in
                and covers mail composed anywhere, not just here. */}
            <StatStrip cols={4}>
                <Stat
                    label="Sent by hand"
                    value={q.isPending ? "—" : num(vol?.sent)}
                    sub="across all mailboxes"
                    accent={!!vol && vol.sent > 0}
                />
                <Stat
                    label="Received"
                    value={q.isPending ? "—" : num(vol?.received)}
                    sub={vol && vol.bounced > 0 ? `${num(vol.bounced)} of them bounces` : "into the inbox"}
                />
                <Stat
                    label="Reply rate"
                    value={q.isPending ? "—" : pct(vol?.reply_rate)}
                    sub={vol ? `${num(vol.replied)} of ${num(vol.threads_started)} threads` : "threads you started"}
                />
                <Stat
                    label="Median reply time"
                    value={q.isPending ? "—" : duration(vol?.median_reply_minutes)}
                    sub="how fast they answer"
                    last
                />
            </StatStrip>

            <div className="px-5 py-4 border-b border-slate-200">
                <MultiTrend
                    labels={labels}
                    series={series}
                    height={200}
                    emptyLabel="No mail sent or received in this range"
                />
            </div>

            {/* Opens and clicks: the opt-in half. The denominator is on the card
                because it is not the same as "sent by hand" above. */}
            <SectionBar label="Opens and clicks" count={tr ? `${tr.mailboxes_opted_in}/${tr.mailboxes_total} mailboxes` : undefined} />
            {q.isPending ? (
                <SkeletonRows rows={1} />
            ) : (tr?.mailboxes_opted_in ?? 0) === 0 ? (
                <EmptyBlock
                    title="No mailbox is tracking direct mail"
                    body="Open a mailbox and turn on 'Track opens and clicks on direct mail' to measure opens and clicks on messages you send from Warmbly. It is off by default, and it only applies to mail sent after you switch it on."
                />
            ) : (tr?.tracked_sent ?? 0) === 0 ? (
                <EmptyBlock
                    title="Nothing tracked yet in this range"
                    body="Tracking is on, but no direct mail has gone out since. The next reply you send from here will be counted."
                />
            ) : (
                <StatStrip cols={4}>
                    <Stat label="Tracked sends" value={num(tr?.tracked_sent)} sub="the denominator" accent />
                    <Stat label="Opened" value={pct(tr?.open_rate)} sub={`${num(tr?.opened)} by a person`} />
                    <Stat label="Auto-opens" value={num(tr?.machine_opened)} sub="not counted above" />
                    <Stat label="Clicked" value={pct(tr?.click_rate)} sub={`${num(tr?.clicked)} messages`} last />
                </StatStrip>
            )}

            <SectionBar label="Per mailbox" />
            {q.isPending ? (
                <SkeletonRows />
            ) : (d?.mailboxes?.length ?? 0) === 0 ? (
                <EmptyBlock title="No mailboxes connected" body="Connect a mailbox and its sending shows up here." />
            ) : (
                <div className="divide-y divide-slate-200/60">
                    {d!.mailboxes.map((m) => (
                        <div key={m.email_account_id} className="h-11 px-5 flex items-center gap-3">
                            <MailIcon className="w-3.5 h-3.5 text-slate-400 shrink-0" />
                            <span className="text-[12.5px] font-medium text-slate-900 truncate max-w-[40%]">{m.email}</span>
                            {m.track_direct_mail && (
                                <span
                                    title="Opens and clicks are tracked on this mailbox's direct mail"
                                    className="shrink-0 inline-flex items-center gap-1 px-1.5 rounded bg-sky-50 text-sky-700 text-[10px] font-medium"
                                >
                                    <EyeIcon className="w-2.5 h-2.5" />
                                    Tracked
                                </span>
                            )}
                            <span className="ml-auto flex items-center gap-2 md:gap-4 font-mono text-[11px] text-slate-500 tabular-nums shrink-0">
                                <span title="Sent by hand">{num(m.sent)} sent</span>
                                <span title="Received" className="text-emerald-600">{num(m.received)} in</span>
                            </span>
                        </div>
                    ))}
                </div>
            )}

            <SectionBar label="Top correspondents" />
            {q.isPending ? (
                <SkeletonRows />
            ) : (d?.top_contacts?.length ?? 0) === 0 ? (
                <EmptyBlock
                    title="No conversations in this range"
                    body="The people you exchange the most mail with appear here, ranked by how much you sent them."
                />
            ) : (
                <div className="divide-y divide-slate-200/60">
                    {d!.top_contacts.map((c) => (
                        <div key={c.email} className="h-11 px-5 flex items-center gap-3">
                            <InboxIcon className="w-3.5 h-3.5 text-slate-400 shrink-0" />
                            <span className="text-[12.5px] text-slate-900 truncate max-w-[45%]" title={c.email}>
                                {c.email}
                            </span>
                            <span className="ml-auto flex items-center gap-2 md:gap-4 font-mono text-[11px] text-slate-500 tabular-nums shrink-0">
                                <span title="You sent">{num(c.sent)} sent</span>
                                <span title="They sent" className="inline-flex items-center gap-1 text-emerald-600">
                                    <ReplyIcon className="w-3 h-3" />
                                    {num(c.received)}
                                </span>
                            </span>
                        </div>
                    ))}
                </div>
            )}
        </>
    );
}
