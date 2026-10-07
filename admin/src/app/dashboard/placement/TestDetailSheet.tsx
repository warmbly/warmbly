// One placement test in full: where each copy landed, per host family and per
// seed, and the rules pass over the copy. Kept live by the realtime spine's
// placement group.

import { useQuery } from "@tanstack/react-query";
import type { ReactNode } from "react";
import { ArrowLeftRight, CircleAlert } from "lucide-react";
import { Sheet, SheetContent, SheetDescription, SheetHeader, SheetTitle } from "@/components/ui/sheet";
import { Button } from "@/components/ui/button";
import { Skeleton } from "@/components/ui/skeleton";
import { Callout, StatusBadge } from "@/components/ui/kit";
import { ErrorState } from "@/components/ErrorState";
import { TONE_TEXT, type Tone } from "@/lib/tones";
import { cn } from "@/lib/utils";
import { getPlacementTest, type PlacementCounts } from "@/lib/api/client/admin/placement";
import { absolute, relative } from "@/app/dashboard/jobs/format";
import { FolderBadge, TestStatusBadge } from "./badges";
import { ORIGIN_LABEL, PANEL_LABEL, pct } from "./format";

export function TestDetailSheet({
    testId,
    onOpenChange,
    onOpenTest,
}: {
    testId: string | null;
    onOpenChange: (open: boolean) => void;
    onOpenTest: (id: string) => void;
}) {
    return (
        <Sheet open={testId !== null} onOpenChange={onOpenChange}>
            <SheetContent side="right" className="w-full gap-0 sm:max-w-2xl">
                {testId && <Detail id={testId} onOpenTest={onOpenTest} />}
            </SheetContent>
        </Sheet>
    );
}

function Detail({ id, onOpenTest }: { id: string; onOpenTest: (id: string) => void }) {
    const q = useQuery({
        queryKey: ["admin", "placement", "test", id],
        queryFn: () => getPlacementTest(id),
    });
    const t = q.data;

    return (
        <>
            <SheetHeader className="gap-1 border-b border-border px-5 py-4 pr-12">
                <SheetTitle className="truncate">{t?.subject || "Placement test"}</SheetTitle>
                <SheetDescription className="text-[12.5px]">
                    {t ? (
                        <>
                            From <span className="font-mono">{t.sender_email || "unknown sender"}</span>, started{" "}
                            <span title={absolute(t.created_at)}>{relative(t.created_at)}</span>
                        </>
                    ) : (
                        <span className="font-mono">{id}</span>
                    )}
                </SheetDescription>
            </SheetHeader>

            <div className="flex-1 space-y-6 overflow-y-auto px-5 py-4">
                {q.isLoading && (
                    <div className="space-y-3">
                        <Skeleton className="h-20 w-full rounded-lg" />
                        <Skeleton className="h-40 w-full rounded-lg" />
                    </div>
                )}
                {q.error && <ErrorState error={q.error} title="Failed to load the test" onRetry={() => q.refetch()} />}

                {t && (
                    <>
                        <div className="flex flex-wrap items-center gap-1.5">
                            <TestStatusBadge status={t.status} />
                            <StatusBadge>{PANEL_LABEL[t.panel] ?? t.panel} panel</StatusBadge>
                            <StatusBadge>{ORIGIN_LABEL[t.origin] ?? t.origin}</StatusBadge>
                            <StatusBadge>
                                {t.open_tracking || t.link_tracking
                                    ? `tracked (${[t.open_tracking && "opens", t.link_tracking && "clicks"].filter(Boolean).join(", ")})`
                                    : "untracked"}
                            </StatusBadge>
                            {t.finished_at && (
                                <span className="ml-1 text-xs text-muted-foreground" title={absolute(t.finished_at)}>
                                    finished {relative(t.finished_at)}
                                </span>
                            )}
                        </div>

                        {t.error && (
                            <Callout tone="danger" icon={CircleAlert}>
                                {t.error}
                            </Callout>
                        )}

                        {t.compare && (
                            <Callout
                                tone="accent"
                                icon={ArrowLeftRight}
                                actions={
                                    <Button size="xs" variant="outline" onClick={() => onOpenTest(t.compare!.id)}>
                                        Open
                                    </Button>
                                }
                            >
                                Half of a tracking comparison. The other half was sent{" "}
                                {t.compare.open_tracking || t.compare.link_tracking ? "tracked" : "untracked"} to the same
                                seeds and reached the primary inbox at{" "}
                                <span className="font-medium text-foreground">{pct(t.compare.summary.inbox_rate)}</span>.
                            </Callout>
                        )}

                        <SummaryGrid counts={t.summary} />

                        <Section title="By provider">
                            <FamiliesTable families={t.families ?? []} />
                        </Section>

                        <Section
                            title={
                                <>
                                    Content score{" "}
                                    <span className={cn("tabular-nums", TONE_TEXT[scoreTone(t.content.score)])}>
                                        {t.content.score}
                                    </span>
                                    <span className="text-muted-foreground">/100</span>
                                </>
                            }
                        >
                            {(t.content.issues ?? []).length === 0 ? (
                                <p className="text-[13px] text-muted-foreground">
                                    The rules pass found nothing to flag in this copy.
                                </p>
                            ) : (
                                <ul className="overflow-hidden surface-lit rounded-xl border border-border bg-card">
                                    {(t.content.issues ?? []).map((issue, i) => (
                                        <li
                                            key={`${issue.code}-${i}`}
                                            className="flex items-start gap-2.5 border-b border-border/70 px-3.5 py-2.5 last:border-0"
                                        >
                                            <StatusBadge tone={issue.severity === "high" ? "danger" : "warning"} className="mt-px">
                                                {issue.severity}
                                            </StatusBadge>
                                            <div className="min-w-0 text-[13px]">
                                                <div className="text-foreground">{issue.message}</div>
                                                {issue.suggestion && (
                                                    <p className="mt-0.5 text-xs leading-relaxed text-muted-foreground">
                                                        {issue.suggestion}
                                                    </p>
                                                )}
                                            </div>
                                        </li>
                                    ))}
                                </ul>
                            )}
                        </Section>

                        <Section title="Copies">
                            <ResultsTable results={t.results ?? []} />
                        </Section>

                        <p className="border-t border-border pt-3 text-xs text-muted-foreground">
                            {t.campaign_id ? "A campaign step" : "An ad-hoc template"}. Test id{" "}
                            <span className="font-mono select-text">{t.id}</span>
                        </p>
                    </>
                )}
            </div>
        </>
    );
}

function Section({ title, children }: { title: ReactNode; children: ReactNode }) {
    return (
        <section>
            <h3 className="mb-2 text-[13px] font-semibold text-foreground">{title}</h3>
            {children}
        </section>
    );
}

// Same bands as the dashboard placement page.
function scoreTone(score: number): Tone {
    return score >= 80 ? "success" : score >= 50 ? "warning" : "danger";
}

function SummaryGrid({ counts }: { counts: PlacementCounts }) {
    const tabs = counts.promotions + counts.other;
    const cells: { label: string; value: number; sub?: string; tone?: Tone }[] = [
        { label: "Resolved (legacy basis)", value: counts.delivered, sub: `includes timeouts, of ${counts.total}` },
        { label: "Observed receipts", value: counts.observed_receipts ?? counts.inbox + counts.promotions + counts.other + counts.spam, sub: `${counts.unknown ?? 0} unknown, ${counts.archive ?? 0} archive, ${counts.custom ?? 0} custom` },
        { label: "Primary inbox", value: counts.inbox, sub: pct(counts.inbox_rate), tone: "success" },
        { label: "Gmail tabs", value: tabs, sub: pct(counts.tabs_rate), tone: "info" },
        { label: "Spam", value: counts.spam, sub: pct(counts.spam_rate), tone: counts.spam > 0 ? "danger" : undefined },
        {
            label: "Missing",
            value: counts.missing,
            sub: pct(counts.missing_rate),
            tone: counts.missing > 0 ? "warning" : undefined,
        },
        {
            label: "Pending",
            value: counts.pending,
            sub: counts.failed || counts.cancelled ? `${counts.failed} failed, ${counts.cancelled} cancelled` : undefined,
        },
    ];
    return (
        <div className="grid grid-cols-2 gap-px overflow-hidden rounded-lg border border-border bg-border sm:grid-cols-3">
            {cells.map((c) => (
                <div key={c.label} className="bg-card px-3.5 py-3">
                    <div className="text-xs text-muted-foreground">{c.label}</div>
                    <div
                        className={cn(
                            "mt-1 text-[20px] leading-6 font-semibold tracking-[-0.02em] tabular-nums",
                            c.tone ? TONE_TEXT[c.tone] : "text-foreground",
                        )}
                    >
                        {c.value.toLocaleString()}
                    </div>
                    <div className="mt-0.5 h-4 truncate text-xs tabular-nums text-muted-foreground">{c.sub}</div>
                </div>
            ))}
        </div>
    );
}

const TABLE = "overflow-hidden surface-lit rounded-xl border border-border bg-card";
const TH = "h-9 whitespace-nowrap px-3 text-xs font-medium text-muted-foreground first:pl-4 last:pr-4";
const TD = "px-3 first:pl-4 last:pr-4";
const TR = "h-10 border-b border-border/70 transition-colors last:border-0 hover:bg-accent/50";

function FamiliesTable({ families }: { families: { family: string; label: string; counts: PlacementCounts }[] }) {
    if (families.length === 0) {
        return <p className="text-[13px] text-muted-foreground">No copies yet.</p>;
    }
    return (
        <div className={TABLE}>
            <div className="overflow-x-auto">
                <table className="w-full border-collapse text-[13px]">
                    <thead>
                        <tr className="border-b border-border">
                            <th className={cn(TH, "text-left")}>Provider</th>
                            <th className={cn(TH, "text-right")}>Resolved (legacy)</th>
                            <th className={cn(TH, "text-right")}>Inbox</th>
                            <th className={cn(TH, "text-right")}>Tabs</th>
                            <th className={cn(TH, "text-right")}>Spam</th>
                            <th className={cn(TH, "text-right")}>Missing</th>
                            <th className={cn(TH, "text-right")}>Inbox rate</th>
                        </tr>
                    </thead>
                    <tbody>
                        {families.map((f) => (
                            <tr key={f.family} className={cn(TR, "tabular-nums")}>
                                <td className={cn(TD, "whitespace-nowrap")}>{f.label}</td>
                                <td className={cn(TD, "text-right")}>{f.counts.delivered}</td>
                                <td className={cn(TD, "text-right")}>{f.counts.inbox}</td>
                                <td className={cn(TD, "text-right")}>{f.counts.promotions + f.counts.other}</td>
                                <td className={cn(TD, "text-right", f.counts.spam > 0 ? TONE_TEXT.danger : "text-muted-foreground")}>
                                    {f.counts.spam}
                                </td>
                                <td
                                    className={cn(
                                        TD,
                                        "text-right",
                                        f.counts.missing > 0 ? TONE_TEXT.warning : "text-muted-foreground",
                                    )}
                                >
                                    {f.counts.missing}
                                </td>
                                <td className={cn(TD, "text-right font-medium")}>{pct(f.counts.inbox_rate)}</td>
                            </tr>
                        ))}
                    </tbody>
                </table>
            </div>
        </div>
    );
}

function ResultsTable({
    results,
}: {
    results: { seed: string; family_label: string; folder: string; sent_at: string | null; detected_at: string | null; error?: string }[];
}) {
    if (results.length === 0) {
        return <p className="text-[13px] text-muted-foreground">No copies were scheduled.</p>;
    }
    return (
        <div className={TABLE}>
            <div className="overflow-x-auto">
                <table className="w-full border-collapse text-[13px]">
                    <thead>
                        <tr className="border-b border-border">
                            <th className={cn(TH, "text-left")}>Seed</th>
                            <th className={cn(TH, "text-left")}>Provider</th>
                            <th className={cn(TH, "text-left")}>Folder</th>
                            <th className={cn(TH, "text-left")}>Sent</th>
                            <th className={cn(TH, "text-left")}>Found</th>
                        </tr>
                    </thead>
                    <tbody>
                        {results.map((r, i) => (
                            <tr key={`${r.seed}-${i}`} className={TR}>
                                <td className={cn(TD, "py-2 font-mono text-[12.5px]")}>{r.seed}</td>
                                <td className={cn(TD, "whitespace-nowrap")}>{r.family_label}</td>
                                <td className={cn(TD, "py-2")}>
                                    <FolderBadge folder={r.folder} />
                                    {r.error && <div className={cn("mt-1 max-w-56 text-xs", TONE_TEXT.danger)}>{r.error}</div>}
                                </td>
                                <td className={cn(TD, "whitespace-nowrap text-muted-foreground")} title={absolute(r.sent_at)}>
                                    {r.sent_at ? relative(r.sent_at) : "—"}
                                </td>
                                <td className={cn(TD, "whitespace-nowrap text-muted-foreground")} title={absolute(r.detected_at)}>
                                    {r.detected_at ? relative(r.detected_at) : "—"}
                                </td>
                            </tr>
                        ))}
                    </tbody>
                </table>
            </div>
        </div>
    );
}
