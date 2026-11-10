// The /r/$token route: a shared client report, read-only, printable and frameable.
// The portal service already 404s unknown links at the shell; the in-app not-found
// path only fires when a link is revoked while the page is open.

import type React from "react";
import { useEffect, useLayoutEffect, useMemo, useRef, useState } from "react";
import { keepPreviousData, useQuery } from "@tanstack/react-query";
import { useParams } from "@tanstack/react-router";

import { StalePageError } from "./api";
import { BrandFooter, BrandMark } from "./Brand";
import { isEmbedded, startPageResizeReporting } from "./embed";
import { AlertIcon, PrintIcon } from "./icons";
import { useBrandLook } from "./look";
import { PlainState, StalePage } from "./NotFound";
import {
    fetchReport,
    ReportNotFoundError,
    type PublicReport,
    type ReportDay,
    type ReportRange,
    type ReportView,
} from "./reportApi";
import type { Corners } from "./theme";
import "./report.css";

const RANGE_LABEL: Record<ReportRange, string> = {
    last_7_days: "Last 7 days",
    last_30_days: "Last 30 days",
    this_month: "This month",
    last_month: "Last month",
};

const RANGES = Object.keys(RANGE_LABEL) as ReportRange[];

const CLASS_LABEL: Record<string, string> = {
    positive: "Positive",
    neutral: "Neutral",
    negative: "Not interested",
    unsubscribe: "Unsubscribe",
    unknown: "Unclassified",
    unclassified: "Unclassified",
};

// The top corners of a bar, in chart pixels, for each corner style.
const BAR_RADIUS: Record<Corners, number> = { sharp: 0, default: 3, round: 6 };

const number = new Intl.NumberFormat();

function rangeFromURL(): ReportRange | null {
    const r = new URLSearchParams(window.location.search).get("range");
    return r && (RANGES as string[]).includes(r) ? (r as ReportRange) : null;
}

function pct(v: number): string {
    return `${v.toFixed(1)}%`;
}

function dayLabel(iso: string, opts: Intl.DateTimeFormatOptions = { month: "short", day: "numeric" }): string {
    const [y, m, d] = iso.split("-").map(Number);
    return new Date(Date.UTC(y, m - 1, d)).toLocaleDateString(undefined, { ...opts, timeZone: "UTC" });
}

export function ReportPage() {
    const { token } = useParams({ from: "/r/$token" });
    const [range, setRange] = useState<ReportRange | null>(rangeFromURL);
    const query = useQuery({
        queryKey: ["report", token, range],
        queryFn: () => fetchReport(token, range),
        placeholderData: keepPreviousData,
        staleTime: 60_000,
        retry: (count, err) => !(err instanceof ReportNotFoundError) && !(err instanceof StalePageError) && count < 2,
    });

    useEffect(() => {
        if (isEmbedded()) document.body.classList.add("embed");
        return startPageResizeReporting("report");
    }, []);

    useEffect(() => {
        if (query.data) document.title = query.data.report.name;
    }, [query.data]);
    useBrandLook(query.data?.brand);

    const choose = (r: ReportRange) => {
        setRange(r);
        const url = new URL(window.location.href);
        url.searchParams.set("range", r);
        window.history.replaceState(null, "", url);
    };

    if (query.isPending) return <Loading />;
    if (query.isError) {
        if (query.error instanceof StalePageError) return <StalePage />;
        if (query.error instanceof ReportNotFoundError) {
            return (
                <PlainState
                    icon={<AlertIcon />}
                    title="This report is no longer shared"
                    body="The link may have expired or been turned off. Ask whoever sent it for a new one."
                />
            );
        }
        return (
            <PlainState
                icon={<AlertIcon />}
                title="This report is temporarily unavailable"
                body="Please try again in a moment."
                action={{ label: "Try again", run: () => void query.refetch() }}
            />
        );
    }
    return <Report data={query.data} onRange={choose} loading={query.isFetching} />;
}

// Loading keeps the shape of the report, so the page does not jump when the numbers arrive.
function Loading() {
    return (
        <div className="wr" aria-busy="true">
            <div className="wr-header">
                <span className="pt-sk wr-sk-mark" />
            </div>
            <div className="wr-main" role="status">
                <span className="pt-sr">Loading</span>
                <div className="wr-titlebar">
                    <div>
                        <span className="pt-sk wr-sk-eyebrow" />
                        <span className="pt-sk wr-sk-title" />
                    </div>
                    <span className="pt-sk wr-sk-ranges" />
                </div>
                <div className="wr-tiles">
                    {[0, 1, 2, 3].map((i) => (
                        <span key={i} className="pt-sk wr-sk-tile" />
                    ))}
                </div>
                <span className="pt-sk wr-sk-chart" />
                <span className="pt-sk wr-sk-table" />
            </div>
        </div>
    );
}

function Report({ data, onRange, loading }: { data: PublicReport; onRange: (r: ReportRange) => void; loading: boolean }) {
    const { brand, report } = data;
    const ranges = data.ranges?.length ? data.ranges : RANGES;
    return (
        <div className="wr">
            <header className="wr-header">
                <BrandMark brand={brand} />
            </header>
            <main className="wr-main" aria-busy={loading}>
                <div className="wr-titlebar">
                    <div className="wr-heading">
                        <p className="wr-eyebrow">
                            {report.from === report.to
                                ? dayLabel(report.to, { month: "short", day: "numeric", year: "numeric" })
                                : `${dayLabel(report.from)} to ${dayLabel(report.to, { month: "short", day: "numeric", year: "numeric" })}`}
                        </p>
                        <h1 className="wr-title">{report.name}</h1>
                    </div>
                    <div className="wr-actions">
                        <div className="wr-ranges" role="group" aria-label="Date range">
                            {ranges.map((r) => (
                                <button
                                    key={r}
                                    type="button"
                                    className={r === report.date_range ? "is-active" : ""}
                                    aria-pressed={r === report.date_range}
                                    onClick={() => onRange(r)}
                                >
                                    {RANGE_LABEL[r] ?? r}
                                </button>
                            ))}
                        </div>
                        <button type="button" className="wr-print" onClick={() => window.print()}>
                            <PrintIcon /> <span className="wr-print-label">Print</span>
                        </button>
                    </div>
                </div>
                <Body report={report} corners={brand.theme.corners} />
                <p className="wr-generated">
                    Updated {new Date(report.generated_at).toLocaleString(undefined, { dateStyle: "medium", timeStyle: "short" })}. Days are in UTC.
                </p>
            </main>
            <BrandFooter brand={brand} className="wr-footer" showName />
        </div>
    );
}

function Body({ report, corners }: { report: ReportView; corners: Corners }) {
    const s = report.sections;
    const nothing =
        !report.overview && !report.daily && !report.campaigns?.length && !report.replies?.length && !report.mailboxes;
    return (
        <>
            {report.overview && <Tiles o={report.overview} />}
            {s.daily_chart && report.daily && <DailyChart days={report.daily} corners={corners} />}
            {s.campaigns && report.campaigns && (
                <Section title="Campaigns">
                    {report.campaigns.length === 0 ? (
                        <p className="wr-empty">Nothing was sent in this period.</p>
                    ) : (
                        <CampaignTable rows={report.campaigns} positive={s.positive_replies} />
                    )}
                </Section>
            )}
            {(s.replies || s.positive_replies) && report.replies && (
                <Section title={s.replies ? "Recent replies" : "Positive replies"}>
                    {report.replies.length === 0 ? (
                        <p className="wr-empty">No replies in this period yet.</p>
                    ) : (
                        <Replies rows={report.replies} snippets={report.show_reply_snippets} />
                    )}
                </Section>
            )}
            {report.mailboxes && <Mailboxes m={report.mailboxes} />}
            {nothing && <p className="wr-empty wr-empty-page">There is nothing to show for this period.</p>}
        </>
    );
}

function Section({ title, action, children }: { title: string; action?: React.ReactNode; children: React.ReactNode }) {
    return (
        <section className="wr-section">
            <div className="wr-section-head">
                <h2 className="wr-h2">{title}</h2>
                {action}
            </div>
            {children}
        </section>
    );
}

function Tiles({ o }: { o: NonNullable<ReportView["overview"]> }) {
    const tiles: { label: string; value: string; note?: string }[] = [
        { label: "Sent", value: number.format(o.sent) },
        { label: "Opened", value: number.format(o.opened), note: `${pct(o.open_rate)} open rate` },
        { label: "Replied", value: number.format(o.replied), note: `${pct(o.reply_rate)} reply rate` },
    ];
    if (o.positive !== undefined) {
        tiles.push({ label: "Positive replies", value: number.format(o.positive), note: `${pct(o.positive_rate ?? 0)} of sent` });
    }
    tiles.push({ label: "Bounce rate", value: pct(o.bounce_rate), note: `${number.format(o.bounced)} bounced` });
    return (
        <div className="wr-tiles">
            {tiles.map((t) => (
                <div key={t.label} className="wr-tile">
                    <div className="wr-tile-label">{t.label}</div>
                    <div className="wr-tile-value">{t.value}</div>
                    {t.note && <div className="wr-tile-note">{t.note}</div>}
                </div>
            ))}
        </div>
    );
}

// One series (emails sent per day) as bars, with the day's opens and replies in the hover tooltip and the table.
function DailyChart({ days, corners }: { days: ReportDay[]; corners: Corners }) {
    const [asTable, setAsTable] = useState(false);
    const quiet = days.every((d) => d.sent === 0);
    return (
        <Section
            title="Emails sent per day"
            action={
                !quiet && (
                    <button type="button" className="wr-link" onClick={() => setAsTable((v) => !v)}>
                        {asTable ? "Show chart" : "Show as table"}
                    </button>
                )
            }
        >
            {quiet ? (
                <p className="wr-empty">Nothing was sent in this period.</p>
            ) : asTable ? (
                <div className="wr-scroll">
                    <table className="wr-table">
                        <thead>
                            <tr>
                                <th>Day</th>
                                <th className="num">Sent</th>
                                <th className="num">Opened</th>
                                <th className="num">Replied</th>
                            </tr>
                        </thead>
                        <tbody>
                            {days.map((d) => (
                                <tr key={d.date}>
                                    <td>{dayLabel(d.date)}</td>
                                    <td className="num">{number.format(d.sent)}</td>
                                    <td className="num">{number.format(d.opens)}</td>
                                    <td className="num">{number.format(d.replies)}</td>
                                </tr>
                            ))}
                        </tbody>
                    </table>
                </div>
            ) : (
                <Bars days={days} radius={BAR_RADIUS[corners]} />
            )}
        </Section>
    );
}

const CHART_H = 200;
const PAD_TOP = 8;
const PAD_BOTTOM = 24;

// Bars draws at the width it is given, one chart unit per pixel, so labels are never stretched. Paper scales the whole drawing.
function Bars({ days, radius }: { days: ReportDay[]; radius: number }) {
    const box = useRef<HTMLDivElement>(null);
    const [W, setW] = useState(640);
    const [hover, setHover] = useState<number | null>(null);
    useLayoutEffect(() => {
        const el = box.current;
        if (!el) return;
        const read = () => setW(Math.max(240, Math.round(el.clientWidth)));
        read();
        const ro = new ResizeObserver(read);
        ro.observe(el);
        return () => ro.disconnect();
    }, []);

    const max = useMemo(() => Math.max(1, ...days.map((d) => d.sent)), [days]);
    const ticks = useMemo(() => niceTicks(max), [max]);
    const top = ticks[ticks.length - 1];
    const padL = 14 + 6.2 * number.format(top).length;
    const plotW = W - padL;
    const plotH = CHART_H - PAD_TOP - PAD_BOTTOM;
    const floor = PAD_TOP + plotH;
    const slot = plotW / Math.max(1, days.length);
    const barW = Math.max(2, Math.min(34, slot * 0.7));
    const labelEvery = Math.max(1, Math.ceil(days.length / Math.max(2, Math.floor(plotW / 64))));
    const active = hover !== null ? days[hover] : undefined;
    const centre = hover !== null ? padL + hover * slot + slot / 2 : 0;

    return (
        <div ref={box} className="wr-chart" onMouseLeave={() => setHover(null)}>
            <svg viewBox={`0 0 ${W} ${CHART_H}`} role="img" aria-label="Emails sent per day">
                {ticks.map((t) => {
                    const y = floor - (t / top) * plotH;
                    return (
                        <g key={t}>
                            <line x1={padL} x2={W} y1={y} y2={y} className="wr-grid" />
                            <text x={padL - 8} y={y + 3.5} className="wr-axis" textAnchor="end">
                                {number.format(t)}
                            </text>
                        </g>
                    );
                })}
                {days.map((d, i) => {
                    const h = (d.sent / top) * plotH;
                    const x = padL + i * slot + (slot - barW) / 2;
                    const y = floor - h;
                    const r = Math.min(radius, barW / 2, h);
                    return (
                        <g key={d.date}>
                            {hover === i && <rect x={padL + i * slot} y={PAD_TOP} width={slot} height={plotH} className="wr-band" />}
                            {h > 0 && (
                                <path
                                    className={`wr-bar${hover === i ? " is-hover" : ""}`}
                                    d={`M${x},${floor} V${y + r} Q${x},${y} ${x + r},${y} H${x + barW - r} Q${x + barW},${y} ${x + barW},${y + r} V${floor} Z`}
                                />
                            )}
                            <rect
                                x={padL + i * slot}
                                y={0}
                                width={slot}
                                height={CHART_H}
                                fill="transparent"
                                onMouseEnter={() => setHover(i)}
                                onFocus={() => setHover(i)}
                                onTouchStart={() => setHover(i)}
                                tabIndex={-1}
                            />
                            {i % labelEvery === 0 && (
                                <text x={padL + i * slot + slot / 2} y={CHART_H - 6} className="wr-axis" textAnchor="middle">
                                    {dayLabel(d.date)}
                                </text>
                            )}
                        </g>
                    );
                })}
                <line x1={padL} x2={W} y1={floor} y2={floor} className="wr-baseline" />
            </svg>
            {active && (
                <div className="wr-tooltip" style={{ left: `${W > 180 ? Math.min(W - 86, Math.max(86, centre)) : centre}px` }} role="status">
                    <div className="wr-tooltip-day">{dayLabel(active.date, { weekday: "short", month: "short", day: "numeric" })}</div>
                    <div>
                        <span className="wr-swatch" /> {number.format(active.sent)} sent
                    </div>
                    <div className="wr-muted">
                        {number.format(active.opens)} opened · {number.format(active.replies)} replied
                    </div>
                </div>
            )}
        </div>
    );
}

function niceTicks(max: number): number[] {
    const raw = max / 4;
    const mag = 10 ** Math.floor(Math.log10(raw));
    const step = [1, 2, 5, 10].map((m) => m * mag).find((s) => s >= raw) ?? raw;
    const out: number[] = [];
    for (let v = 0; v <= max + step - 1e-9; v += step) out.push(Math.round(v));
    return out.length > 1 ? out : [0, 1];
}

function CampaignTable({ rows, positive }: { rows: NonNullable<ReportView["campaigns"]>; positive: boolean }) {
    return (
        <div className="wr-scroll">
            <table className="wr-table">
                <thead>
                    <tr>
                        <th>Campaign</th>
                        <th className="num">Sent</th>
                        <th className="num">Open rate</th>
                        <th className="num">Replies</th>
                        {positive && <th className="num">Positive</th>}
                        <th className="num">Bounce rate</th>
                    </tr>
                </thead>
                <tbody>
                    {rows.map((c, i) => (
                        <tr key={`${c.name}-${i}`}>
                            <td className="wr-name">{c.name}</td>
                            <td className="num">{number.format(c.sent)}</td>
                            <td className="num">{pct(c.open_rate)}</td>
                            <td className="num">
                                {number.format(c.replies)} <span className="wr-muted">({pct(c.reply_rate)})</span>
                            </td>
                            {positive && <td className="num">{number.format(c.positive ?? 0)}</td>}
                            <td className="num">{pct(c.bounce_rate)}</td>
                        </tr>
                    ))}
                </tbody>
            </table>
        </div>
    );
}

function Replies({ rows, snippets }: { rows: NonNullable<ReportView["replies"]>; snippets: boolean }) {
    return (
        <ul className="wr-replies">
            {rows.map((r, i) => (
                <li key={`${r.email}-${r.replied_at}-${i}`} className="wr-reply">
                    <div className="wr-reply-top">
                        <span className="wr-reply-from">{r.name || r.email}</span>
                        {r.name && <span className="wr-muted wr-reply-email">{r.email}</span>}
                        <span className={`wr-chip is-${Object.hasOwn(CLASS_LABEL, r.classification) ? r.classification : "unknown"}`}>
                            {CLASS_LABEL[r.classification] ?? r.classification}
                        </span>
                        <time className="wr-muted wr-reply-time" dateTime={r.replied_at}>
                            {new Date(r.replied_at).toLocaleString(undefined, { month: "short", day: "numeric", hour: "numeric", minute: "2-digit" })}
                        </time>
                    </div>
                    <div className="wr-reply-subject">{r.subject || r.campaign}</div>
                    {r.subject && <div className="wr-muted wr-reply-campaign">{r.campaign}</div>}
                    {snippets && r.snippet && <p className="wr-snippet">{r.snippet}</p>}
                </li>
            ))}
        </ul>
    );
}

function Mailboxes({ m }: { m: NonNullable<ReportView["mailboxes"]> }) {
    const rows: { label: string; value: number; tone: string }[] = [
        { label: "Healthy", value: m.healthy, tone: "good" },
        { label: "Needs attention", value: m.warning, tone: "warning" },
        { label: "Not sending", value: m.error, tone: "critical" },
    ];
    const counted = rows.filter((r) => r.value > 0);
    return (
        <Section title="Mailboxes">
            <div className="wr-mailboxes">
                <div className="wr-health">
                    {counted.length > 0 && (
                        <div className="wr-healthbar" aria-hidden="true">
                            {counted.map((r) => (
                                <span key={r.label} className={`is-${r.tone}`} style={{ flexGrow: r.value }} />
                            ))}
                        </div>
                    )}
                    {rows.map((r) => (
                        <div key={r.label} className="wr-health-row">
                            <span className={`wr-dot is-${r.tone}`} aria-hidden="true" />
                            <span>{r.label}</span>
                            <span className="wr-health-value">{number.format(r.value)}</span>
                        </div>
                    ))}
                    <div className="wr-muted wr-health-total">{number.format(m.total)} mailboxes in this report</div>
                </div>
                <div className="wr-warmup">
                    <div className="wr-tile-label">Warmup in this period</div>
                    <div className="wr-warmup-grid">
                        <div>
                            <div className="wr-tile-value">{number.format(m.warmup_sent)}</div>
                            <div className="wr-tile-note">sent</div>
                        </div>
                        <div>
                            <div className="wr-tile-value">{number.format(m.warmup_received)}</div>
                            <div className="wr-tile-note">received</div>
                        </div>
                        <div>
                            <div className="wr-tile-value">{number.format(m.warmup_replied)}</div>
                            <div className="wr-tile-note">replied</div>
                        </div>
                    </div>
                </div>
            </div>
        </Section>
    );
}
