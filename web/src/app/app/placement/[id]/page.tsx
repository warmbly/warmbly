// One placement test: where every copy landed, by folder, provider and seed,
// the content check of the copy that was sent, and for a tracking comparison
// the two halves side by side. Live through PLACEMENT_TEST_UPDATED.

import React from "react";
import { Link, useParams } from "react-router-dom";
import { ArrowLeftIcon, ArrowUpRightIcon, Loader2Icon, SquareIcon } from "lucide-react";
import toast from "react-hot-toast";
import { EmptyBlock, SectionBar } from "@/components/layout/Page";
import PermissionButton from "@/components/ui/PermissionButton";
import EmailBody from "@/components/app/unibox/EmailBody";
import { IssueRow } from "@/components/app/campaigns/ContentScore";
import { useConfirm } from "@/hooks/context/confirm";
import useCampaign from "@/lib/api/hooks/app/campaigns/useCampaign";
import { useCancelPlacementTest, usePlacementTest } from "@/lib/api/hooks/app/placement/usePlacement";
import {
    PANEL_LABEL,
    type PlacementCounts,
    type PlacementResult,
    type PlacementTest,
    type PlacementTestDetail,
} from "@/lib/api/models/app/placement/Placement";
import type { AppError } from "@/lib/api/client/normalizeError";
import buildError from "@/lib/helper/buildError";
import {
    FolderChip,
    PlacementBar,
    PlacementCaveat,
    PlacementLegend,
    StatusChip,
    TrackingBadge,
} from "@/components/app/placement/tests/PlacementParts";
import {
    FOLDER,
    ORIGIN_LABEL,
    fmtDate,
    fmtRate,
    isTracked,
    rateTone,
    resolvedCount,
} from "@/components/app/placement/tests/placementTests";
import { cn } from "@/lib/utils";

export default function PlacementTestPage() {
    const { id = "" } = useParams();
    const q = usePlacementTest(id);

    return (
        <div className="flex flex-col min-h-full bg-white">
            <div className="px-3 sm:px-5 pt-3 sm:pt-4">
                <Link
                    to="/app/placement"
                    className="inline-flex items-center gap-1 h-6 -ml-1.5 px-1.5 mb-1 rounded-md text-[11.5px] text-slate-500 hover:text-slate-900 hover:bg-slate-100 transition-colors"
                >
                    <ArrowLeftIcon className="w-3 h-3" />
                    Placement tests
                </Link>
            </div>
            {q.isLoading ? (
                <div className="px-5 py-16 flex justify-center">
                    <Loader2Icon className="w-5 h-5 animate-spin text-slate-300" />
                </div>
            ) : q.isError || !q.data ? (
                <EmptyBlock
                    title={(q.error as unknown as AppError)?.status === 404 ? "This test does not exist" : "This test could not be loaded"}
                    body={(q.error as unknown as AppError)?.status === 404 ? "It may belong to another workspace." : q.error ? buildError(q.error as unknown as AppError) : undefined}
                />
            ) : (
                <Detail test={q.data} />
            )}
        </div>
    );
}

function Detail({ test }: { test: PlacementTestDetail }) {
    const confirm = useConfirm();
    const cancel = useCancelPlacementTest();
    const campaign = useCampaign(test.campaign_id ?? "");
    const running = test.status === "running";
    const s = test.summary;

    const onCancel = () =>
        confirm.show(
            "Stop this test? Copies not sent yet are cancelled. The ones already sent keep being classified.",
            async () => {
                try {
                    await cancel.mutateAsync(test.id);
                    toast.success("Test stopped.");
                } catch (e) {
                    const err = e as AppError;
                    toast.error(err?.code === "placement_not_running" ? "This test has already finished." : buildError(err));
                }
            },
        );

    return (
        <>
            {/* Header */}
            <div className="px-3 sm:px-5 pb-4 flex flex-wrap items-start gap-3 border-b border-slate-200">
                <div className="min-w-0 flex-1">
                    <div className="flex flex-wrap items-center gap-x-2 gap-y-1">
                        <h1 className="min-w-0 max-w-full text-[18px] font-semibold text-slate-900 truncate">{test.subject || "(no subject)"}</h1>
                        <StatusChip status={test.status} counts={s} />
                        <TrackingBadge test={test} />
                    </div>
                    <div className="mt-1.5 flex flex-wrap items-center gap-x-3 gap-y-1 text-[11.5px] text-slate-500">
                        <span>
                            From <span className="text-slate-800">{test.sender_email || "a deleted mailbox"}</span>
                        </span>
                        <span>{PANEL_LABEL[test.panel] ?? test.panel}</span>
                        <span>Started {fmtDate(test.created_at)}</span>
                        {test.finished_at && <span>Finished {fmtDate(test.finished_at)}</span>}
                        {test.origin !== "manual" && <span>{ORIGIN_LABEL[test.origin] ?? test.origin}</span>}
                        {test.campaign_id && (
                            <Link to={`/app/campaigns/${test.campaign_id}/steps`} className="inline-flex items-center gap-0.5 text-sky-700 hover:text-sky-800">
                                {campaign.data?.name ?? "Campaign"}
                                <ArrowUpRightIcon className="w-3 h-3" />
                            </Link>
                        )}
                        {test.compare && (
                            <Link to={`/app/placement/${test.compare.id}`} className="inline-flex items-center gap-0.5 text-sky-700 hover:text-sky-800">
                                {isTracked(test.compare) ? "Tracked half" : "Untracked half"}
                                <ArrowUpRightIcon className="w-3 h-3" />
                            </Link>
                        )}
                    </div>
                    {test.error && <p className="mt-1.5 text-[11.5px] text-rose-600">{test.error}</p>}
                </div>
                {running && (
                    <PermissionButton
                        permission="SEND_CAMPAIGNS"
                        type="button"
                        onClick={onCancel}
                        disabled={cancel.isPending}
                        className="shrink-0 h-7 px-2.5 rounded-md border border-slate-200 hover:border-slate-300 bg-white text-[12px] font-medium text-slate-700 hover:text-slate-900 inline-flex items-center gap-1.5 transition-colors disabled:opacity-60"
                    >
                        {cancel.isPending ? <Loader2Icon className="w-3.5 h-3.5 animate-spin" /> : <SquareIcon className="w-3 h-3" />}
                        Stop test
                    </PermissionButton>
                )}
            </div>

            {/* Folder breakdown */}
            <Breakdown counts={s} running={running} />

            {test.compare && <Comparison test={test} other={test.compare} />}

            {/* By provider */}
            <SectionBar label="By provider" count={test.families?.length || undefined}>
                <PlacementLegend className="hidden md:flex" />
            </SectionBar>
            {(test.families ?? []).length === 0 ? (
                <p className="px-5 py-4 text-[12px] text-slate-400">No copies have a verdict yet.</p>
            ) : (
                <div className="overflow-x-auto">
                    <table className="w-full text-left">
                        <thead>
                            <tr className="h-8 border-b border-slate-200/60 text-[10px] uppercase tracking-[0.14em] text-slate-400">
                                <th className="px-5 font-medium">Provider</th>
                                <th className="px-3 font-medium w-[30%] hidden sm:table-cell" />
                                <th className="px-3 font-medium text-right">Inbox</th>
                                <th className="px-3 font-medium text-right">Tabs</th>
                                <th className="px-3 font-medium text-right">Spam</th>
                                <th className="px-3 font-medium text-right">Missing</th>
                                <th className="px-5 font-medium text-right hidden md:table-cell">Copies</th>
                            </tr>
                        </thead>
                        <tbody className="divide-y divide-slate-200/60">
                            {(test.families ?? []).map((f) => (
                                <tr key={f.family} className="h-10">
                                    <td className="px-5 text-[12.5px] font-medium text-slate-900 whitespace-nowrap">{f.label || f.family}</td>
                                    <td className="px-3 hidden sm:table-cell">
                                        <PlacementBar counts={f.counts} />
                                    </td>
                                    <td className={cn("px-3 text-right font-mono text-[11.5px] tabular-nums", rateTone(f.counts.inbox_rate))}>
                                        {fmtRate(f.counts.inbox_rate)}
                                    </td>
                                    <td className="px-3 text-right font-mono text-[11.5px] tabular-nums text-violet-600">{fmtRate(f.counts.tabs_rate)}</td>
                                    <td className="px-3 text-right font-mono text-[11.5px] tabular-nums text-rose-600">{fmtRate(f.counts.spam_rate)}</td>
                                    <td className="px-3 text-right font-mono text-[11.5px] tabular-nums text-slate-500">{fmtRate(f.counts.missing_rate)}</td>
                                    <td className="px-5 text-right font-mono text-[11px] tabular-nums text-slate-400 hidden md:table-cell">
                                        {resolvedCount(f.counts)}/{f.counts.total}
                                    </td>
                                </tr>
                            ))}
                        </tbody>
                    </table>
                </div>
            )}

            <div className="grid lg:grid-cols-[minmax(0,1fr)_minmax(0,1fr)] border-t border-slate-200">
                {/* Per seed */}
                <section className="min-w-0 lg:border-r lg:border-slate-200">
                    <SectionBar label="Seed inboxes" count={test.results?.length || undefined} />
                    <SeedResults results={test.results ?? []} masked={test.panel !== "workspace"} />
                </section>

                {/* Content check + the copy */}
                <section className="min-w-0">
                    <SectionBar label="Content check" />
                    <ContentCheck test={test} />
                    <SectionBar label="Copy that was tested" />
                    <div className="px-5 py-3">
                        <div className="rounded-md border border-slate-200 bg-white">
                            <div className="border-b border-slate-200/70 px-3 py-2 text-[12.5px]">
                                <span className="text-slate-400">Subject: </span>
                                <span className="text-slate-800">{test.subject || "(no subject)"}</span>
                            </div>
                            <div className="min-h-[160px] px-3 py-2.5">
                                {test.body_html || test.body_plain ? (
                                    <EmailBody html={test.body_html || null} plain={test.body_plain || null} />
                                ) : (
                                    <p className="text-[12px] text-slate-400">The body is not stored for this test.</p>
                                )}
                            </div>
                        </div>
                        <p className="mt-1.5 text-[11px] text-slate-400 leading-relaxed">
                            The template as written. Each seed got it rendered for the chosen contact, with the signature,
                            opt-out footer and unsubscribe header the real send adds.
                        </p>
                    </div>
                </section>
            </div>

            <PlacementCaveat className="mx-5 my-5" />
        </>
    );
}

// Hairlines for a 2x2 grid on phones and one row of four from md up.
const CELL_BORDER = ["border-r max-md:border-b", "md:border-r max-md:border-b", "border-r", ""];

function Breakdown({ counts, running }: { counts: PlacementCounts; running: boolean }) {
    const tabs = counts.promotions + counts.other;
    const cells: { label: string; n: number; rate: number | null; tone: string; dot: string; sub?: string }[] = [
        { label: "Inbox", n: counts.inbox, rate: counts.inbox_rate, tone: FOLDER.inbox.text, dot: FOLDER.inbox.dot },
        {
            label: "Gmail tabs",
            n: tabs,
            rate: counts.tabs_rate,
            tone: FOLDER.promotions.text,
            dot: FOLDER.promotions.dot,
            sub: `${counts.promotions} Promotions, ${counts.other} other tabs`,
        },
        { label: "Spam", n: counts.spam, rate: counts.spam_rate, tone: FOLDER.spam.text, dot: FOLDER.spam.dot },
        {
            label: "Never arrived",
            n: counts.missing,
            rate: counts.missing_rate,
            tone: FOLDER.missing.text,
            dot: FOLDER.missing.dot,
            sub: "Not seen within 2 hours",
        },
    ];
    const notSent = counts.failed + counts.cancelled;
    return (
        <section className="border-b border-slate-200">
            <div className="grid grid-cols-2 md:grid-cols-4">
                {cells.map((c, i) => (
                    <div
                        key={c.label}
                        className={cn("px-5 py-4 border-slate-200", CELL_BORDER[i])}
                    >
                        <div className="flex items-center gap-2">
                            <span className={cn("size-1.5 rounded-full", c.dot)} />
                            <span className="text-[10px] uppercase tracking-[0.14em] text-slate-400 font-medium">{c.label}</span>
                        </div>
                        <div className={cn("mt-2 text-[26px] font-light leading-none tabular-nums", c.rate == null ? "text-slate-300" : c.tone)}>
                            {fmtRate(c.rate)}
                        </div>
                        <div className="mt-1.5 text-[10.5px] text-slate-400 font-mono truncate">
                            {c.n} cop{c.n === 1 ? "y" : "ies"}
                            {c.sub ? `, ${c.sub}` : ""}
                        </div>
                    </div>
                ))}
            </div>
            <div className="px-5 pb-4 pt-1">
                <PlacementBar counts={counts} height={8} />
                <p className="mt-2 text-[11px] text-slate-500">
                    {running
                        ? `${resolvedCount(counts)} of ${counts.total} copies have a verdict. This page updates as they arrive.`
                        : `${counts.delivered} of ${counts.total} copies were sent and classified.`}
                    {notSent > 0 && ` ${notSent} not sent (${counts.failed} failed, ${counts.cancelled} cancelled).`}
                    {" "}Rates are shares of the copies that were sent.
                </p>
            </div>
        </section>
    );
}

// Two tests to the same seeds, one untracked and one tracked, side by side.
function Comparison({ test, other }: { test: PlacementTest; other: PlacementTest }) {
    const [without, withT] = isTracked(test) ? [other, test] : [test, other];
    const rows: { label: string; key: keyof PlacementCounts }[] = [
        { label: "Inbox", key: "inbox_rate" },
        { label: "Gmail tabs", key: "tabs_rate" },
        { label: "Spam", key: "spam_rate" },
        { label: "Never arrived", key: "missing_rate" },
    ];
    const diff = (a: number | null, b: number | null) => (a == null || b == null ? null : Math.round((b - a) * 100));
    const inboxDelta = diff(without.summary.inbox_rate, withT.summary.inbox_rate);
    return (
        <section className="border-b border-slate-200">
            <SectionBar label="Tracking comparison" />
            <div className="grid sm:grid-cols-2">
                {[
                    { title: "Without tracking", t: without },
                    { title: "With tracking", t: withT },
                ].map(({ title, t }, i) => (
                    <div key={t.id} className={cn("px-5 py-4 min-w-0", i === 0 && "sm:border-r border-slate-200 max-sm:border-b")}>
                        <div className="flex items-center gap-2">
                            <span className="text-[12.5px] font-medium text-slate-900">{title}</span>
                            <StatusChip status={t.status} counts={t.summary} />
                            {t.id !== test.id && (
                                <Link to={`/app/placement/${t.id}`} className="ml-auto text-[11px] text-sky-700 hover:text-sky-800 inline-flex items-center gap-0.5">
                                    Open
                                    <ArrowUpRightIcon className="w-3 h-3" />
                                </Link>
                            )}
                        </div>
                        <PlacementBar counts={t.summary} className="mt-3" />
                        <dl className="mt-3 grid grid-cols-2 gap-x-4 gap-y-1.5">
                            {rows.map((r) => (
                                <div key={r.key} className="flex items-center justify-between gap-2">
                                    <dt className="text-[11.5px] text-slate-500">{r.label}</dt>
                                    <dd className="font-mono text-[12px] tabular-nums text-slate-800">{fmtRate(t.summary[r.key] as number | null)}</dd>
                                </div>
                            ))}
                        </dl>
                    </div>
                ))}
            </div>
            <p className="px-5 pb-4 text-[11.5px] text-slate-500 leading-relaxed">
                {inboxDelta == null
                    ? "The difference shows once both halves have verdicts."
                    : inboxDelta === 0
                      ? "Tracking made no difference to how much reached the inbox in this test."
                      : inboxDelta < 0
                        ? `The tracked copies reached the inbox ${Math.abs(inboxDelta)} points less often. Run it again before turning tracking off: one test is noise.`
                        : `The tracked copies reached the inbox ${inboxDelta} points more often. With this few seeds that is within the noise.`}
            </p>
        </section>
    );
}

function SeedResults({ results, masked }: { results: PlacementResult[]; masked: boolean }) {
    if (results.length === 0) return <p className="px-5 py-4 text-[12px] text-slate-400">No seeds were picked for this test.</p>;
    return (
        <>
            {masked && (
                <p className="px-5 pt-3 text-[11px] text-slate-400">Addresses on a shared panel are partly hidden.</p>
            )}
            <ul className="divide-y divide-slate-200/60">
                {results.map((r, i) => (
                    <li key={`${r.seed}-${i}`} className="px-5 py-2 flex items-center gap-3 min-h-11">
                        <div className="min-w-0 flex-1">
                            <div className="text-[12.5px] text-slate-800 font-mono truncate">{r.seed}</div>
                            <div className="text-[11px] text-slate-400 truncate">
                                {r.family_label || r.family}
                                {r.detected_at
                                    ? `, seen ${fmtDate(r.detected_at)}`
                                    : r.sent_at
                                      ? `, sent ${fmtDate(r.sent_at)}`
                                      : r.scheduled_at && r.folder === "pending"
                                        ? `, sends ${fmtDate(r.scheduled_at)}`
                                        : ""}
                            </div>
                            {r.error && <div className="text-[11px] text-amber-600 truncate" title={r.error}>{r.error}</div>}
                        </div>
                        <FolderChip folder={r.folder} />
                    </li>
                ))}
            </ul>
        </>
    );
}

function ContentCheck({ test }: { test: PlacementTestDetail }) {
    const { score } = test.content;
    const issues = test.content.issues ?? [];
    const tone = score >= 80 ? "text-emerald-600" : score >= 50 ? "text-amber-600" : "text-rose-600";
    const label = score >= 80 ? "Looks good" : score >= 50 ? "Could improve" : "Needs work";
    return (
        <div className="px-5 py-3">
            <div className="flex items-baseline gap-2">
                <span className={cn("text-[22px] font-light tabular-nums", tone)}>{score}</span>
                <span className="text-[11px] text-slate-400">out of 100</span>
                <span className={cn("text-[11.5px] font-medium", tone)}>{label}</span>
            </div>
            {issues.length === 0 ? (
                <p className="mt-1 text-[11.5px] text-slate-500">Nothing in the copy stands out to a spam filter.</p>
            ) : (
                <ul className="mt-1 divide-y divide-slate-100">
                    {issues.map((issue, i) => (
                        <IssueRow key={`${issue.code}-${i}`} issue={issue} />
                    ))}
                </ul>
            )}
            <p className="mt-2 text-[11px] text-slate-400 leading-relaxed">
                The same rules the step editor checks, run on the copy that was tested. Placement also depends on the
                sender&apos;s reputation, which no content check sees.
            </p>
        </div>
    );
}
