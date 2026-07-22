// /warmup-content/overview — the automation status panel for the warmup
// content library: pipeline readiness (configured → enabled → scheduled),
// library stock vs the scheduler's targets, today's generation budget,
// headline counts, and the content-source vs spam-placement A/B comparison.

import { useMemo } from "react";
import { useQuery } from "@tanstack/react-query";
import {
    Archive,
    CheckCircle2,
    CircleAlert,
    CircleDashed,
    Inbox,
    Play,
} from "lucide-react";
import { Skeleton } from "@/components/ui/skeleton";
import { ErrorState } from "@/components/ErrorState";
import {
    getWarmupContentAb,
    getWarmupContentOverview,
    type WarmupContentOverview,
} from "@/lib/api/client/admin/warmupContent";
import { StatCard } from "./components";
import { fmtDate } from "./shared";

interface PipelineStep {
    label: string;
    ok: boolean;
    detail: string;
}

// The controller is always enabled. The only external dependency is an AI
// provider; without one, reviewed static content keeps sends operational.
function pipelineSteps(d: WarmupContentOverview): PipelineStep[] {
    const totalTarget = d.stock.reduce((n, s) => n + s.target, 0);
    const totalStock = d.stock.reduce((n, s) => n + Math.min(s.active, s.target), 0);
    const stocked = totalTarget > 0 && totalStock >= totalTarget;
    return [
        {
            label: "AI client configured",
            ok: d.ai_configured,
            detail: d.ai_configured
                ? "OPENAI_API_KEY is set on the backend"
                : "Set OPENAI_API_KEY on the backend, then restart it",
        },
        {
            label: "AI content enabled",
            ok: d.ai_enabled,
            detail: d.ai_enabled
                ? `${d.ai_selection_share}% of warmup sends draw AI content`
                : "Static fallback is active",
        },
        {
            label: "Scheduled top-up",
            ok: d.schedule_enabled,
            detail: d.schedule_enabled
                ? `Tops the library up every ${d.cadence_hours}h`
                : "Static fallback is active",
        },
        {
            label: "Library stocked",
            ok: stocked,
            detail:
                totalTarget > 0
                    ? `${totalStock.toLocaleString()} of ${totalTarget.toLocaleString()} target threads active`
                    : "Waiting for demand history",
        },
    ];
}

function StepIcon({ ok, blocked }: { ok: boolean; blocked: boolean }) {
    if (ok) return <CheckCircle2 className="size-4 shrink-0 text-emerald-600" />;
    if (blocked) return <CircleDashed className="size-4 shrink-0 text-muted-foreground" />;
    return <CircleAlert className="size-4 shrink-0 text-amber-600" />;
}

function AutomationPanel({ data }: { data: WarmupContentOverview }) {
    const steps = pipelineSteps(data);
    const allOk = steps.every((s) => s.ok);
    const firstGap = steps.findIndex((s) => !s.ok);
    const capped = data.daily_generation_cap > 0;
    const budgetUsed = capped
        ? Math.min(100, Math.round((data.generated_today / data.daily_generation_cap) * 100))
        : 0;

    return (
        <section className="rounded-lg border border-border bg-card p-4">
            <div className="flex flex-wrap items-center justify-between gap-2">
                <div>
                    <h2 className="text-sm font-semibold">Automatic extension</h2>
                    <p className="mt-0.5 text-[11px] text-muted-foreground">
                        {allOk
                            ? data.refresh_enabled
                                ? "The library extends itself: every run generates new threads, humanizes and lints them, and recycles the most-used ones so fresh content keeps flowing indefinitely."
                                : "The library tops itself up to the target. Continuous refresh is off, so generation pauses once the target is reached."
                            : "Not fully automatic yet — fix the first amber step below and the library will keep itself stocked without manual runs."}
                    </p>
                </div>
                <span className="rounded-full bg-emerald-50 px-2.5 py-1 text-[11px] font-medium text-emerald-700">
                    Autopilot
                </span>
            </div>

            <ol className="mt-3 grid gap-2 sm:grid-cols-2 xl:grid-cols-4">
                {steps.map((s, i) => (
                    <li
                        key={s.label}
                        className={`flex items-start gap-2 rounded-md border px-3 py-2 ${
                            s.ok
                                ? "border-border"
                                : i === firstGap
                                  ? "border-amber-300 bg-amber-50/50"
                                  : "border-border opacity-70"
                        }`}
                    >
                        <StepIcon ok={s.ok} blocked={!s.ok && i !== firstGap} />
                        <div className="min-w-0">
                            <div className="text-xs font-medium">
                                {i + 1}. {s.label}
                            </div>
                            <div className="mt-0.5 text-[11px] text-muted-foreground">
                                {s.detail}
                            </div>
                        </div>
                    </li>
                ))}
            </ol>

            <div className="mt-3 flex flex-wrap items-center gap-x-6 gap-y-2 text-[11px] text-muted-foreground">
                <div className="flex items-center gap-2">
                    <span className="font-medium text-foreground">Today's budget:</span>
                    {capped ? (
                        <>
                            <span className="tabular-nums">
                                {data.generated_today.toLocaleString()} /{" "}
                                {data.daily_generation_cap.toLocaleString()} threads
                            </span>
                            <span className="inline-block h-1.5 w-24 overflow-hidden rounded-full bg-muted">
                                <span
                                    className={`block h-full rounded-full ${
                                        budgetUsed >= 100 ? "bg-amber-500" : "bg-emerald-500"
                                    }`}
                                    style={{ width: `${budgetUsed}%` }}
                                />
                            </span>
                        </>
                    ) : (
                        <span>
                            uncapped ({data.generated_today.toLocaleString()} generated today)
                        </span>
                    )}
                </div>
                <div className="flex items-center gap-2">
                    <span className="font-medium text-foreground">Continuous refresh:</span>
                    {data.refresh_enabled ? (
                        <span>
                            on — recycles the {data.refresh_per_run} most-used threads each run
                        </span>
                    ) : (
                        <span>static fallback active</span>
                    )}
                </div>
                <div>
                    Generated threads are humanized, lint-gated, and any send that fails the
                    gate falls back to the static library. Threads with a meaningful sample and
                    unsafe spam placement are archived automatically.
                </div>
            </div>
        </section>
    );
}

function StockTable({ data }: { data: WarmupContentOverview }) {
    if (data.stock.length === 0) return null;
    return (
        <section>
            <h2 className="mb-2 text-sm font-semibold">Stock vs target</h2>
            <div className="overflow-hidden rounded-lg border border-border bg-card">
                <table className="w-full text-sm">
                    <thead className="bg-muted/50 text-xs uppercase text-muted-foreground">
                        <tr>
                            <th className="px-3 py-2 text-left font-medium">Segment</th>
                            <th className="px-3 py-2 text-right font-medium">Daily demand</th>
                            <th className="px-3 py-2 text-right font-medium">Active</th>
                            <th className="px-3 py-2 text-right font-medium">Target</th>
                            <th className="px-3 py-2 text-left font-medium">Fill</th>
                        </tr>
                    </thead>
                    <tbody>
                        {data.stock.map((s) => {
                            const pct =
                                s.target > 0
                                    ? Math.min(100, Math.round((s.active / s.target) * 100))
                                    : 100;
                            const deficit = Math.max(0, s.target - s.active);
                            return (
                                <tr key={s.segment || "generic"} className="border-t border-border">
                                    <td className="px-3 py-2 text-xs">
                                        {s.segment || "generic"}
                                    </td>
                                    <td className="px-3 py-2 text-right tabular-nums text-muted-foreground">
                                        {s.average_daily_sends.toLocaleString()}
                                    </td>
                                    <td className="px-3 py-2 text-right tabular-nums">
                                        {s.active.toLocaleString()}
                                    </td>
                                    <td className="px-3 py-2 text-right tabular-nums text-muted-foreground">
                                        {s.target.toLocaleString()}
                                    </td>
                                    <td className="px-3 py-2">
                                        <div className="flex items-center gap-2">
                                            <span className="inline-block h-1.5 w-28 overflow-hidden rounded-full bg-muted">
                                                <span
                                                    className={`block h-full rounded-full ${
                                                        pct >= 100
                                                            ? "bg-emerald-500"
                                                            : pct >= 50
                                                              ? "bg-sky-500"
                                                              : "bg-amber-500"
                                                    }`}
                                                    style={{ width: `${pct}%` }}
                                                />
                                            </span>
                                            <span className="text-[11px] tabular-nums text-muted-foreground">
                                                {deficit > 0
                                                    ? `${deficit.toLocaleString()} short`
                                                    : "at target"}
                                            </span>
                                        </div>
                                    </td>
                                </tr>
                            );
                        })}
                    </tbody>
                </table>
            </div>
            <p className="mt-1.5 text-[11px] text-muted-foreground">
                The target is calculated from the last seven days of total warmup sends. The
                controller keeps at least 200 shared threads, expands the bank automatically,
                and submits at most 250 new threads in one batch.
            </p>
        </section>
    );
}

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
            <div className="space-y-3">
                <Skeleton className="h-40" />
                <div className="grid gap-3 md:grid-cols-3">
                    {Array.from({ length: 3 }).map((_, i) => (
                        <Skeleton key={i} className="h-24" />
                    ))}
                </div>
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
            <AutomationPanel data={data} />

            <div className="grid gap-3 md:grid-cols-3">
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

            <StockTable data={data} />

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
                                        No generated content yet. The controller will submit a
                                        batch automatically; static content is active meanwhile.
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
                <p className="mb-2 text-[11px] text-muted-foreground">
                    Compares how often generated and static warmup mail lands in spam. The
                    library remains reviewable so unsafe generated content can be archived.
                </p>
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
