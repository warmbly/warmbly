import { useEffect, useState } from "react";
import { Link } from "react-router-dom";
import { Activity, AlertTriangle, RefreshCw } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Skeleton } from "@/components/ui/skeleton";
import { Callout, EmptyState, StatusBadge } from "@/components/ui/kit";
import { useInstanceMonitoring } from "@/hooks/useInstanceMonitoring";
import { useMe } from "@/hooks/useMe";
import { AdminPerm } from "@/lib/auth/permissions";
import type { MonitoringMetric, MonitoringSource } from "@/lib/api/client/admin/monitoring";
import {
    ageLabel, canMonitor, CONDITION_LABELS, investigationPath, isBrokerQueue,
    MONITORING_SOURCES, monitoringReason, needsMonitoringAttention, observationIsOld,
    permittedSource, timestamp,
} from "@/lib/monitoring";
import type { Tone } from "@/lib/tones";

function ObservationTime({ label, value, now }: { label: string; value: string | null; now: number }) {
    const at = timestamp(value);
    return (
        <span className="break-words">
            {label}: {at === null ? "unknown" : <time dateTime={value!} title={new Date(at).toISOString()}>{new Date(at).toLocaleString()} ({ageLabel(at, now)})</time>}
        </span>
    );
}

function metricIsStale(metric: MonitoringMetric, source: MonitoringSource, now: number): boolean {
    return metric.availability === "stale" || source.availability === "stale" ||
        observationIsOld(metric.observed_at, now) || observationIsOld(source.observed_at, now);
}

function metricTone(metric: MonitoringMetric, stale: boolean): Tone {
    if (metric.availability === "unavailable") return "neutral";
    if (stale) return "warning";
    if (needsMonitoringAttention(metric)) return isBrokerQueue(metric) ? "info" : metric.severity === "error" ? "danger" : "warning";
    if (metric.condition === "healthy") return "success";
    return "info";
}

function MetricRow({ metric: m, source, mask, now, compact }: {
    metric: MonitoringMetric; source: MonitoringSource; mask: number | undefined; now: number; compact?: boolean;
}) {
    const stale = metricIsStale(m, source, now);
    const unavailable = m.availability === "unavailable";
    const path = investigationPath(m.investigate, mask);
    const measured = !unavailable && typeof m.count === "number";
    return (
        <li className="min-w-0 space-y-2 px-4 py-3">
            <div className="flex flex-wrap items-start justify-between gap-2">
                <h4 className="min-w-0 break-words text-[13px] font-medium">{m.title}</h4>
                <div className="flex flex-wrap gap-1.5">
                    <StatusBadge tone={!measured ? "neutral" : metricTone(m, stale)}>{unavailable ? "Unavailable" : !measured ? "Unknown" : CONDITION_LABELS[m.condition] ?? "Unknown"}</StatusBadge>
                    {!unavailable && stale && <StatusBadge tone="warning">Stale observation</StatusBadge>}
                    {isBrokerQueue(m) && <StatusBadge tone="info">Queue measurement, not outage</StatusBadge>}
                </div>
            </div>
            {compact && <p className="text-xs text-muted-foreground">{MONITORING_SOURCES[source.id]?.title ?? "Unrecognized source"}</p>}
            <div className="flex flex-wrap gap-x-4 gap-y-1 text-[13px] tabular-nums">
                <span className="font-medium">{measured ? `${m.count!.toLocaleString()} ${m.unit}` : "Not measured"}</span>
                {!unavailable && m.affected_mailboxes !== null && <span>{m.affected_mailboxes.toLocaleString()} affected mailboxes</span>}
                {!unavailable && m.affected_organizations !== null && <span>{m.affected_organizations.toLocaleString()} affected organizations</span>}
                {!unavailable && m.unknown_organization_rows !== null && m.unknown_organization_rows > 0 && <span>{m.unknown_organization_rows.toLocaleString()} rows with unknown organization</span>}
            </div>
            {m.reason && <p className="text-xs text-muted-foreground">{monitoringReason(m.reason)}</p>}
            {m.note && <p className="break-words text-xs leading-relaxed text-muted-foreground">{m.note}</p>}
            <div className="flex flex-wrap gap-x-4 gap-y-1 text-xs text-muted-foreground">
                <ObservationTime label="Observed" value={m.observed_at} now={now} />
                <ObservationTime label="Evidence since" value={m.evidence_at} now={now} />
                {m.latest_evidence_at && <ObservationTime label="Latest evidence" value={m.latest_evidence_at} now={now} />}
                {m.next_eligible_at && <ObservationTime label="Next eligible" value={m.next_eligible_at} now={now} />}
            </div>
            {!compact && <div className="flex flex-wrap gap-x-4 gap-y-1 text-xs text-muted-foreground">
                {m.evidence_age_seconds !== null && <span>Evidence age reported by snapshot: {m.evidence_age_seconds.toLocaleString()}s</span>}
                {m.threshold_seconds !== null && <span>Monitoring threshold: {m.threshold_seconds.toLocaleString()}s (not a provider SLA)</span>}
                {m.window_start && <ObservationTime label="Window start" value={m.window_start} now={now} />}
                {m.window_end && <ObservationTime label="Window end" value={m.window_end} now={now} />}
            </div>}
            {path && <Link to={path} className="inline-flex text-xs font-medium text-[var(--admin-accent-strong)] hover:underline">Investigate {m.title}</Link>}
        </li>
    );
}

function SourcePanel({ source, mask, now }: { source: MonitoringSource; mask: number | undefined; now: number }) {
    const unavailable = source.availability === "unavailable";
    const stale = !unavailable && (source.availability === "stale" || observationIsOld(source.observed_at, now));
    return (
        <section className="min-w-0 overflow-hidden rounded-lg border border-border bg-card" aria-label={MONITORING_SOURCES[source.id]?.title ?? "Unrecognized source"}>
            <div className="space-y-2 border-b border-border px-4 py-3">
                <div className="flex flex-wrap items-center justify-between gap-2">
                    <h3 className="text-sm font-medium">{MONITORING_SOURCES[source.id]?.title ?? "Unrecognized source"}</h3>
                    <StatusBadge tone={unavailable ? "neutral" : stale || source.coverage !== "complete" ? "warning" : "info"}>
                        {unavailable ? "Unavailable" : stale ? "Stale" : "Fresh"} / {source.coverage === "complete" ? "complete" : "incomplete"} coverage
                    </StatusBadge>
                </div>
                <div className="flex flex-wrap gap-x-4 gap-y-1 text-xs text-muted-foreground">
                    <ObservationTime label="Source observed" value={source.observed_at} now={now} />
                    <ObservationTime label="Collection attempted" value={source.checked_at} now={now} />
                    {source.measured_scopes !== null && <span>{source.measured_scopes.toLocaleString()} / {source.expected_scopes?.toLocaleString() ?? "unknown"} scopes measured</span>}
                </div>
                {source.reason && <p className="text-xs text-muted-foreground">{monitoringReason(source.reason)}</p>}
                {source.coverage !== "complete" && !unavailable && <p className="text-xs text-muted-foreground">Incomplete coverage. Measured counts may be lower bounds; do not sum overlapping metrics.</p>}
            </div>
            {(unavailable || source.metrics.length === 0) && (
                <p className="px-4 py-4 text-[13px] text-muted-foreground">{unavailable ? "No current measurement available. This is not zero backlog or evidence of an outage." : "No metrics returned. Operational health is unknown for this source."}</p>
            )}
            {source.metrics.length > 0 && (
                <ul className="divide-y divide-border">{source.metrics.map((m, i) => <MetricRow key={`${m.id}:${i}`} metric={unavailable ? { ...m, availability: "unavailable", observed_at: null } : m} source={source} mask={mask} now={now} />)}</ul>
            )}
        </section>
    );
}

export function InstanceMonitoringPanel({ compact = false }: { compact?: boolean }) {
    const { data: me } = useMe();
    const query = useInstanceMonitoring();
    const [, setClockTick] = useState(0);
    useEffect(() => {
        const timer = setInterval(() => setClockTick((tick) => tick + 1), 30_000);
        return () => clearInterval(timer);
    }, []);
    const now = Date.now();
    const mask = me?.admin_permissions;
    const allowed = canMonitor(mask, AdminPerm.ViewAnalytics);
    const data = allowed ? query.data : undefined;
    const sources = data?.sources.map((source) => {
        const permitted = permittedSource(source, mask);
        return query.isError && permitted.availability === "fresh" ? { ...permitted, availability: "stale" as const } : permitted;
    }) ?? [];
    const attention = sources.flatMap((source) => source.availability === "unavailable" ? [] : source.metrics.filter(needsMonitoringAttention).map((metric) => ({ source, metric })));
    const queues = attention.filter(({ metric }) => isBrokerQueue(metric));
    const issues = attention.filter(({ metric }) => !isBrokerQueue(metric));
    const gaps = sources.filter((s) => s.availability === "unavailable" || s.coverage !== "complete" || s.metrics.length === 0 || s.metrics.some((m) => m.availability === "unavailable" || m.count === null));
    const stale = sources.filter((s) => s.availability === "stale" || s.availability !== "unavailable" && (observationIsOld(s.observed_at, now) || s.metrics.some((m) => metricIsStale(m, s, now))));
    const unknown = sources.filter((s) => s.metrics.some((m) => ["unknown", "no_recent_evidence"].includes(m.condition)));
    const missing = data?.version === "1" ? Object.keys(MONITORING_SOURCES).filter((id) => !sources.some((s) => s.id === id)).length : 0;
    const refreshAt = timestamp(data?.refresh_after);
    const waiting = !query.isError && refreshAt !== null && refreshAt > now;
    const unsupported = data && data.version !== "1";
    return (
        <section className={compact ? "mb-8 space-y-3" : "space-y-4"} aria-label="Operational monitoring">
            <div className="flex flex-wrap items-center justify-between gap-3">
                <h2 className="flex items-center gap-2 text-sm font-semibold"><Activity className="size-4" />Operational monitoring</h2>
                <div className="flex flex-wrap items-center gap-3">
                    {compact && allowed && <Link to="/health?tab=operations" className="text-xs font-medium text-[var(--admin-accent-strong)] hover:underline">All operational details</Link>}
                    {allowed && <Button size="sm" variant="outline" onClick={() => query.refetch()} disabled={query.isFetching || waiting} title={waiting ? "The backend reuses the snapshot until its refresh time" : undefined}>
                        <RefreshCw className={query.isFetching ? "size-3.5 animate-spin" : "size-3.5"} />{query.isFetching ? "Refreshing..." : "Refresh monitoring"}
                    </Button>}
                </div>
            </div>
            {!allowed && <Callout tone="neutral" title="Monitoring permission required">Analytics access and a known permission mask are required. No operational measurements were requested.</Callout>}
            {allowed && query.isLoading && <div role="status" aria-label="Loading operational monitoring"><Skeleton className="h-24 w-full" /><span className="sr-only">Loading operational monitoring</span></div>}
            {allowed && query.isError && <Callout tone="warning" icon={AlertTriangle} title="Monitoring refresh failed">{data ? "Showing the previous snapshot with its original timestamps, not a fresh health verdict." : "Operational measurements are unavailable. An empty or failed request is not an all-clear. Verify access or retry."}</Callout>}
            {unsupported && <Callout tone="warning" title="Unsupported monitoring version">This admin release cannot interpret this snapshot. Update the admin frontend; health remains unknown.</Callout>}
            {data && !unsupported && <>
                <div className="flex flex-wrap gap-x-4 gap-y-1 text-xs text-muted-foreground">
                    <ObservationTime label="Snapshot checked" value={data.checked_at} now={now} />
                    <ObservationTime label="Refresh eligible" value={data.refresh_after} now={now} />
                </div>
                {sources.length === 0 ? <EmptyState title="No monitoring sources returned" hint="Coverage is unknown. An empty response does not establish health or zero backlog." /> : <>
                    <Callout tone={gaps.length || stale.length || missing || data.coverage !== "complete" ? "warning" : "info"} title={`${attention.length} recorded attention signals`}>
                        {gaps.length} sources with coverage gaps; {stale.length} stale; {unknown.length} with unknown or no recent evidence{missing > 0 ? `; ${missing} expected sources not returned` : ""}. Snapshot coverage: {data.coverage === "complete" ? "complete for collected sources" : "incomplete"}.
                        {attention.length === 0 && " No attention signals in measured sources. This is not an instance-wide all-clear."}
                        {stale.length > 0 && " Stale observations are historical, not proof of a current failure."}
                    </Callout>
                    <p className="text-xs leading-relaxed text-muted-foreground">Counts are per metric and may overlap. Scheduled, retry, budget and daily-limit waits are informational; safety holds are restrictions, not provider outages. Heartbeats, completed tasks and sync ticks do not prove sends or recovery.</p>
                    {compact ? <>
                        {queues.length > 0 && <section className="overflow-hidden rounded-lg border border-border bg-card" aria-label="Positive broker queue measurements">
                            <h3 className="px-4 pt-3 text-sm font-medium">{queues.length} positive broker queue measurements</h3>
                            <p className="px-4 py-2 text-xs text-muted-foreground">Observed pending or lag counts, not a proven outage or delivery rate. Separate scopes and metrics must not be added together.</p>
                            <ul className="max-h-96 divide-y divide-border overflow-auto">{queues.map(({ source, metric }, i) => <MetricRow key={`${source.id}:${metric.id}:${i}`} metric={metric} source={source} mask={mask} now={now} compact />)}</ul>
                        </section>}
                        {issues.length > 0 && <div className="overflow-hidden rounded-lg border border-border bg-card"><ul className="divide-y divide-border">{issues.slice(0, 6).map(({ source, metric }, i) => <MetricRow key={`${source.id}:${metric.id}:${i}`} metric={metric} source={source} mask={mask} now={now} compact />)}</ul></div>}
                        {issues.length > 6 && <p className="text-xs text-muted-foreground">{issues.length - 6} more signals in operational details.</p>}
                    </> : <div className="grid grid-cols-1 items-start gap-4 xl:grid-cols-2">{sources.map((source) => <SourcePanel key={source.id} source={source} mask={mask} now={now} />)}</div>}
                </>}
            </>}
        </section>
    );
}
