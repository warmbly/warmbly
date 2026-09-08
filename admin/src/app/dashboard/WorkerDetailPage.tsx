// Single worker detail — overview header + the SSH lifecycle actions wired to
// the admin endpoints. Routine actions (test, install, restart, pull latest,
// apply config) sit in the header; the destructive and rarely-used ones (rotate
// keys, OS update, reboot, uninstall, delete) are grouped in a Maintenance card
// so they can't be hit by accident. Logs panel tails journald with a selectable
// line count and an optional follow mode. The worker row, its mailboxes and
// its stats are keyed under ["admin","workers"] so the realtime workers spine
// refreshes them; only the SSH probes (live status, log follow) still poll.

import { useEffect, useRef, useState } from "react";
import { Link, useNavigate, useParams } from "react-router-dom";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { toast } from "sonner";
import {
    ArrowLeft,
    ArrowRightLeft,
    ArrowUpCircle,
    Check,
    Copy,
    Gauge,
    Download,
    Hammer,
    KeyRound,
    PackageOpen,
    PlayCircle,
    PowerOff,
    RefreshCw,
    RotateCcw,
    ShieldAlert,
    SlidersHorizontal,
    StopCircle,
    Trash2,
    X,
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
import { Checkbox } from "@/components/ui/checkbox";
import {
    Dialog,
    DialogContent,
    DialogDescription,
    DialogFooter,
    DialogHeader,
    DialogTitle,
} from "@/components/ui/dialog";
import {
    applyWorkerConfig,
    deleteWorker,
    getManagedWorker,
    getWorkerEmails,
    getWorkerLogs,
    getWorkerLiveStatus,
    getWorkerStats,
    listManagedWorkers,
    reassignWorkerEmails,
    installWorker,
    rebootWorker,
    restartWorker,
    rotateWorkerKeys,
    systemUpdateWorker,
    testWorker,
    uninstallWorker,
    upgradeWorker,
    type WorkerStats,
} from "@/lib/api/client/admin/workers";
import type { AdminWorkerEmail, ManagedWorker } from "@/lib/api/models/admin";

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
    const navigate = useNavigate();

    const workerQ = useQuery({
        queryKey: ["admin", "workers", id],
        queryFn: () => getManagedWorker(id),
        enabled: !!id,
    });

    const statsQ = useQuery({
        queryKey: ["admin", "workers", id, "stats"],
        queryFn: () => getWorkerStats(id),
        enabled: !!id,
        retry: false,
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
        queryKey: ["admin", "workers", id, "emails"],
        queryFn: () => getWorkerEmails(id),
        enabled: !!id,
    });

    // Mailbox selection for the reassign action; cleared when the list changes.
    const [selected, setSelected] = useState<Set<string>>(new Set());
    const [reassignOpen, setReassignOpen] = useState(false);
    const mailboxes = emailsQ.data?.data ?? [];
    const allSelected = mailboxes.length > 0 && mailboxes.every((m) => selected.has(m.id));

    function toggleOne(mid: string) {
        setSelected((prev) => {
            const next = new Set(prev);
            if (next.has(mid)) next.delete(mid);
            else next.add(mid);
            return next;
        });
    }

    function toggleAll() {
        setSelected(allSelected ? new Set() : new Set(mailboxes.map((m) => m.id)));
    }

    const invalidate = () => {
        qc.invalidateQueries({ queryKey: ["admin", "workers"] });
        qc.invalidateQueries({ queryKey: ["admin", "worker", id] });
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

    const upgradeMut = useMutation({
        mutationFn: () => upgradeWorker(id),
        onSuccess: () => {
            toast.success("Pulling the latest image and restarting");
            invalidate();
        },
        onError: (e: Error) => toast.error(e.message),
    });

    const applyMut = useMutation({
        mutationFn: () => applyWorkerConfig(id),
        onSuccess: () => {
            toast.success("Config rewritten and worker restarted");
            invalidate();
        },
        onError: (e: Error) => toast.error(e.message),
    });

    // The rotated public key is shown once here; it has to reach the VPS's
    // authorized_keys or every later SSH action fails.
    const [rotatedKey, setRotatedKey] = useState<string | null>(null);
    const rotateMut = useMutation({
        mutationFn: () => rotateWorkerKeys(id),
        onSuccess: (res) => {
            setRotatedKey(res.ssh_public_key);
            toast.success("New keypair generated");
            invalidate();
        },
        onError: (e: Error) => toast.error(e.message),
    });

    const [systemUpdateOutput, setSystemUpdateOutput] = useState<string | null>(null);
    const systemUpdateMut = useMutation({
        mutationFn: () => systemUpdateWorker(id),
        onSuccess: (res) => {
            setSystemUpdateOutput(res.output);
            toast.success(
                res.reboot_required
                    ? "OS packages updated. A reboot is required."
                    : "OS packages updated",
            );
        },
        onError: (e: Error) => toast.error(e.message),
    });

    const [confirmReboot, setConfirmReboot] = useState(false);
    const rebootMut = useMutation({
        mutationFn: () => rebootWorker(id),
        onSuccess: () => {
            toast.success("Reboot issued");
            setConfirmReboot(false);
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

    const [confirmDelete, setConfirmDelete] = useState(false);
    const deleteMut = useMutation({
        mutationFn: () => deleteWorker(id),
        onSuccess: () => {
            toast.success("Worker deleted");
            qc.invalidateQueries({ queryKey: ["admin", "workers", "managed"] });
            navigate("/workers");
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
                    variant="outline"
                    onClick={() => upgradeMut.mutate()}
                    disabled={upgradeMut.isPending}
                >
                    <ArrowUpCircle className="size-4" />
                    {upgradeMut.isPending ? "Pulling…" : "Pull latest"}
                </Button>
                <Button
                    size="sm"
                    variant="outline"
                    onClick={() => applyMut.mutate()}
                    disabled={applyMut.isPending}
                >
                    <SlidersHorizontal className="size-4" />
                    {applyMut.isPending ? "Applying…" : "Apply config"}
                </Button>
            </PageHeader>

            {rotatedKey && (
                <div className="mb-4 rounded-md border border-amber-200 bg-amber-50 p-3 text-sm text-amber-800">
                    <div className="flex items-start gap-2">
                        <KeyRound className="size-4 mt-0.5 shrink-0" />
                        <div className="min-w-0 flex-1">
                            <p className="font-medium">
                                New public key. Add it to the VPS before the next action.
                            </p>
                            <p className="mt-1">
                                Until this lands in <code>~/.ssh/authorized_keys</code> on the
                                machine, every SSH action here will fail.
                            </p>
                            <pre className="mt-2 overflow-x-auto rounded bg-white/70 p-2 text-xs">
                                {rotatedKey}
                            </pre>
                            <div className="mt-2 flex gap-3">
                                <button
                                    className="underline"
                                    onClick={() => {
                                        navigator.clipboard.writeText(rotatedKey);
                                        toast.success("Public key copied");
                                    }}
                                >
                                    copy
                                </button>
                                <button className="underline" onClick={() => setRotatedKey(null)}>
                                    dismiss
                                </button>
                            </div>
                        </div>
                    </div>
                </div>
            )}

            {systemUpdateOutput && (
                <div className="mb-4 rounded-md border border-slate-200 bg-slate-50 p-3 text-sm">
                    <div className="flex items-center justify-between">
                        <span className="font-medium">Package manager output</span>
                        <button className="underline" onClick={() => setSystemUpdateOutput(null)}>
                            dismiss
                        </button>
                    </div>
                    <pre className="mt-2 max-h-64 overflow-auto rounded bg-white p-2 text-xs">
                        {systemUpdateOutput}
                    </pre>
                </div>
            )}

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

            {confirmDelete && (
                <div className="mb-4 rounded-md border border-red-200 bg-red-50 p-3 text-sm text-red-700 flex items-start gap-2">
                    <ShieldAlert className="size-4 mt-0.5" />
                    <div>
                        Deleting removes the worker row and its stored SSH key. It does not stop
                        anything still running on the machine, so uninstall first unless the box is
                        already gone.
                        <button onClick={() => setConfirmDelete(false)} className="ml-3 underline">
                            cancel
                        </button>
                    </div>
                </div>
            )}

            <div className="grid grid-cols-1 lg:grid-cols-2 xl:grid-cols-4 gap-3">
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

                <CapacityCard stats={statsQ.data} loading={statsQ.isLoading} error={statsQ.isError} />
            </div>

            <Card className="mt-4">
                <CardHeader>
                    <CardTitle className="flex flex-wrap items-center justify-between gap-2">
                        <span>Mailboxes</span>
                        <Button
                            size="sm"
                            variant="outline"
                            disabled={mailboxes.length === 0}
                            onClick={() => {
                                if (selected.size === 0) setSelected(new Set(mailboxes.map((m) => m.id)));
                                setReassignOpen(true);
                            }}
                        >
                            <ArrowRightLeft className="size-4" />
                            {selected.size > 0 ? `Move ${selected.size} to worker…` : "Move all to worker…"}
                        </Button>
                    </CardTitle>
                    <CardDescription>
                        Inboxes assigned to this worker and their health. Risk band drives which
                        workers a mailbox may share — low-health inboxes are kept off trusted workers.
                        Select rows to move them to another worker of the same tier.
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
                                                <th className="w-8 px-2 py-1.5">
                                                    <Checkbox
                                                        checked={allSelected}
                                                        onCheckedChange={toggleAll}
                                                        aria-label="Select all mailboxes"
                                                    />
                                                </th>
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
                                                <tr
                                                    key={m.id}
                                                    onClick={() => toggleOne(m.id)}
                                                    className={`border-t border-border cursor-pointer ${selected.has(m.id) ? "bg-[var(--admin-accent-soft)]" : "hover:bg-muted/40"}`}
                                                >
                                                    <td className="px-2 py-1.5" onClick={(e) => e.stopPropagation()}>
                                                        <Checkbox
                                                            checked={selected.has(m.id)}
                                                            onCheckedChange={() => toggleOne(m.id)}
                                                            aria-label={`Select ${m.email}`}
                                                        />
                                                    </td>
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

            <SelectionBar
                count={selected.size}
                onMove={() => setReassignOpen(true)}
                onClear={() => setSelected(new Set())}
            />

            <ReassignDialog
                open={reassignOpen}
                onOpenChange={setReassignOpen}
                source={w}
                mailboxes={mailboxes.filter((m) => selected.has(m.id))}
                onDone={() => {
                    setSelected(new Set());
                    invalidate();
                }}
            />

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

            <Card className="mt-4">
                <CardHeader>
                    <CardTitle>Maintenance</CardTitle>
                    <CardDescription>
                        Host-level and destructive operations. Each one acts on the machine over
                        SSH, so the worker must be reachable.
                    </CardDescription>
                </CardHeader>
                <CardContent className="pt-0">
                    <div className="flex flex-wrap gap-2">
                        <Button
                            size="sm"
                            variant="outline"
                            onClick={() => systemUpdateMut.mutate()}
                            disabled={systemUpdateMut.isPending}
                        >
                            <PackageOpen className="size-4" />
                            {systemUpdateMut.isPending ? "Updating…" : "Update OS packages"}
                        </Button>
                        <Button
                            size="sm"
                            variant="outline"
                            onClick={() => rotateMut.mutate()}
                            disabled={rotateMut.isPending}
                        >
                            <KeyRound className="size-4" />
                            {rotateMut.isPending ? "Rotating…" : "Rotate SSH keys"}
                        </Button>
                        <Button
                            size="sm"
                            variant={confirmReboot ? "destructive" : "outline"}
                            onClick={() =>
                                confirmReboot ? rebootMut.mutate() : setConfirmReboot(true)
                            }
                            disabled={rebootMut.isPending}
                        >
                            <RotateCcw className="size-4" />
                            {rebootMut.isPending
                                ? "Rebooting…"
                                : confirmReboot
                                    ? "Confirm reboot"
                                    : "Reboot"}
                        </Button>
                        <Button
                            size="sm"
                            variant={confirmUninstall ? "destructive" : "outline"}
                            onClick={() =>
                                confirmUninstall ? uninstallMut.mutate() : setConfirmUninstall(true)
                            }
                            disabled={uninstallMut.isPending}
                        >
                            {confirmUninstall ? (
                                <StopCircle className="size-4" />
                            ) : (
                                <PowerOff className="size-4" />
                            )}
                            {uninstallMut.isPending
                                ? "Uninstalling…"
                                : confirmUninstall
                                    ? "Confirm uninstall"
                                    : "Uninstall"}
                        </Button>
                        <Button
                            size="sm"
                            variant={confirmDelete ? "destructive" : "outline"}
                            onClick={() =>
                                confirmDelete ? deleteMut.mutate() : setConfirmDelete(true)
                            }
                            disabled={deleteMut.isPending}
                        >
                            <Trash2 className="size-4" />
                            {deleteMut.isPending
                                ? "Deleting…"
                                : confirmDelete
                                    ? "Confirm delete"
                                    : "Delete worker"}
                        </Button>
                    </div>
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

function CapacityCard({
    stats,
    loading,
    error,
}: {
    stats: WorkerStats | undefined;
    loading: boolean;
    error: boolean;
}) {
    return (
        <Card>
            <CardHeader>
                <CardTitle className="flex items-center gap-1.5">
                    <Gauge className="size-4" />
                    Capacity
                </CardTitle>
                <CardDescription>Send throughput and queue depth for this worker.</CardDescription>
            </CardHeader>
            <CardContent className="pt-0 text-sm space-y-2">
                {loading && <Skeleton className="h-4 w-2/3" />}
                {error && <div className="text-xs text-muted-foreground">Stats unavailable.</div>}
                {stats && (
                    <>
                        <KV label="Sent today" value={stats.emails_sent_today.toLocaleString()} />
                        <KV label="This week" value={stats.emails_sent_this_week.toLocaleString()} />
                        <KV label="All time" value={stats.total_emails_sent.toLocaleString()} />
                        <KV
                            label="Success"
                            value={
                                <span className={stats.success_rate < 90 && stats.total_emails_sent > 0 ? "text-amber-700" : ""}>
                                    {stats.success_rate.toFixed(1)}%
                                </span>
                            }
                        />
                        <KV label="Avg delivery" value={`${Math.round(stats.average_delivery_time_ms)} ms`} />
                        <KV
                            label="Queue"
                            value={<span className={stats.queue_depth > 0 ? "font-medium" : ""}>{stats.queue_depth.toLocaleString()}</span>}
                        />
                    </>
                )}
            </CardContent>
        </Card>
    );
}

// Floating bottom-center bar for the mailbox selection. Fixed, so it stays
// in view however long the table is.
function SelectionBar({ count, onMove, onClear }: { count: number; onMove: () => void; onClear: () => void }) {
    if (count === 0) return null;
    return (
        <div className="fixed bottom-4 left-1/2 z-30 flex max-w-[calc(100vw-16px)] -translate-x-1/2 flex-wrap items-center justify-center gap-1.5 rounded-md border border-border bg-card px-2 py-1.5 shadow-[0_6px_20px_-4px_rgba(15,23,42,0.18),0_2px_4px_rgba(15,23,42,0.06)]">
            <div className="inline-flex h-7 items-center gap-1.5 rounded bg-[var(--admin-accent-soft)] px-2 text-[12px] font-medium text-[var(--admin-accent-strong)]">
                <Check className="size-3" />
                <span>{count} selected</span>
            </div>
            <button
                type="button"
                onClick={onMove}
                className="inline-flex h-7 items-center gap-1.5 rounded px-2.5 text-[12px] font-medium text-foreground transition-colors hover:bg-muted"
            >
                <ArrowRightLeft className="size-3" />
                Move to worker…
            </button>
            <button
                type="button"
                onClick={onClear}
                className="grid size-7 place-items-center rounded text-muted-foreground transition-colors hover:bg-muted hover:text-foreground"
                aria-label="Clear selection"
            >
                <X className="size-3.5" />
            </button>
        </div>
    );
}

const HEALTH_LABEL: Record<string, string> = {
    healthy: "healthy",
    watch: "watch",
    throttled: "throttled",
    quarantined: "quarantined",
    blocked: "blocked",
};

// Pick a target worker for the selected mailboxes. Mailboxes keep their
// tier, so a worker in the other tier is listed but cannot be chosen.
function ReassignDialog({
    open,
    onOpenChange,
    source,
    mailboxes,
    onDone,
}: {
    open: boolean;
    onOpenChange: (v: boolean) => void;
    source: ManagedWorker;
    mailboxes: AdminWorkerEmail[];
    onDone: () => void;
}) {
    const [target, setTarget] = useState("");

    const workersQ = useQuery({
        queryKey: ["admin", "workers", "managed"],
        queryFn: listManagedWorkers,
        enabled: open,
        staleTime: 30_000,
    });
    const candidates = (workersQ.data?.data ?? []).filter((x) => x.id !== source.id);
    const chosen = candidates.find((x) => x.id === target) ?? null;

    const mutation = useMutation({
        mutationFn: () =>
            reassignWorkerEmails(
                target,
                mailboxes.map((m) => m.id),
            ),
        onSuccess: () => {
            toast.success(`${mailboxes.length} mailbox${mailboxes.length === 1 ? "" : "es"} moved to ${chosen?.name || target.slice(0, 8)}`);
            setTarget("");
            onDone();
            onOpenChange(false);
        },
        onError: (e: Error) => toast.error(e.message || "Reassign failed"),
    });

    const sameTier = !!chosen && chosen.free_tier === source.free_tier;

    return (
        <Dialog
            open={open}
            onOpenChange={(v) => {
                if (!v && mutation.isPending) return;
                if (!v) setTarget("");
                onOpenChange(v);
            }}
        >
            <DialogContent>
                <DialogHeader>
                    <DialogTitle>Move {mailboxes.length} mailbox{mailboxes.length === 1 ? "" : "es"} to another worker</DialogTitle>
                    <DialogDescription>
                        Sending and sync for these mailboxes continue from the target on its next heartbeat. Tier
                        placement is strict: a {source.free_tier ? "free" : "premium"}-tier mailbox only runs on a{" "}
                        {source.free_tier ? "free" : "premium"}-tier worker.
                    </DialogDescription>
                </DialogHeader>

                <div className="space-y-3">
                    <div className="max-h-28 overflow-auto rounded-md border border-border bg-muted/30 p-2 font-mono text-[11px] leading-relaxed">
                        {mailboxes.map((m) => (
                            <div key={m.id} className="truncate">
                                {m.email}
                            </div>
                        ))}
                    </div>

                    <div className="space-y-1.5">
                        <div className="text-[10px] font-semibold uppercase tracking-wider text-muted-foreground">Target worker</div>
                        <Select value={target || undefined} onValueChange={setTarget}>
                            <SelectTrigger className="h-8 w-full text-[12.5px]">
                                <SelectValue placeholder={workersQ.isLoading ? "Loading workers…" : "Pick a worker"} />
                            </SelectTrigger>
                            <SelectContent>
                                {candidates.length === 0 && (
                                    <div className="px-2 py-1.5 text-xs text-muted-foreground">No other workers.</div>
                                )}
                                {candidates.map((x) => {
                                    const mismatch = x.free_tier !== source.free_tier;
                                    return (
                                        <SelectItem key={x.id} value={x.id} disabled={mismatch} className="text-[12.5px]">
                                            {x.name || x.id.slice(0, 8)} · {x.free_tier ? "free" : "premium"} · {x.worker_type} ·{" "}
                                            {HEALTH_LABEL[x.health_state] ?? x.health_state} · {x.account_count} mailbox{x.account_count === 1 ? "" : "es"}
                                            {mismatch ? " (other tier)" : ""}
                                        </SelectItem>
                                    );
                                })}
                            </SelectContent>
                        </Select>
                        {chosen && chosen.worker_type === "dedicated" && (
                            <p className="text-[11px] text-amber-700">
                                That worker is dedicated to one workspace; only move mailboxes that belong to it.
                            </p>
                        )}
                        {chosen && chosen.health_state !== "healthy" && (
                            <p className="text-[11px] text-amber-700">
                                That worker is {chosen.health_state}; the assignment loop would not place new mailboxes there.
                            </p>
                        )}
                    </div>
                </div>

                <DialogFooter>
                    <Button variant="outline" onClick={() => onOpenChange(false)} disabled={mutation.isPending}>
                        Cancel
                    </Button>
                    <Button onClick={() => mutation.mutate()} disabled={!chosen || !sameTier || mailboxes.length === 0 || mutation.isPending}>
                        {mutation.isPending
                            ? "Moving…"
                            : `Move ${mailboxes.length} to ${chosen ? chosen.name || chosen.id.slice(0, 8) : "worker"}`}
                    </Button>
                </DialogFooter>
            </DialogContent>
        </Dialog>
    );
}
