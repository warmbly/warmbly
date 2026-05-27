// Workers → Provisioning Jobs. Top panel = in-flight jobs (refreshed
// every 2s). Bottom panel = history with state + provider filters.
// Click a row for the timeline + retry button on failed jobs.

import { useMemo, useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Link } from "react-router-dom";
import { toast } from "sonner";
import {
    Activity,
    ArrowLeft,
    Filter,
    History,
    RefreshCw,
    Rocket,
} from "lucide-react";

import { PageHeader } from "@/components/layout/PageHeader";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import {
    Card,
    CardContent,
    CardDescription,
    CardHeader,
    CardTitle,
} from "@/components/ui/card";
import {
    Select,
    SelectContent,
    SelectItem,
    SelectTrigger,
    SelectValue,
} from "@/components/ui/select";
import { Skeleton } from "@/components/ui/skeleton";

import {
    getProvisioningJob,
    listProvisioningJobs,
    retryProvisioningJob,
} from "@/lib/api/client/admin/provisioning";
import type {
    ProvisioningJob,
    ProvisioningJobState,
} from "@/lib/api/models/admin";

import { JobProgressTimeline, ProvisionModal } from "./ProvisionModal";

const TERMINAL: Set<ProvisioningJobState> = new Set(["completed", "failed"]);

export default function ProvisioningJobsPage() {
    const qc = useQueryClient();
    const [selectedId, setSelectedId] = useState<string | null>(null);
    const [stateFilter, setStateFilter] = useState<string>("__all");
    const [providerFilter, setProviderFilter] = useState<string>("__all");
    const [provisionOpen, setProvisionOpen] = useState(false);

    // In-flight jobs - polled every 2s.
    const inflightQ = useQuery({
        queryKey: ["admin", "provisioning-jobs", "inflight"],
        queryFn: () => listProvisioningJobs({}),
        retry: false,
        refetchInterval: 2000,
    });

    // History list - last 30 days.
    const historyQ = useQuery({
        queryKey: [
            "admin",
            "provisioning-jobs",
            "history",
            stateFilter,
            providerFilter,
        ],
        queryFn: () =>
            listProvisioningJobs({
                state: stateFilter === "__all" ? undefined : stateFilter,
                provider: providerFilter === "__all" ? undefined : providerFilter,
                since_days: 30,
            }),
        retry: false,
        refetchInterval: 30_000,
    });

    const inflight = useMemo(
        () => (inflightQ.data ?? []).filter((j) => !TERMINAL.has(j.state)),
        [inflightQ.data],
    );
    const history = useMemo(
        () => (historyQ.data ?? []).filter((j) => TERMINAL.has(j.state)),
        [historyQ.data],
    );

    if (selectedId) {
        return (
            <JobDetail
                id={selectedId}
                onBack={() => setSelectedId(null)}
                onChanged={() =>
                    qc.invalidateQueries({
                        queryKey: ["admin", "provisioning-jobs"],
                    })
                }
            />
        );
    }

    return (
        <div>
            <Link
                to="/workers"
                className="inline-flex items-center gap-1 text-xs text-muted-foreground hover:text-foreground mb-2"
            >
                <ArrowLeft className="size-3" />
                All workers
            </Link>
            <PageHeader
                title="Provisioning Jobs"
                description="Every machine the platform has tried to create, in-flight or historical. Click a row for the timeline."
            >
                <Button
                    size="sm"
                    onClick={() => setProvisionOpen(true)}
                    className="bg-[var(--admin-accent)] hover:bg-[var(--admin-accent-strong)] text-white"
                >
                    <Rocket className="size-4" />
                    Provision new
                </Button>
            </PageHeader>

            <ProvisionModal
                open={provisionOpen}
                onOpenChange={setProvisionOpen}
                onJobCreated={() =>
                    qc.invalidateQueries({
                        queryKey: ["admin", "provisioning-jobs"],
                    })
                }
            />

            <Card className="mb-4">
                <CardHeader>
                    <CardTitle className="flex items-center gap-2">
                        <Activity className="size-4 text-[var(--admin-accent)]" />
                        In-flight
                    </CardTitle>
                    <CardDescription>
                        Currently provisioning. Refreshes every 2 seconds.
                    </CardDescription>
                </CardHeader>
                <CardContent className="pt-0">
                    {inflightQ.isLoading && <Skeleton className="h-16 w-full" />}
                    {!inflightQ.isLoading && inflight.length === 0 && (
                        <div className="text-xs text-muted-foreground py-2">
                            No provisioning jobs running right now.
                        </div>
                    )}
                    {inflight.length > 0 && (
                        <JobsTable
                            jobs={inflight}
                            onSelect={(id) => setSelectedId(id)}
                        />
                    )}
                </CardContent>
            </Card>

            <Card>
                <CardHeader>
                    <CardTitle className="flex items-center justify-between">
                        <span className="flex items-center gap-2">
                            <History className="size-4" />
                            History (last 30 days)
                        </span>
                        <div className="flex items-center gap-2">
                            <Filter className="size-3 text-muted-foreground" />
                            <Select value={stateFilter} onValueChange={setStateFilter}>
                                <SelectTrigger size="sm" className="w-32">
                                    <SelectValue />
                                </SelectTrigger>
                                <SelectContent>
                                    <SelectItem value="__all">All states</SelectItem>
                                    <SelectItem value="completed">Completed</SelectItem>
                                    <SelectItem value="failed">Failed</SelectItem>
                                </SelectContent>
                            </Select>
                            <Select
                                value={providerFilter}
                                onValueChange={setProviderFilter}
                            >
                                <SelectTrigger size="sm" className="w-36">
                                    <SelectValue />
                                </SelectTrigger>
                                <SelectContent>
                                    <SelectItem value="__all">All providers</SelectItem>
                                    <SelectItem value="hetzner">Hetzner</SelectItem>
                                </SelectContent>
                            </Select>
                        </div>
                    </CardTitle>
                </CardHeader>
                <CardContent className="pt-0">
                    {historyQ.isLoading && <Skeleton className="h-32 w-full" />}
                    {!historyQ.isLoading && history.length === 0 && (
                        <div className="text-xs text-muted-foreground py-3 text-center">
                            No historical jobs match these filters.
                            <div className="text-[11px] text-muted-foreground mt-1">
                                Backend endpoint
                                <code className="px-1">/admin/provisioning-jobs</code>
                                needs to be wired before history shows up.
                            </div>
                        </div>
                    )}
                    {history.length > 0 && (
                        <JobsTable
                            jobs={history}
                            onSelect={(id) => setSelectedId(id)}
                        />
                    )}
                </CardContent>
            </Card>
        </div>
    );
}

// --------------------------------------------------------------------
// Jobs table
// --------------------------------------------------------------------

const STATE_TONE: Record<ProvisioningJobState, string> = {
    pending: "bg-zinc-100 text-zinc-600",
    creating_server: "bg-amber-100 text-amber-700",
    creating_ips: "bg-amber-100 text-amber-700",
    assigning_ips: "bg-amber-100 text-amber-700",
    setting_rdns: "bg-amber-100 text-amber-700",
    installing: "bg-amber-100 text-amber-700",
    verifying: "bg-blue-100 text-blue-700",
    completed: "bg-emerald-100 text-emerald-700",
    failed: "bg-red-100 text-red-700",
};

function JobsTable({
    jobs,
    onSelect,
}: {
    jobs: ProvisioningJob[];
    onSelect: (id: string) => void;
}) {
    return (
        <div className="border border-border rounded-lg overflow-hidden bg-card">
            <table className="w-full text-sm">
                <thead className="bg-muted/50 text-muted-foreground text-xs uppercase">
                    <tr>
                        <th className="text-left px-3 py-2 font-medium">Job</th>
                        <th className="text-left px-3 py-2 font-medium">Template</th>
                        <th className="text-left px-3 py-2 font-medium">Provider</th>
                        <th className="text-left px-3 py-2 font-medium">Location</th>
                        <th className="text-left px-3 py-2 font-medium">Servers</th>
                        <th className="text-left px-3 py-2 font-medium">State</th>
                        <th className="text-left px-3 py-2 font-medium">Started</th>
                    </tr>
                </thead>
                <tbody>
                    {jobs.map((j) => (
                        <tr
                            key={j.id}
                            className="border-t border-border hover:bg-muted/30 cursor-pointer"
                            onClick={() => onSelect(j.id)}
                        >
                            <td className="px-3 py-2 font-mono text-[10px]">
                                {j.id.slice(0, 8)}
                            </td>
                            <td className="px-3 py-2 text-xs">
                                {j.template_name || (
                                    <span className="text-muted-foreground">custom</span>
                                )}
                            </td>
                            <td className="px-3 py-2 text-xs">{j.config.provider}</td>
                            <td className="px-3 py-2 text-xs font-mono">
                                {j.config.location}
                            </td>
                            <td className="px-3 py-2 text-xs tabular-nums">
                                {j.config.server_count}x {j.config.server_type}
                            </td>
                            <td className="px-3 py-2">
                                <span
                                    className={`px-1.5 py-0.5 rounded text-xs ${STATE_TONE[j.state]}`}
                                >
                                    {j.state.replace(/_/g, " ")}
                                </span>
                            </td>
                            <td className="px-3 py-2 text-xs text-muted-foreground">
                                {j.started_at
                                    ? new Date(j.started_at).toLocaleString()
                                    : new Date(j.created_at).toLocaleString()}
                            </td>
                        </tr>
                    ))}
                </tbody>
            </table>
        </div>
    );
}

// --------------------------------------------------------------------
// Job detail view
// --------------------------------------------------------------------

function JobDetail({
    id,
    onBack,
    onChanged,
}: {
    id: string;
    onBack: () => void;
    onChanged: () => void;
}) {
    const jobQ = useQuery({
        queryKey: ["admin", "provisioning-job", id],
        queryFn: () => getProvisioningJob(id),
        refetchInterval: (q) => {
            const j = q.state.data;
            if (j && TERMINAL.has(j.state)) return false;
            return 2000;
        },
        retry: false,
    });

    const retryMut = useMutation({
        mutationFn: () => retryProvisioningJob(id),
        onSuccess: () => {
            toast.success("Job re-queued");
            onChanged();
            jobQ.refetch();
        },
        onError: (e: Error) => toast.error(e.message),
    });

    const job = jobQ.data;

    return (
        <div>
            <button
                onClick={onBack}
                className="inline-flex items-center gap-1 text-xs text-muted-foreground hover:text-foreground mb-2"
            >
                <ArrowLeft className="size-3" />
                All jobs
            </button>
            <PageHeader
                title={`Job ${id.slice(0, 8)}`}
                description={
                    job?.template_name
                        ? `Launched from template "${job.template_name}"`
                        : "Launched with a custom config"
                }
            >
                {job?.state === "failed" && (
                    <Button
                        size="sm"
                        onClick={() => retryMut.mutate()}
                        disabled={retryMut.isPending}
                        className="bg-[var(--admin-accent)] hover:bg-[var(--admin-accent-strong)] text-white"
                    >
                        <RefreshCw className="size-4" />
                        {retryMut.isPending ? "Retrying..." : "Retry"}
                    </Button>
                )}
            </PageHeader>

            {jobQ.isLoading && <Skeleton className="h-40 w-full" />}
            {!jobQ.isLoading && !job && (
                <Card>
                    <CardContent className="py-8 text-center text-sm text-muted-foreground">
                        Job not found. The
                        <code className="px-1">/admin/provisioning-jobs/{id}</code>
                        endpoint returned nothing.
                    </CardContent>
                </Card>
            )}

            {job && (
                <div className="grid grid-cols-1 lg:grid-cols-2 gap-3">
                    <Card>
                        <CardHeader>
                            <CardTitle>Timeline</CardTitle>
                            <CardDescription>
                                State-machine progress for this provisioning attempt.
                            </CardDescription>
                        </CardHeader>
                        <CardContent className="pt-0">
                            <JobProgressTimeline job={job} />
                        </CardContent>
                    </Card>

                    <Card>
                        <CardHeader>
                            <CardTitle>Config</CardTitle>
                            <CardDescription>
                                The exact request the control plane sent to the provider.
                            </CardDescription>
                        </CardHeader>
                        <CardContent className="pt-0 space-y-2 text-sm">
                            <KV label="Provider" value={job.config.provider} mono />
                            <KV label="Location" value={job.config.location} mono />
                            <KV label="Server type" value={job.config.server_type} mono />
                            <KV
                                label="Servers"
                                value={`${job.config.server_count}x`}
                            />
                            <KV
                                label="IPv4 / server"
                                value={String(job.config.ipv4_per_server)}
                            />
                            <KV
                                label="IPv6 / server"
                                value={String(job.config.ipv6_per_server)}
                            />
                            <KV label="Worker tier" value={job.config.worker_tier} />
                            <KV label="Egress kind" value={job.config.egress_kind} />
                            <KV label="Image" value={job.config.image} mono />
                            <KV label="Firewall" value={job.config.firewall} mono />
                            {job.created_worker_ids &&
                                job.created_worker_ids.length > 0 && (
                                    <div className="pt-2 border-t border-border">
                                        <div className="text-xs text-muted-foreground mb-1">
                                            Created workers
                                        </div>
                                        <div className="space-y-1">
                                            {job.created_worker_ids.map((wid) => (
                                                <Link
                                                    key={wid}
                                                    to={`/workers/${wid}`}
                                                    className="block text-xs font-mono text-[var(--admin-accent-strong)] hover:underline"
                                                >
                                                    {wid}
                                                </Link>
                                            ))}
                                        </div>
                                    </div>
                                )}
                        </CardContent>
                    </Card>
                </div>
            )}

            {job?.timeline && job.timeline.length > 0 && (
                <Card className="mt-3">
                    <CardHeader>
                        <CardTitle>Event log</CardTitle>
                        <CardDescription>
                            Every state transition for this job, newest first.
                        </CardDescription>
                    </CardHeader>
                    <CardContent className="pt-0">
                        <ul className="space-y-1.5 text-xs">
                            {[...job.timeline].reverse().map((t, i) => (
                                <li
                                    key={i}
                                    className="flex items-start gap-2 border-l-2 border-border pl-3"
                                >
                                    <span className="font-mono text-muted-foreground w-40 shrink-0">
                                        {new Date(t.at).toLocaleString()}
                                    </span>
                                    <Badge variant="outline" className="text-[10px]">
                                        {t.state}
                                    </Badge>
                                    {t.note && (
                                        <span className="text-muted-foreground">{t.note}</span>
                                    )}
                                </li>
                            ))}
                        </ul>
                    </CardContent>
                </Card>
            )}
        </div>
    );
}

function KV({
    label,
    value,
    mono,
}: {
    label: string;
    value: string;
    mono?: boolean;
}) {
    return (
        <div className="flex items-center justify-between gap-3">
            <span className="text-xs text-muted-foreground uppercase tracking-wide">
                {label}
            </span>
            <span className={mono ? "font-mono text-xs" : "text-sm"}>{value}</span>
        </div>
    );
}
