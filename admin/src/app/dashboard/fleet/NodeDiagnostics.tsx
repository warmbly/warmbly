import { useQuery } from "@tanstack/react-query";
import { Link } from "react-router-dom";
import { FileText, RefreshCw } from "lucide-react";
import { ErrorState } from "@/components/ErrorState";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Skeleton } from "@/components/ui/skeleton";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { EmptyState, Panel, Property, PropertyList, Section, StatusBadge } from "@/components/ui/kit";
import { useMe } from "@/hooks/useMe";
import useBrowseState from "@/hooks/useBrowseState";
import { AdminPerm, hasAdminPerm } from "@/lib/auth/permissions";
import type { FleetNode } from "@/lib/api/client/admin/fleetNodes";
import type { AdminWorkerEmail } from "@/lib/api/models/admin";
import { getNodeLogs, getNodeBroker, type NodeBrokerSource, type NodeLogFilters } from "@/lib/api/client/admin/nodeDiagnostics";
import { diagnosticReason, emptyLogFilters, eventLabels, isUUID, logFilterError, parseLogFilters, safeCount, safeLogEvent, validTime, validatedBroker } from "@/lib/nodeDiagnostics";

function Timestamp({ value }: { value?: string | null }) {
    return validTime(value) ? <time dateTime={value}>{new Date(value).toLocaleString()}</time> : <>Not observed</>;
}
const count = (value: unknown) => safeCount(value) ? value.toLocaleString() : "Unknown";

function SourceBadge({ availability }: { availability?: string }) {
    return <StatusBadge tone={availability === "fresh" ? "neutral" : "warning"}>{availability === "fresh" ? "Fresh observation" : availability === "stale" ? "Stale observation" : "Unavailable observation"}</StatusBadge>;
}

function BrokerScope({ title, source, group, topic }: { title: string; source: NodeBrokerSource; group: string; topic: string }) {
    const observation = validatedBroker(source, group, topic);
    const coverage = ["complete", "partial", "unavailable"].includes(source.coverage) ? source.coverage : "unknown";
    return <Panel title={title}>
        <SourceBadge availability={source.availability} />
        <PropertyList className="mt-3">
            <Property label="Consumer group"><span className="font-mono text-xs break-all">{group}</span></Property>
            <Property label="Topic"><span className="font-mono text-xs break-all">{topic}</span></Property>
            <Property label="Coverage">{coverage} ({count(source.measured_scopes)} / {count(source.expected_scopes)} scopes)</Property>
            <Property label="Checked"><Timestamp value={source.checked_at} /></Property>
            <Property label="Observed"><Timestamp value={source.observed_at} /></Property>
            <Property label="Committed lag"><span className="font-mono">{observation ? `${observation.lag.toLocaleString()} messages` : "Unknown / unavailable"}</span></Property>
            <Property label="Group members">{observation ? count(observation.members) : "Unknown / unavailable"}</Property>
        </PropertyList>
        <p className="mt-3 text-xs text-muted-foreground">{observation ? "Validated retained offsets at the observation time. Broker health and delivery remain unknown." : diagnosticReason(source.metrics?.find((m) => m.reason)?.reason ?? source.reason)}</p>
        {observation && <details className="mt-4 text-xs">
            <summary className="cursor-pointer font-medium">Validated partition offsets</summary>
            <div className="mt-2 overflow-x-auto">
                <table className="w-full text-left tabular-nums" aria-label={`${title} partition offsets`}>
                    <thead><tr className="border-b text-muted-foreground"><th className="p-2">Partition</th><th className="p-2">Earliest</th><th className="p-2">Committed</th><th className="p-2">Latest</th><th className="p-2">Lag</th></tr></thead>
                    <tbody>{observation.detail.partitions.map((p) => <tr key={p.partition} className="border-b"><td className="p-2">{p.partition}</td><td className="p-2">{p.earliest}</td><td className="p-2">{p.committed}</td><td className="p-2">{p.latest}</td><td className="p-2">{p.committed_lag}</td></tr>)}</tbody>
                </table>
            </div>
        </details>}
    </Panel>;
}

export function NodeDiagnostics({ node, mailboxes }: { node: FleetNode; mailboxes: AdminWorkerEmail[] }) {
    const { data: me } = useMe();
    const canViewBroker = !!me && hasAdminPerm(me.admin_permissions, AdminPerm.ViewAnalytics);
    const canViewMailboxes = !!me && hasAdminPerm(me.admin_permissions, AdminPerm.ViewUsers);
    const [filters, setFilters] = useBrowseState(`node-logs:${node.id}`, emptyLogFilters, parseLogFilters);
    const filterError = logFilterError(filters);
    const logs = useQuery({
        queryKey: ["admin", "node-diagnostics", node.id, "logs", filters],
        queryFn: () => getNodeLogs(node.id, filters),
        enabled: !filterError,
        retry: false, refetchInterval: 30_000,
    });
    const broker = useQuery({
        queryKey: ["admin", "node-diagnostics", node.id, "broker"],
        queryFn: () => getNodeBroker(node.id),
        enabled: node.role === "worker" && canViewBroker,
        retry: false, refetchInterval: 30_000,
    });
    const history = logs.data;
    const matchedHistory = history?.node_id === node.id ? history : undefined;
    const readable = !!matchedHistory && ["fresh", "stale"].includes(matchedHistory.availability) && matchedHistory.capture?.protocol === 1;
    const events = readable ? (matchedHistory.events ?? []).filter(safeLogEvent) : [];
    const hidden = readable && (matchedHistory.events?.length ?? 0) > events.length;
    const setFilter = (key: keyof NodeLogFilters, value: string) => setFilters((previous) => ({ ...previous, [key]: value }));

    return <>
        <Section title="Recent redacted node logs" description="Partial allowlisted evidence, not a full journal, mailbox health check or confirmation of delivery. Snapshots refresh every 30 seconds."
            actions={<Button size="xs" variant="ghost" disabled={logs.isFetching || !!filterError} onClick={() => void logs.refetch()}><RefreshCw className="size-3" />{logs.isFetching ? "Refreshing logs…" : "Refresh logs"}</Button>}>
            <Panel>
                <div className="grid gap-3 sm:grid-cols-2 xl:grid-cols-4">
                    <div className="space-y-1.5"><Label htmlFor="node-log-level">Log level</Label>
                        <Select value={filters.level || "all"} onValueChange={(v) => setFilter("level", v === "all" ? "" : v)}>
                            <SelectTrigger id="node-log-level"><SelectValue /></SelectTrigger>
                            <SelectContent><SelectItem value="all">All levels</SelectItem><SelectItem value="info">Info</SelectItem><SelectItem value="warn">Warn</SelectItem><SelectItem value="error">Error</SelectItem></SelectContent>
                        </Select>
                    </div>
                    <div className="space-y-1.5"><Label htmlFor="node-log-after">After (local time)</Label><Input id="node-log-after" type="datetime-local" value={filters.after} onChange={(e) => setFilter("after", e.target.value)} /></div>
                    <div className="space-y-1.5"><Label htmlFor="node-log-before">Before (local time)</Label><Input id="node-log-before" type="datetime-local" value={filters.before} onChange={(e) => setFilter("before", e.target.value)} /></div>
                    <div className="space-y-1.5"><Label htmlFor="node-log-mailbox">Mailbox UUID</Label><Input id="node-log-mailbox" placeholder="Any mailbox" maxLength={36} value={filters.mailbox} onChange={(e) => setFilter("mailbox", e.target.value)} /></div>
                </div>
                <div className="mt-3 flex flex-wrap items-center gap-3"><Button variant="ghost" size="xs" onClick={() => setFilters(emptyLogFilters)}>Clear log filters</Button><span className="text-xs text-muted-foreground">Inclusive time bounds; up to 200 newest matching events. Filters are saved for this admin and node in this browser tab.</span></div>
                {filterError && <p role="alert" className="mt-3 text-sm text-destructive">{filterError}</p>}
                {!filterError && logs.isLoading && <Skeleton className="mt-4 h-32 w-full" />}
                {logs.isError && !filterError && <ErrorState className="mt-4" error={logs.error} title="Node evidence could not be refreshed" onRetry={() => void logs.refetch()} />}
                {!filterError && history && !matchedHistory && <p role="alert" className="mt-4 text-sm">Evidence does not match this node. No evidence is displayed.</p>}
                {!filterError && matchedHistory && <>
                    {logs.isError && <p className="mt-3 text-xs text-muted-foreground">Last successful snapshot below. The latest refresh failed; it is not a current observation.</p>}
                    <div className="mt-5 flex flex-wrap gap-2"><SourceBadge availability={logs.isError ? "stale" : matchedHistory.availability} /><StatusBadge>{matchedHistory.coverage === "partial" ? "Partial coverage" : "Unavailable coverage"}</StatusBadge></div>
                    <p className="mt-3 text-sm text-muted-foreground">{diagnosticReason(matchedHistory.reason)}</p>
                    <PropertyList className="mt-3">
                        <Property label="Source">Node-agent allowlisted events via bounded Redis history. No historical journal import.</Property>
                        <Property label="Capture protocol">{matchedHistory.capture?.protocol === 1 ? "Protocol 1" : "Not observed or unsupported"}</Property>
                        <Property label="Snapshot queried"><Timestamp value={matchedHistory.observed_at} /></Property>
                        <Property label="Capture observed"><Timestamp value={matchedHistory.capture?.observed_at} /></Property>
                        <Property label="Capture received"><Timestamp value={matchedHistory.capture?.received_at} /></Property>
                        <Property label="Capture run"><span className="font-mono text-xs">{isUUID(matchedHistory.capture?.run_id ?? "") ? matchedHistory.capture?.run_id : "Not observed"}</span></Property>
                        <Property label="Run started"><Timestamp value={matchedHistory.capture?.started_at} /></Property>
                        <Property label="Known drops">{matchedHistory.capture ? count(matchedHistory.capture.dropped) : "Unknown"} (per process run, not total historical loss)</Property>
                        <Property label="Retention">Up to {count(matchedHistory.retention_seconds)} seconds and {count(matchedHistory.max_events)} events. Metadata expires separately.</Property>
                        <Property label="Retained window">{readable ? count(matchedHistory.retained_count) : "Unknown"} events across all filters. <Timestamp value={matchedHistory.oldest_retained_at} /> to <Timestamp value={matchedHistory.newest_retained_at} /></Property>
                    </PropertyList>
                    <p className="mt-3 text-xs text-muted-foreground">Pre-installation events, abrupt crashes and lost acknowledgements may be uncounted. Empty evidence never means zero errors or healthy mailboxes.</p>
                    {matchedHistory.truncated && <p role="status" className="mt-3 text-sm">More matching events are retained than this response contains. Narrow the filters to inspect the window.</p>}
                    {hidden && <p role="status" className="mt-3 text-sm">Unsupported or invalid event records were withheld.</p>}
                    {!readable ? <EmptyState icon={FileText} title="Log evidence unavailable" hint="No observed event count can be inferred. Check capture availability above." className="py-8" /> : events.length === 0 ?
                        <EmptyState icon={FileText} title="No retained events match these filters" hint="No matching evidence is not evidence of health. Retention and capture coverage still apply." className="py-8" /> :
                        <div className="mt-4 overflow-x-auto"><table className="w-full text-left text-xs" aria-label="Redacted node events">
                            <thead><tr className="border-b text-muted-foreground"><th className="p-2">Observed</th><th className="p-2">Level</th><th className="p-2">Evidence</th><th className="p-2">Mailbox</th></tr></thead>
                            <tbody>{events.map((event) => {
                                const mailbox = mailboxes.find((m) => m.id === event.mailbox_id);
                                const mailboxID = isUUID(event.mailbox_id ?? "") ? event.mailbox_id : null;
                                return <tr key={event.id} className="border-b align-top">
                                    <td className="whitespace-nowrap p-2"><Timestamp value={event.observed_at} /></td>
                                    <td className="p-2"><StatusBadge tone={event.level === "error" ? "danger" : event.level === "warn" ? "warning" : "neutral"}>{event.level}</StatusBadge></td>
                                    <td className="p-2"><div className="font-medium">{eventLabels[event.event].title}</div><div className="mt-1 text-muted-foreground">{event.category.replaceAll("_", " ")}
                                        {event.event === "sync_control_plane_held" && safeCount(event.http_status) && event.http_status >= 100 && event.http_status <= 599 && ` · HTTP ${event.http_status}`}
                                        {safeCount(event.count) && event.count <= 1000000 && ` · count ${event.count}`}</div></td>
                                    <td className="p-2 font-mono">{mailboxID ? mailbox && canViewMailboxes ? <Link className="text-[var(--admin-accent-strong)] underline underline-offset-2" to={`/mailboxes?q=${encodeURIComponent(mailbox.email)}&worker=${node.id}`} title="Open assigned mailbox in the mailbox browser">{mailboxID}</Link> : <span title="Mailbox identity is not in the currently loaded assignment page, or mailbox access is not permitted.">{mailboxID}</span> : "Not provided"}</td>
                                </tr>;
                            })}</tbody>
                        </table></div>}
                </>}
            </Panel>
        </Section>
        <Section title="Broker committed-offset observations" description="Committed lag is not send rate, processing throughput, provider health, confirmation of delivery or evidence of an outage. Command and shared result scopes must never be added together."
            actions={node.role === "worker" && canViewBroker ? <Button size="xs" variant="ghost" disabled={broker.isFetching} onClick={() => void broker.refetch()}><RefreshCw className="size-3" />{broker.isFetching ? "Refreshing broker…" : "Refresh broker"}</Button> : undefined}>
            {node.role !== "worker" ? <Panel>Command-group broker observations require a worker node. Consumer nodes use generic log evidence above; no worker lag is inferred for this consumer.</Panel> : !canViewBroker ? <Panel>View analytics permission is also required to read broker observations. Log access is unchanged.</Panel> : <>
                {broker.isLoading && <Skeleton className="h-48 w-full" />}
                {broker.isError && <ErrorState error={broker.error} title="Broker observation could not be refreshed" onRetry={() => void broker.refetch()} />}
                {!broker.isError && broker.data && (broker.data.node_id === node.id ? <>
                    <p className="mb-3 text-xs text-muted-foreground">Snapshot queried: <Timestamp value={broker.data.observed_at} />. Shared results belong to the result-consumer group, not this node's processing or delivery count.</p>
                    <div className="grid gap-4 xl:grid-cols-2">
                        <BrokerScope title="Worker command scope" source={broker.data.commands} group={`worker-${node.id}`} topic={`w.${node.id}`} />
                        <BrokerScope title="Shared result-consumer scope" source={broker.data.results} group="consumer-group" topic="jobs.worker-events" />
                    </div>
                </> : <p role="alert">Broker observation does not match this node. No metrics are displayed.</p>)}
            </>}
        </Section>
    </>;
}
