// Single worker detail — overview header + the four SSH lifecycle
// actions (test, install, restart, uninstall) wired to the admin
// endpoints. Logs panel pulls the last 200 lines from journald.

import { useState } from "react";
import { Link, useParams } from "react-router-dom";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { toast } from "sonner";
import {
    ArrowLeft,
    Download,
    Hammer,
    PlayCircle,
    PowerOff,
    RefreshCw,
    ShieldAlert,
    StopCircle,
} from "lucide-react";
import { PageHeader } from "@/components/layout/PageHeader";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardHeader, CardTitle, CardDescription } from "@/components/ui/card";
import { Skeleton } from "@/components/ui/skeleton";
import { Badge } from "@/components/ui/badge";
import {
    getManagedWorker,
    getWorkerLogs,
    getWorkerLiveStatus,
    installWorker,
    restartWorker,
    testWorker,
    uninstallWorker,
} from "@/lib/api/client/admin/workers";

export default function WorkerDetailPage() {
    const { id = "" } = useParams<{ id: string }>();
    const qc = useQueryClient();

    const workerQ = useQuery({
        queryKey: ["admin", "worker", id],
        queryFn: () => getManagedWorker(id),
        enabled: !!id,
        refetchInterval: 15_000,
    });

    const liveQ = useQuery({
        queryKey: ["admin", "worker", id, "live"],
        queryFn: () => getWorkerLiveStatus(id),
        enabled: !!id,
        refetchInterval: 10_000,
        retry: false,
    });

    const logsQ = useQuery({
        queryKey: ["admin", "worker", id, "logs"],
        queryFn: () => getWorkerLogs(id, 200),
        enabled: !!id,
        retry: false,
    });

    const invalidate = () => {
        qc.invalidateQueries({ queryKey: ["admin", "worker", id] });
        qc.invalidateQueries({ queryKey: ["admin", "workers", "managed"] });
    };

    const testMut = useMutation({
        mutationFn: () => testWorker(id),
        onSuccess: (res) => {
            toast.success(res.ok ? "SSH reachable" : res.error || "SSH unreachable");
            invalidate();
        },
        onError: (e: Error) => toast.error(e.message),
    });

    const installMut = useMutation({
        mutationFn: () => installWorker(id),
        onSuccess: () => {
            toast.success("Install kicked off");
            invalidate();
        },
        onError: (e: Error) => toast.error(e.message),
    });

    const restartMut = useMutation({
        mutationFn: () => restartWorker(id),
        onSuccess: () => {
            toast.success("Worker restarting");
            invalidate();
        },
        onError: (e: Error) => toast.error(e.message),
    });

    const [confirmUninstall, setConfirmUninstall] = useState(false);
    const uninstallMut = useMutation({
        mutationFn: () => uninstallWorker(id),
        onSuccess: () => {
            toast.success("Uninstall scheduled");
            setConfirmUninstall(false);
            invalidate();
        },
        onError: (e: Error) => toast.error(e.message),
    });

    if (workerQ.isLoading) {
        return (
            <div>
                <Skeleton className="h-8 w-1/3 mb-4" />
                <Skeleton className="h-40 w-full" />
            </div>
        );
    }

    if (workerQ.isError || !workerQ.data) {
        return (
            <div>
                <PageHeader title="Worker not found" />
                <p className="text-sm text-muted-foreground">
                    Worker <code>{id}</code> isn't in the managed-worker registry. It may have been
                    deleted, or you may not have permission to view it.
                </p>
                <Link to="/workers" className="text-sm underline mt-3 inline-block">
                    Back to workers
                </Link>
            </div>
        );
    }

    const w = workerQ.data;

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
                title={w.name || w.id.slice(0, 12)}
                description={`${w.worker_type}${w.worker_type === "shared" ? (w.free_tier ? " · free tier" : " · premium tier") : ""} · install state: ${w.install_state}`}
            >
                <Button
                    size="sm"
                    variant="outline"
                    onClick={() => testMut.mutate()}
                    disabled={testMut.isPending}
                >
                    <PlayCircle className="size-4" />
                    {testMut.isPending ? "Testing…" : "Test SSH"}
                </Button>
                <Button
                    size="sm"
                    variant="outline"
                    onClick={() => installMut.mutate()}
                    disabled={installMut.isPending}
                >
                    <Hammer className="size-4" />
                    {installMut.isPending ? "Installing…" : "Install"}
                </Button>
                <Button
                    size="sm"
                    variant="outline"
                    onClick={() => restartMut.mutate()}
                    disabled={restartMut.isPending}
                >
                    <RefreshCw className="size-4" />
                    {restartMut.isPending ? "Restarting…" : "Restart"}
                </Button>
                <Button
                    size="sm"
                    variant={confirmUninstall ? "destructive" : "outline"}
                    onClick={() => (confirmUninstall ? uninstallMut.mutate() : setConfirmUninstall(true))}
                    disabled={uninstallMut.isPending}
                >
                    {confirmUninstall ? <StopCircle className="size-4" /> : <PowerOff className="size-4" />}
                    {uninstallMut.isPending
                        ? "Uninstalling…"
                        : confirmUninstall
                            ? "Confirm uninstall"
                            : "Uninstall"}
                </Button>
            </PageHeader>

            {confirmUninstall && (
                <div className="mb-4 rounded-md border border-red-200 bg-red-50 p-3 text-sm text-red-700 flex items-start gap-2">
                    <ShieldAlert className="size-4 mt-0.5" />
                    <div>
                        Uninstalling will stop the worker process, drop the systemd unit, and detach
                        the machine from the fleet. Existing mailbox assignments will need to be
                        re-routed manually before you do this.
                        <button
                            onClick={() => setConfirmUninstall(false)}
                            className="ml-3 underline"
                        >
                            cancel
                        </button>
                    </div>
                </div>
            )}

            <div className="grid grid-cols-1 lg:grid-cols-3 gap-3">
                <Card>
                    <CardHeader>
                        <CardTitle>SSH target</CardTitle>
                        <CardDescription>How the control plane reaches this worker.</CardDescription>
                    </CardHeader>
                    <CardContent className="pt-0 text-sm space-y-2">
                        <KV label="Host" value={w.ssh_host || w.ip_addr} mono />
                        <KV label="Port" value={String(w.ssh_port ?? "22")} mono />
                        <KV label="User" value={w.ssh_user || "—"} mono />
                        <KV label="IP" value={w.ip_addr} mono />
                        {w.ssh_host_fingerprint && (
                            <KV label="Fingerprint" value={w.ssh_host_fingerprint} mono />
                        )}
                    </CardContent>
                </Card>

                <Card>
                    <CardHeader>
                        <CardTitle>Runtime</CardTitle>
                        <CardDescription>Live status from the worker daemon.</CardDescription>
                    </CardHeader>
                    <CardContent className="pt-0 text-sm space-y-2">
                        {liveQ.isLoading && <Skeleton className="h-4 w-2/3" />}
                        {liveQ.isError && (
                            <div className="text-xs text-muted-foreground">
                                Live status unavailable (worker offline or SSH unreachable).
                            </div>
                        )}
                        {liveQ.data && (
                            <>
                                <KV
                                    label="Service"
                                    value={
                                        <Badge
                                            variant={liveQ.data.service_active ? "default" : "secondary"}
                                            className={liveQ.data.service_active ? "bg-emerald-600" : ""}
                                        >
                                            {liveQ.data.service_active ? "active" : "inactive"}
                                        </Badge>
                                    }
                                />
                                <KV
                                    label="Container"
                                    value={
                                        <Badge
                                            variant={liveQ.data.container_up ? "default" : "secondary"}
                                            className={liveQ.data.container_up ? "bg-emerald-600" : ""}
                                        >
                                            {liveQ.data.container_up ? "up" : "down"}
                                        </Badge>
                                    }
                                />
                                <KV label="Image" value={liveQ.data.container_image || "—"} mono />
                                <KV label="Uptime" value={liveQ.data.uptime || "—"} />
                            </>
                        )}
                    </CardContent>
                </Card>

                <Card>
                    <CardHeader>
                        <CardTitle>Fleet position</CardTitle>
                        <CardDescription>How this worker is being used today.</CardDescription>
                    </CardHeader>
                    <CardContent className="pt-0 text-sm space-y-2">
                        <KV label="Type" value={w.worker_type} />
                        <KV label="Free tier" value={w.free_tier ? "yes" : "no"} />
                        <KV label="Risk pool" value={w.risk_pool} />
                        <KV label="Mailboxes" value={String(w.account_count)} />
                        <KV label="Image" value={w.image_version || "—"} mono />
                        {w.tags && w.tags.length > 0 && (
                            <div className="pt-1 flex flex-wrap gap-1">
                                {w.tags.map((t) => (
                                    <Badge key={t} variant="outline" className="text-[10px]">
                                        {t}
                                    </Badge>
                                ))}
                            </div>
                        )}
                    </CardContent>
                </Card>
            </div>

            <Card className="mt-4">
                <CardHeader>
                    <CardTitle className="flex items-center justify-between">
                        <span>Recent logs</span>
                        <Button
                            size="sm"
                            variant="ghost"
                            onClick={() => logsQ.refetch()}
                            disabled={logsQ.isFetching}
                        >
                            <RefreshCw className="size-4" />
                            {logsQ.isFetching ? "Loading…" : "Reload"}
                        </Button>
                    </CardTitle>
                    <CardDescription>
                        Tail of the worker's systemd journal — last 200 lines pulled over SSH.
                    </CardDescription>
                </CardHeader>
                <CardContent className="pt-0">
                    {logsQ.isLoading && <Skeleton className="h-40 w-full" />}
                    {logsQ.isError && (
                        <div className="text-xs text-muted-foreground">
                            Could not fetch logs (worker offline or SSH unreachable).
                        </div>
                    )}
                    {logsQ.data && (
                        <pre className="text-[11px] leading-relaxed font-mono bg-zinc-950 text-zinc-100 rounded-md p-3 overflow-auto max-h-96">
                            {logsQ.data.logs || "(no log output)"}
                        </pre>
                    )}
                    {logsQ.data?.logs && (
                        <div className="mt-2 flex items-center gap-2 text-xs text-muted-foreground">
                            <Download className="size-3" />
                            <a
                                href={`data:text/plain;charset=utf-8,${encodeURIComponent(logsQ.data.logs)}`}
                                download={`worker-${w.id}-logs.txt`}
                                className="underline"
                            >
                                Download as .txt
                            </a>
                        </div>
                    )}
                </CardContent>
            </Card>
        </div>
    );
}

function KV({
    label,
    value,
    mono,
}: {
    label: string;
    value: React.ReactNode;
    mono?: boolean;
}) {
    return (
        <div className="flex items-center justify-between gap-3">
            <span className="text-xs text-muted-foreground uppercase tracking-wide">{label}</span>
            <span className={mono ? "font-mono text-xs truncate max-w-[60%]" : "text-sm"}>
                {value}
            </span>
        </div>
    );
}
