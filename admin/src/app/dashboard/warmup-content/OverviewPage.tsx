// /warmup-content/overview — headline counts, AI/schedule status, per-pool
// library breakdown, and content-source vs spam-placement A/B comparison.

import { useMemo } from "react";
import { useQuery } from "@tanstack/react-query";
import { Archive, CalendarClock, Inbox, Play, Sparkles } from "lucide-react";
import { Skeleton } from "@/components/ui/skeleton";
import { ErrorState } from "@/components/ErrorState";
import {
    getWarmupContentAb,
    getWarmupContentOverview,
} from "@/lib/api/client/admin/warmupContent";
import { StatCard } from "./components";
import { fmtDate } from "./shared";

export default function OverviewPage() {
    const { data, isLoading, error, refetch } = useQuery({
        queryKey: ["admin", "warmup-content", "overview"],
        queryFn: getWarmupContentOverview,
        refetchInterval: 30_000,
    });

    const ab = useQuery({
        queryKey: ["admin", "warmup-content", "ab", 14],
        queryFn: () => getWarmupContentAb(14),
        staleTime: 60_000,
    });

    // Content is one shared library now — pools only isolate mailbox
    // reputation, not content. Aggregate the per-pool breakdown by
    // segment+source so the table reflects the actual library shape rather
    // than misleading per-pool rows (e.g. "free has no content").
    const bySegmentSource = useMemo(() => {
        const acc = new Map<
            string,
            { segment: string; source: string; active: number; archived: number }
        >();
        for (const p of data?.by_pool ?? []) {
            const key = `${p.segment}::${p.source}`;
            const cur = acc.get(key);
            if (cur) {
                cur.active += p.active;
                cur.archived += p.archived;
            } else {
                acc.set(key, {
                    segment: p.segment,
                    source: p.source,
                    active: p.active,
                    archived: p.archived,
                });
            }
        }
        return Array.from(acc.values());
    }, [data?.by_pool]);

    if (isLoading) {
        return (
            <div className="grid gap-3 md:grid-cols-5">
                {Array.from({ length: 5 }).map((_, i) => (
                    <Skeleton key={i} className="h-24" />
                ))}
            </div>
        );
    }
    if (error) {
        return (
            <ErrorState
                error={error}
                title="Failed to load overview"
                onRetry={() => refetch()}
            />
        );
    }
    if (!data) return null;

    return (
        <div className="space-y-6">
            <div className="grid gap-3 md:grid-cols-5">
                <StatCard
                    icon={<Inbox className="size-4" />}
                    title="Active threads"
                    value={(data.total_active ?? 0).toLocaleString()}
                    hint="available to warmup sends"
                />
                <StatCard
                    icon={<Archive className="size-4" />}
                    title="Archived"
                    value={(data.total_archived ?? 0).toLocaleString()}
                    hint="retired from rotation"
                />
                <StatCard
                    icon={<Sparkles className="size-4" />}
                    title="AI generation"
                    value={data.ai_enabled ? "Enabled" : "Disabled"}
                    tone={data.ai_enabled ? "text-emerald-600" : "text-muted-foreground"}
                    hint="master generation toggle"
                />
                <StatCard
                    icon={<CalendarClock className="size-4" />}
                    title="Schedule"
                    value={data.schedule_enabled ? "On" : "Off"}
                    tone={
                        data.schedule_enabled ? "text-emerald-600" : "text-muted-foreground"
                    }
                    hint="automatic top-up jobs"
                />
                <StatCard
                    icon={<Play className="size-4" />}
                    title="Last generated"
                    value={
                        data.last_generated_at
                            ? new Date(data.last_generated_at).toLocaleDateString()
                            : "Never"
                    }
                    hint={
                        data.last_generated_at
                            ? fmtDate(data.last_generated_at)
                            : "no jobs yet"
                    }
                />
            </div>

            <section>
                <h2 className="mb-2 text-sm font-semibold">Library by segment & source</h2>
                <div className="overflow-hidden rounded-lg border border-border bg-card">
                    <table className="w-full text-sm">
                        <thead className="bg-muted/50 text-xs uppercase text-muted-foreground">
                            <tr>
                                <th className="px-3 py-2 text-left font-medium">Segment</th>
                                <th className="px-3 py-2 text-left font-medium">Source</th>
                                <th className="px-3 py-2 text-right font-medium">Active</th>
                                <th className="px-3 py-2 text-right font-medium">Archived</th>
                            </tr>
                        </thead>
                        <tbody>
                            {bySegmentSource.map((p, i) => (
                                <tr
                                    key={`${p.segment}-${p.source}-${i}`}
                                    className="border-t border-border"
                                >
                                    <td className="px-3 py-2 text-xs">{p.segment || "—"}</td>
                                    <td className="px-3 py-2 text-xs text-muted-foreground">
                                        {p.source || "—"}
                                    </td>
                                    <td className="px-3 py-2 text-right tabular-nums text-emerald-600">
                                        {p.active.toLocaleString()}
                                    </td>
                                    <td className="px-3 py-2 text-right tabular-nums text-muted-foreground">
                                        {p.archived.toLocaleString()}
                                    </td>
                                </tr>
                            ))}
                            {bySegmentSource.length === 0 && (
                                <tr>
                                    <td
                                        colSpan={4}
                                        className="py-6 text-center text-sm text-muted-foreground"
                                    >
                                        No content generated yet.
                                    </td>
                                </tr>
                            )}
                        </tbody>
                    </table>
                </div>
            </section>

            <section>
                <h2 className="mb-2 text-sm font-semibold">
                    Content source vs spam placement
                    {ab.data ? (
                        <span className="ml-2 text-xs font-normal text-muted-foreground">
                            last {ab.data.window_days} days
                        </span>
                    ) : null}
                </h2>
                {ab.error ? (
                    <ErrorState
                        error={ab.error}
                        title="Failed to load A/B comparison"
                        onRetry={() => ab.refetch()}
                    />
                ) : ab.isLoading ? (
                    <Skeleton className="h-24" />
                ) : (
                    <div className="overflow-hidden rounded-lg border border-border bg-card">
                        <table className="w-full text-sm">
                            <thead className="bg-muted/50 text-xs uppercase text-muted-foreground">
                                <tr>
                                    <th className="px-3 py-2 text-left font-medium">Source</th>
                                    <th className="px-3 py-2 text-right font-medium">Sent</th>
                                    <th className="px-3 py-2 text-right font-medium">
                                        Spam placements
                                    </th>
                                    <th className="px-3 py-2 text-right font-medium">
                                        Placement rate
                                    </th>
                                </tr>
                            </thead>
                            <tbody>
                                {(ab.data?.data ?? []).map((r) => {
                                    // Backend already returns a percent (it
                                    // multiplies by 100), so use it directly.
                                    const pct = r.spam_placement_rate ?? 0;
                                    const tone =
                                        pct >= 20
                                            ? "text-red-700"
                                            : pct >= 10
                                              ? "text-amber-700"
                                              : "text-emerald-600";
                                    return (
                                        <tr
                                            key={r.content_source}
                                            className="border-t border-border"
                                        >
                                            <td className="px-3 py-2 text-xs">
                                                {r.content_source}
                                            </td>
                                            <td className="px-3 py-2 text-right tabular-nums">
                                                {r.sent.toLocaleString()}
                                            </td>
                                            <td className="px-3 py-2 text-right tabular-nums">
                                                {r.spam_placements.toLocaleString()}
                                            </td>
                                            <td
                                                className={`px-3 py-2 text-right tabular-nums ${tone}`}
                                            >
                                                {pct.toFixed(2)}%
                                            </td>
                                        </tr>
                                    );
                                })}
                                {(ab.data?.data ?? []).length === 0 && (
                                    <tr>
                                        <td
                                            colSpan={4}
                                            className="py-6 text-center text-sm text-muted-foreground"
                                        >
                                            Not enough delivery data yet.
                                        </td>
                                    </tr>
                                )}
                            </tbody>
                        </table>
                    </div>
                )}
            </section>
        </div>
    );
}
