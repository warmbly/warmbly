// One placement test in full: where each copy landed, per host family and per
// seed, and the rules pass over the copy. Kept live by the realtime spine's
// placement group.

import { useQuery } from "@tanstack/react-query";
import { ArrowLeftRight } from "lucide-react";
import { Sheet, SheetContent, SheetDescription, SheetHeader, SheetTitle } from "@/components/ui/sheet";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Skeleton } from "@/components/ui/skeleton";
import { ErrorState } from "@/components/ErrorState";
import { cn } from "@/lib/utils";
import { getPlacementTest, type PlacementCounts } from "@/lib/api/client/admin/placement";
import { absolute, relative } from "@/app/dashboard/jobs/format";
import { FolderBadge, StatusBadge } from "./badges";
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
            <SheetHeader className="border-b border-border pr-10">
                <SheetTitle className="truncate">{t?.subject || "Placement test"}</SheetTitle>
                <SheetDescription className="text-xs">
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

            <div className="flex-1 space-y-5 overflow-y-auto p-4">
                {q.isLoading && (
                    <div className="space-y-3">
                        <Skeleton className="h-20 w-full" />
                        <Skeleton className="h-40 w-full" />
                    </div>
                )}
                {q.error && <ErrorState error={q.error} title="Failed to load the test" onRetry={() => q.refetch()} />}

                {t && (
                    <>
                        <div className="flex flex-wrap items-center gap-1.5 text-xs">
                            <StatusBadge status={t.status} />
                            <Badge variant="outline" className="text-[10px]">
                                {PANEL_LABEL[t.panel] ?? t.panel} panel
                            </Badge>
                            <Badge variant="outline" className="text-[10px]">
                                {ORIGIN_LABEL[t.origin] ?? t.origin}
                            </Badge>
                            <Badge variant="outline" className="text-[10px]">
                                {t.open_tracking || t.link_tracking
                                    ? `tracked (${[t.open_tracking && "opens", t.link_tracking && "clicks"].filter(Boolean).join(", ")})`
                                    : "untracked"}
                            </Badge>
                            {t.finished_at && (
                                <span className="text-muted-foreground" title={absolute(t.finished_at)}>
                                    finished {relative(t.finished_at)}
                                </span>
                            )}
                        </div>

                        {t.error && (
                            <div className="rounded-md border border-red-200 bg-red-50 p-3 text-xs text-red-700">{t.error}</div>
                        )}

                        {t.compare && (
                            <div className="flex items-center justify-between gap-3 rounded-md border border-border bg-muted/30 p-3 text-xs">
                                <span className="text-muted-foreground">
                                    Half of a tracking comparison. The other half was sent{" "}
                                    {t.compare.open_tracking || t.compare.link_tracking ? "tracked" : "untracked"} to the same
                                    seeds and reached the primary inbox at{" "}
                                    <span className="font-medium text-foreground">{pct(t.compare.summary.inbox_rate)}</span>.
                                </span>
                                <Button size="xs" variant="outline" onClick={() => onOpenTest(t.compare!.id)}>
                                    <ArrowLeftRight /> Open
                                </Button>
                            </div>
                        )}

                        <SummaryGrid counts={t.summary} />

                        <Section title="By provider">
                            <FamiliesTable families={t.families ?? []} />
                        </Section>

                        <Section title={`Content score ${t.content.score}/100`}>
                            {(t.content.issues ?? []).length === 0 ? (
                                <p className="text-xs text-muted-foreground">The rules pass found nothing to flag in this copy.</p>
                            ) : (
                                <ul className="space-y-1.5">
                                    {(t.content.issues ?? []).map((issue, i) => (
                                        <li key={`${issue.code}-${i}`} className="rounded-md border border-border p-2 text-xs">
                                            <div className="flex items-center gap-1.5">
                                                <Badge
                                                    variant="outline"
                                                    className={cn(
                                                        "text-[10px]",
                                                        issue.severity === "high"
                                                            ? "border-red-300 bg-red-50 text-red-700"
                                                            : "border-amber-300 bg-amber-50 text-amber-700",
                                                    )}
                                                >
                                                    {issue.severity}
                                                </Badge>
                                                <span>{issue.message}</span>
                                            </div>
                                            {issue.suggestion && (
                                                <p className="mt-1 text-muted-foreground">{issue.suggestion}</p>
                                            )}
                                        </li>
                                    ))}
                                </ul>
                            )}
                        </Section>

                        <Section title="Copies">
                            <ResultsTable results={t.results ?? []} />
                        </Section>

                        <p className="text-[11px] text-muted-foreground">
                            {t.campaign_id ? "A campaign step" : "An ad-hoc template"}. Test id{" "}
                            <span className="font-mono select-text">{t.id}</span>
                        </p>
                    </>
                )}
            </div>
        </>
    );
}

function Section({ title, children }: { title: string; children: React.ReactNode }) {
    return (
        <section>
            <h3 className="mb-2 text-[10px] font-semibold uppercase tracking-[0.14em] text-muted-foreground">{title}</h3>
            {children}
        </section>
    );
}

function SummaryGrid({ counts }: { counts: PlacementCounts }) {
    const tabs = counts.promotions + counts.other;
    const cells: { label: string; value: number; sub?: string; tone?: string }[] = [
        { label: "Delivered", value: counts.delivered, sub: `of ${counts.total}` },
        { label: "Primary inbox", value: counts.inbox, sub: pct(counts.inbox_rate), tone: "text-emerald-700" },
        { label: "Gmail tabs", value: tabs, sub: pct(counts.tabs_rate), tone: "text-sky-700" },
        { label: "Spam", value: counts.spam, sub: pct(counts.spam_rate), tone: counts.spam > 0 ? "text-red-700" : undefined },
        {
            label: "Missing",
            value: counts.missing,
            sub: pct(counts.missing_rate),
            tone: counts.missing > 0 ? "text-amber-700" : undefined,
        },
        {
            label: "Pending",
            value: counts.pending,
            sub: counts.failed || counts.cancelled ? `${counts.failed} failed, ${counts.cancelled} cancelled` : undefined,
        },
    ];
    return (
        <div className="grid grid-cols-2 gap-2 sm:grid-cols-3">
            {cells.map((c) => (
                <div key={c.label} className="rounded-md border border-border bg-card p-2.5">
                    <div className="text-[10px] uppercase tracking-wider text-muted-foreground">{c.label}</div>
                    <div className={cn("mt-0.5 text-lg font-semibold tabular-nums", c.tone)}>{c.value.toLocaleString()}</div>
                    {c.sub && <div className="text-[11px] text-muted-foreground tabular-nums">{c.sub}</div>}
                </div>
            ))}
        </div>
    );
}

function FamiliesTable({ families }: { families: { family: string; label: string; counts: PlacementCounts }[] }) {
    if (families.length === 0) {
        return <p className="text-xs text-muted-foreground">No copies yet.</p>;
    }
    return (
        <div className="overflow-x-auto rounded-md border border-border">
            <table className="w-full text-xs">
                <thead className="bg-muted/40 text-[10px] uppercase tracking-wider text-muted-foreground">
                    <tr>
                        <th className="px-2.5 py-1.5 text-left font-medium">Provider</th>
                        <th className="px-2.5 py-1.5 text-right font-medium">Delivered</th>
                        <th className="px-2.5 py-1.5 text-right font-medium">Inbox</th>
                        <th className="px-2.5 py-1.5 text-right font-medium">Tabs</th>
                        <th className="px-2.5 py-1.5 text-right font-medium">Spam</th>
                        <th className="px-2.5 py-1.5 text-right font-medium">Missing</th>
                        <th className="px-2.5 py-1.5 text-right font-medium">Inbox rate</th>
                    </tr>
                </thead>
                <tbody>
                    {families.map((f) => (
                        <tr key={f.family} className="border-t border-border tabular-nums">
                            <td className="px-2.5 py-1.5">{f.label}</td>
                            <td className="px-2.5 py-1.5 text-right">{f.counts.delivered}</td>
                            <td className="px-2.5 py-1.5 text-right">{f.counts.inbox}</td>
                            <td className="px-2.5 py-1.5 text-right">{f.counts.promotions + f.counts.other}</td>
                            <td className={cn("px-2.5 py-1.5 text-right", f.counts.spam > 0 && "text-red-700")}>{f.counts.spam}</td>
                            <td className={cn("px-2.5 py-1.5 text-right", f.counts.missing > 0 && "text-amber-700")}>
                                {f.counts.missing}
                            </td>
                            <td className="px-2.5 py-1.5 text-right font-medium">{pct(f.counts.inbox_rate)}</td>
                        </tr>
                    ))}
                </tbody>
            </table>
        </div>
    );
}

function ResultsTable({
    results,
}: {
    results: { seed: string; family_label: string; folder: string; sent_at: string | null; detected_at: string | null; error?: string }[];
}) {
    if (results.length === 0) {
        return <p className="text-xs text-muted-foreground">No copies were scheduled.</p>;
    }
    return (
        <div className="overflow-x-auto rounded-md border border-border">
            <table className="w-full text-xs">
                <thead className="bg-muted/40 text-[10px] uppercase tracking-wider text-muted-foreground">
                    <tr>
                        <th className="px-2.5 py-1.5 text-left font-medium">Seed</th>
                        <th className="px-2.5 py-1.5 text-left font-medium">Provider</th>
                        <th className="px-2.5 py-1.5 text-left font-medium">Folder</th>
                        <th className="px-2.5 py-1.5 text-left font-medium">Sent</th>
                        <th className="px-2.5 py-1.5 text-left font-medium">Found</th>
                    </tr>
                </thead>
                <tbody>
                    {results.map((r, i) => (
                        <tr key={`${r.seed}-${i}`} className="border-t border-border align-top">
                            <td className="px-2.5 py-1.5 font-mono">{r.seed}</td>
                            <td className="px-2.5 py-1.5">{r.family_label}</td>
                            <td className="px-2.5 py-1.5">
                                <FolderBadge folder={r.folder} />
                                {r.error && <div className="mt-0.5 max-w-56 text-[11px] text-red-700">{r.error}</div>}
                            </td>
                            <td className="px-2.5 py-1.5 whitespace-nowrap text-muted-foreground" title={absolute(r.sent_at)}>
                                {r.sent_at ? relative(r.sent_at) : "—"}
                            </td>
                            <td className="px-2.5 py-1.5 whitespace-nowrap text-muted-foreground" title={absolute(r.detected_at)}>
                                {r.detected_at ? relative(r.detected_at) : "—"}
                            </td>
                        </tr>
                    ))}
                </tbody>
            </table>
        </div>
    );
}
