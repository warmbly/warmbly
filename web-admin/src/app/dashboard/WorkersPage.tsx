// Workers list — distilled from web/src/app/app/admin/workers/page.tsx.
// Same data source (/admin/workers/managed), simpler view: filter +
// table + per-row link into the detail page. Health classification
// reuses the dashboard's online/stale/offline windows.

import { useMemo, useState } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { Link } from "react-router-dom";
import { Rocket } from "lucide-react";
import { PageHeader } from "@/components/layout/PageHeader";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Skeleton } from "@/components/ui/skeleton";
import { listManagedWorkers } from "@/lib/api/client/admin/workers";
import type {
    ManagedWorker,
    WorkerInstallState,
} from "@/lib/api/models/admin";
import { ProvisionModal } from "./ProvisionModal";

const OFFLINE_MS = 5 * 60_000;

function liveness(w: ManagedWorker): { label: string; cls: string } {
    if (!w.last_seen_at) return { label: "no heartbeat", cls: "text-zinc-400" };
    const age = Date.now() - new Date(w.last_seen_at).getTime();
    if (age < 90_000) return { label: "online", cls: "text-emerald-600" };
    if (age < OFFLINE_MS * 2) return { label: "stale", cls: "text-amber-600" };
    return { label: "offline", cls: "text-red-600" };
}

const STATE_TONE: Record<WorkerInstallState, string> = {
    pending: "bg-zinc-100 text-zinc-600",
    provisioning: "bg-amber-100 text-amber-700",
    installed: "bg-emerald-100 text-emerald-700",
    error: "bg-red-100 text-red-700",
    uninstalling: "bg-orange-100 text-orange-700",
    uninstalled: "bg-zinc-100 text-zinc-500",
};

export default function WorkersPage() {
    const qc = useQueryClient();
    const { data, isLoading, error } = useQuery({
        queryKey: ["admin", "workers", "managed"],
        queryFn: listManagedWorkers,
        refetchInterval: 15_000,
    });
    const [filter, setFilter] = useState("");
    const [provisionOpen, setProvisionOpen] = useState(false);

    const rows = useMemo(() => {
        const all = data?.data ?? [];
        const q = filter.trim().toLowerCase();
        if (!q) return all;
        return all.filter((w) => {
            const blob =
                `${w.name} ${w.id} ${w.ssh_host ?? ""} ${w.ip_addr} ${w.worker_type} ${w.image_version ?? ""}`.toLowerCase();
            return blob.includes(q);
        });
    }, [data, filter]);

    return (
        <div>
            <PageHeader
                title="Workers"
                description="Physical worker processes managed over SSH. One worker = one machine running the Warmbly worker binary."
            >
                <Input
                    placeholder="Filter by name, host, version…"
                    value={filter}
                    onChange={(e) => setFilter(e.target.value)}
                    className="w-72"
                />
                <Link
                    to="/workers/provisioning-jobs"
                    className="text-xs text-muted-foreground hover:text-foreground underline"
                >
                    View provisioning jobs
                </Link>
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
                onJobCreated={() => {
                    qc.invalidateQueries({
                        queryKey: ["admin", "provisioning-jobs"],
                    });
                }}
            />

            {isLoading && <SkeletonTable />}
            {error && (
                <div className="text-sm text-red-600 border border-red-200 bg-red-50 rounded-md p-3">
                    Failed to load workers. The /admin/workers/managed endpoint returned an error.
                </div>
            )}

            {!isLoading && (
                <div className="border border-border rounded-lg overflow-hidden bg-card">
                    <table className="w-full text-sm">
                        <thead className="bg-muted/50 text-muted-foreground text-xs uppercase">
                            <tr>
                                <th className="text-left px-3 py-2 font-medium">Name</th>
                                <th className="text-left px-3 py-2 font-medium">Host</th>
                                <th className="text-left px-3 py-2 font-medium">Tier</th>
                                <th className="text-left px-3 py-2 font-medium">Install</th>
                                <th className="text-left px-3 py-2 font-medium">Live</th>
                                <th className="text-left px-3 py-2 font-medium">Mailboxes</th>
                                <th className="text-left px-3 py-2 font-medium">Image</th>
                                <th className="text-left px-3 py-2 font-medium">Seen</th>
                            </tr>
                        </thead>
                        <tbody>
                            {rows.map((w) => {
                                const live = liveness(w);
                                return (
                                    <tr key={w.id} className="border-t border-border hover:bg-muted/30">
                                        <td className="px-3 py-2">
                                            <Link
                                                to={`/workers/${w.id}`}
                                                className="text-[var(--admin-accent-strong)] hover:underline font-medium"
                                            >
                                                {w.name || w.id.slice(0, 8)}
                                            </Link>
                                            <div className="text-[10px] text-muted-foreground font-mono">
                                                {w.id}
                                            </div>
                                        </td>
                                        <td className="px-3 py-2 font-mono text-xs">
                                            {w.ssh_user}@{w.ssh_host || w.ip_addr}:{w.ssh_port}
                                        </td>
                                        <td className="px-3 py-2 text-xs">
                                            {w.worker_type}
                                            {w.worker_type === "shared" && (
                                                <span className="text-muted-foreground">
                                                    {w.free_tier ? " · free" : " · premium"}
                                                </span>
                                            )}
                                        </td>
                                        <td className="px-3 py-2">
                                            <span className={`px-1.5 py-0.5 rounded text-xs ${STATE_TONE[w.install_state]}`}>
                                                {w.install_state}
                                            </span>
                                        </td>
                                        <td className={`px-3 py-2 text-xs font-medium ${live.cls}`}>
                                            {live.label}
                                        </td>
                                        <td className="px-3 py-2 tabular-nums">{w.account_count}</td>
                                        <td className="px-3 py-2 text-xs font-mono">
                                            {w.image_version || (
                                                <span className="text-muted-foreground">—</span>
                                            )}
                                        </td>
                                        <td className="px-3 py-2 text-xs text-muted-foreground">
                                            {w.last_seen_at
                                                ? new Date(w.last_seen_at).toLocaleString()
                                                : "—"}
                                        </td>
                                    </tr>
                                );
                            })}
                            {rows.length === 0 && (
                                <tr>
                                    <td colSpan={8} className="text-center text-muted-foreground py-8 text-sm">
                                        No workers match this filter.
                                    </td>
                                </tr>
                            )}
                        </tbody>
                    </table>
                </div>
            )}

            <div className="mt-3 flex items-center gap-2 text-xs text-muted-foreground">
                <Badge variant="outline" className="text-[10px]">refresh 15s</Badge>
                <span>
                    Worker SSH lifecycle (install / restart / uninstall) lives inside each worker's
                    detail page.
                </span>
            </div>
        </div>
    );
}

function SkeletonTable() {
    return (
        <div className="border border-border rounded-lg p-4 bg-card space-y-2">
            {Array.from({ length: 6 }).map((_, i) => (
                <Skeleton key={i} className="h-7 w-full" />
            ))}
        </div>
    );
}
