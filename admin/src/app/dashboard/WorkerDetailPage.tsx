// Single worker detail — overview header + the four SSH lifecycle
// actions (test, install, restart, uninstall) wired to the admin
// endpoints. Logs panel tails journald with a selectable line count
// and an optional follow mode.

import { useEffect, useRef, useState } from "react";
import { Link, useParams } from "react-router-dom";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { toast } from "sonner";
import {
    ArrowLeft,
    Copy,
    Download,
    Hammer,
    PlayCircle,
    PowerOff,
    RefreshCw,
    ShieldAlert,
    StopCircle,
} from "lucide-react";
import { PageHeader } from "@/components/layout/PageHeader";
import { StateLegend } from "@/components/StateLegend";
import { WORKER_HEALTH_LEGEND } from "@/lib/legends";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardHeader, CardTitle, CardDescription } from "@/components/ui/card";
import { Skeleton } from "@/components/ui/skeleton";
import { Badge } from "@/components/ui/badge";
import {
    Select,
    SelectContent,
    SelectItem,
    SelectTrigger,
    SelectValue,
} from "@/components/ui/select";
import { Switch } from "@/components/ui/switch";
import {
    getManagedWorker,
    getWorkerEmails,
    getWorkerLogs,
    getWorkerLiveStatus,
    installWorker,
    restartWorker,
    testWorker,
    uninstallWorker,
} from "@/lib/api/client/admin/workers";

// Risk band (mailbox reputation tier) + health state (warmup/worker) tones.
const RISK_TONE: Record<string, string> = {
    clean: "border-emerald-300 bg-emerald-50 text-emerald-700",
    risky: "border-amber-300 bg-amber-50 text-amber-700",
    quarantine: "border-red-300 bg-red-50 text-red-700",
};

const HEALTH_TONE: Record<string, string> = {
    healthy: "border-emerald-300 bg-emerald-50 text-emerald-700",
    watch: "border-amber-300 bg-amber-50 text-amber-700",
    throttled: "border-orange-300 bg-orange-50 text-orange-700",
    quarantined: "border-red-300 bg-red-50 text-red-700",
    blocked: "border-red-300 bg-red-50 text-red-700",
};

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

    const [logLines, setLogLines] = useState(200);
    const [followLogs, setFollowLogs] = useState(false);
    const logScrollRef = useRef<HTMLDivElement>(null);

    const logsQ = useQuery({
        queryKey: ["admin", "worker", id, "logs", logLines],
        queryFn: () => getWorkerLogs(id, logLines),
        enabled: !!id,
        retry: false,
        refetchInterval: followLogs ? 5_000 : false,
    });

    // Follow mode pins the viewport to the newest lines on every refetch.
    useEffect(() => {
        if (!followLogs) return;
        const el = logScrollRef.current;
        if (el) el.scrollTop = el.scrollHeight;
    }, [followLogs, logsQ.data]);

    const copyLogs = async () => {
        if (!logsQ.data?.logs) return;
        try {
            await navigator.clipboard.writeText(logsQ.data.logs);
            toast.success("Logs copied to clipboard");
        } catch {
            toast.error("Could not copy logs");
        }
    };

    const emailsQ = useQuery({
        queryKey: ["admin", "worker", id, "emails"],
        queryFn: () => getWorkerEmails(id),
        enabled: !!id,
        staleTime: 30_000,
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
                description={`Managed worker · install state: ${w.install_state}`}
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
                        <div className="flex flex-wrap gap-3 pt-1">
                            <StateLegend label="Health states" entries={WORKER_HEALTH_LEGEND} />
                        </div>
                    </CardHeader>
                    <CardContent className="pt-0 text-sm space-y-2">
                        <KV
                            label="Health"
                            value={
                                <Badge
                                    variant="outline"
                                    className={`text-[10px] ${HEALTH_TONE[w.health_state] ?? "border-zinc-300 text-zinc-600"}`}
                                >
                                    {w.health_state}
                                </Badge>
                            }
                        />
                        <KV label="Load score" value={w.load_score.toFixed(2)} />
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
                    <CardTitle>Mailboxes</CardTitle>
                    <CardDescription>
                        Inboxes assigned to this worker and their health. Risk band drives which
                        workers a mailbox may share — low-health inboxes are kept off trusted workers.
                    </CardDescription>
                </CardHeader>
                <CardContent className="pt-0">
                    {emailsQ.isLoading && <Skeleton className="h-32 w-full" />}
                    {emailsQ.isError && (
                        <div className="text-xs text-muted-foreground">Could not load mailboxes.</div>
                    )}
                    {emailsQ.data &&
                        ((emailsQ.data.data ?? []).length === 0 ? (
                            <div className="text-sm text-muted-foreground">
                                No mailboxes assigned to this worker.
                            </div>
                        ) : (
                            <>
                                <div className="overflow-x-auto">
                                    <table className="w-full text-sm">
                                        <thead className="text-muted-foreground text-xs uppercase">
                                            <tr>
                                                <th className="text-left px-2 py-1.5 font-medium">Mailbox</th>
                                                <th className="text-left px-2 py-1.5 font-medium">Provider</th>
                                                <th className="text-left px-2 py-1.5 font-medium">Status</th>
                                                <th className="text-left px-2 py-1.5 font-medium">Risk band</th>
                                                <th className="text-left px-2 py-1.5 font-medium">Warmup health</th>
                                                <th className="text-right px-2 py-1.5 font-medium">Spam</th>
                                                <th className="text-left px-2 py-1.5 font-medium">Synced</th>
                                            </tr>
                                        </thead>
                                        <tbody>
                                            {(emailsQ.data.data ?? []).map((m) => (
                                                <tr key={m.id} className="border-t border-border">
                                                    <td className="px-2 py-1.5 font-mono text-xs">{m.email}</td>
                                                    <td className="px-2 py-1.5 text-xs">
                                                        {m.provider}
                                                        {m.warmup_enabled && (
                                                            <span className="ml-1 text-[10px] text-sky-600">warming</span>
                                                        )}
                                                    </td>
                                                    <td className="px-2 py-1.5 text-xs">{m.status}</td>
                                                    <td className="px-2 py-1.5">
                                                        <Badge
                                                            variant="outline"
                                                            className={`text-[10px] ${RISK_TONE[m.risk_band] ?? "border-zinc-300 text-zinc-600"}`}
                                                        >
                                                            {m.risk_band}
                                                        </Badge>
                                                    </td>
                                                    <td className="px-2 py-1.5">
                                                        {m.warmup_health ? (
                                                            <Badge
                                                                variant="outline"
                                                                className={`text-[10px] ${HEALTH_TONE[m.warmup_health] ?? "border-zinc-300 text-zinc-600"}`}
                                                            >
                                                                {m.warmup_health}
                                                            </Badge>
                                                        ) : (
                                                            <span className="text-xs text-muted-foreground">—</span>
                                                        )}
                                                    </td>
                                                    <td className="px-2 py-1.5 text-right tabular-nums text-xs">
                                                        {m.spam_score ?? "—"}
                                                    </td>
                                                    <td className="px-2 py-1.5 text-xs text-muted-foreground">
                                                        {m.last_synced_at
                                                            ? new Date(m.last_synced_at).toLocaleDateString()
                                                            : "—"}
                                                    </td>
                                                </tr>
                                            ))}
                                        </tbody>
                                    </table>
                                </div>
                                {emailsQ.data.pagination?.has_more && (
                                    <div className="mt-2 text-xs text-muted-foreground">
                                        Showing the first {(emailsQ.data.data ?? []).length}
                                        {emailsQ.data.pagination.total != null
                                            ? ` of ${emailsQ.data.pagination.total}`
                                            : ""}{" "}
                                        — use the Mailboxes explorer for the full list.
                                    </div>
                                )}
                            </>
                        ))}
                </CardContent>
            </Card>

            <Card className="mt-4">
                <CardHeader>
                    <CardTitle className="flex flex-wrap items-center justify-between gap-2">
                        <span>Recent logs</span>
                        <div className="flex flex-wrap items-center gap-2">
                            <Select
                                value={String(logLines)}
                                onValueChange={(v) => setLogLines(Number(v))}
                            >
                                <SelectTrigger className="h-8 w-[120px] text-xs" size="sm">
                                    <SelectValue />
                                </SelectTrigger>
                                <SelectContent>
                                    <SelectItem value="100">100 lines</SelectItem>
                                    <SelectItem value="200">200 lines</SelectItem>
                                    <SelectItem value="500">500 lines</SelectItem>
                                    <SelectItem value="1000">1000 lines</SelectItem>
                                </SelectContent>
                            </Select>
                            <label className="flex items-center gap-1.5 text-xs font-normal text-muted-foreground">
                                <Switch
                                    checked={followLogs}
                                    onCheckedChange={setFollowLogs}
                                />
                                Follow
                            </label>
                            <Button
                                size="sm"
                                variant="ghost"
                                onClick={copyLogs}
                                disabled={!logsQ.data?.logs}
                            >
                                <Copy className="size-4" />
                                Copy
                            </Button>
                            <Button
                                size="sm"
                                variant="ghost"
                                onClick={() => logsQ.refetch()}
                                disabled={logsQ.isFetching}
                            >
                                <RefreshCw
                                    className={`size-4 ${logsQ.isFetching ? "animate-spin" : ""}`}
                                />
                                {logsQ.isFetching ? "Loading…" : "Reload"}
                            </Button>
                        </div>
                    </CardTitle>
                    <CardDescription>
                        Tail of the worker's systemd journal, last {logLines} lines pulled over
                        SSH. Follow refetches every 5s and keeps the newest lines in view.
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
                        <div
                            ref={logScrollRef}
                            className="rounded-md border border-zinc-800 bg-zinc-950 overflow-auto max-h-96"
                        >
                            <pre className="text-[11px] leading-relaxed font-mono text-zinc-100 p-3">
                                {logsQ.data.logs || "(no log output)"}
                            </pre>
                        </div>
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
