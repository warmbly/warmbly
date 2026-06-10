import { useMemo, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { Link } from "react-router-dom";
import { listManagedWorkers } from "@/lib/api/client/app/admin/workers";
import { listWorkerProfiles } from "@/lib/api/client/app/admin/credentials";
import type { ManagedWorker, WorkerInstallState } from "@/lib/api/models/app/admin/Worker";
import type { WorkerProfile } from "@/lib/api/models/app/admin/Credentials";
import { TagsCell, smartLabels } from "../_components/WorkerLabels";

const stateStyle: Record<WorkerInstallState, string> = {
    pending: "bg-slate-100 text-slate-600",
    provisioning: "bg-amber-100 text-amber-700",
    installed: "bg-green-100 text-green-700",
    error: "bg-red-100 text-red-700",
    uninstalling: "bg-orange-100 text-orange-700",
    uninstalled: "bg-slate-100 text-slate-500",
};

const OFFLINE_MS = 5 * 60_000;

function liveness(w: ManagedWorker): { label: string; cls: string } {
    if (!w.last_seen_at) return { label: "no heartbeat", cls: "text-slate-400" };
    const ageMs = Date.now() - new Date(w.last_seen_at).getTime();
    if (ageMs < 90_000) return { label: "online", cls: "text-green-600" };
    if (ageMs < OFFLINE_MS * 2) return { label: "stale", cls: "text-amber-600" };
    return { label: "offline", cls: "text-red-600" };
}

interface Health {
    errored: ManagedWorker[];
    offline: ManagedWorker[];
    staleConfig: ManagedWorker[];
    updateAvailable: ManagedWorker[];
    inProgress: ManagedWorker[];
}

function computeHealth(workers: ManagedWorker[], profilesById: Map<string, WorkerProfile>): Health {
    const h: Health = { errored: [], offline: [], staleConfig: [], updateAvailable: [], inProgress: [] };
    for (const w of workers) {
        if (w.install_state === "error") {
            h.errored.push(w);
            continue;
        }
        if (w.install_state === "pending" || w.install_state === "provisioning" || w.install_state === "uninstalling") {
            h.inProgress.push(w);
            continue;
        }
        if (w.install_state !== "installed") continue;
        // Liveness
        const seenAge = w.last_seen_at ? Date.now() - new Date(w.last_seen_at).getTime() : Infinity;
        if (seenAge > OFFLINE_MS) {
            h.offline.push(w);
        }
        const profile = w.profile_id ? profilesById.get(w.profile_id) : undefined;
        if (profile) {
            // Stale config: profile updated after this worker last applied.
            if (
                w.config_applied_at &&
                new Date(profile.updated_at) > new Date(w.config_applied_at)
            ) {
                h.staleConfig.push(w);
            }
            // Update available: profile has a resolved tag, worker is on a different version.
            if (
                profile.resolved_image_tag &&
                w.image_version &&
                w.image_version !== profile.resolved_image_tag
            ) {
                h.updateAvailable.push(w);
            }
        }
    }
    return h;
}

type Filter = "all" | "errored" | "offline" | "stale_config" | "update_available" | "in_progress";

export default function AdminWorkersPage() {
    const { data, isLoading, error, refetch } = useQuery({
        queryKey: ["admin", "workers", "managed"],
        queryFn: listManagedWorkers,
        refetchInterval: 15_000,
    });
    const profilesQ = useQuery({ queryKey: ["admin", "profiles"], queryFn: listWorkerProfiles });

    const profilesById = useMemo(() => {
        const m = new Map<string, WorkerProfile>();
        for (const p of profilesQ.data?.data ?? []) m.set(p.id, p);
        return m;
    }, [profilesQ.data]);

    const health = useMemo(() => computeHealth(data?.data ?? [], profilesById), [data, profilesById]);

    const [filter, setFilter] = useState<Filter>("all");
    const [tagFilter, setTagFilter] = useState<string | null>(null);

    const filteredWorkers = useMemo(() => {
        const all = data?.data ?? [];
        let base: ManagedWorker[];
        switch (filter) {
            case "errored":          base = health.errored; break;
            case "offline":          base = health.offline; break;
            case "stale_config":     base = health.staleConfig; break;
            case "update_available": base = health.updateAvailable; break;
            case "in_progress":      base = health.inProgress; break;
            default:                 base = all;
        }
        if (tagFilter) {
            base = base.filter((w) => {
                const labels = [...(w.tags ?? []), ...smartLabels(w)];
                return labels.includes(tagFilter);
            });
        }
        return base;
    }, [filter, data, health, tagFilter]);

    // Tag frequency across all workers (user tags + auto labels) for the
    // "filter by tag" chip strip.
    const tagFrequency = useMemo(() => {
        const counts = new Map<string, number>();
        for (const w of data?.data ?? []) {
            const labels = [...(w.tags ?? []), ...smartLabels(w)];
            for (const t of labels) counts.set(t, (counts.get(t) ?? 0) + 1);
        }
        return Array.from(counts.entries())
            .sort((a, b) => b[1] - a[1])
            .slice(0, 16);
    }, [data]);

    const totalNeedsAttention =
        health.errored.length + health.offline.length + health.inProgress.length;

    return (
        <div>
            <div className="flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between mb-4">
                <div>
                    <h2 className="text-slate-700 font-semibold text-lg">Managed Workers</h2>
                    <p className="text-slate-400 text-sm">
                        VPSes added here are managed over SSH from this control plane.
                    </p>
                </div>
                <Link
                    to="/app/admin/workers/new"
                    className="self-start sm:self-auto shrink-0 whitespace-nowrap bg-blue-600 hover:bg-blue-700 text-white px-4 py-2 rounded-lg text-sm font-medium"
                >
                    + Add Worker
                </Link>
            </div>

            {/* Health banner */}
            {totalNeedsAttention > 0 && (
                <div className="border border-amber-300 bg-amber-50 rounded-lg p-3 mb-4">
                    <div className="flex items-center justify-between">
                        <div className="text-sm">
                            <span className="font-semibold text-amber-800">
                                {totalNeedsAttention} worker{totalNeedsAttention === 1 ? "" : "s"} need attention
                            </span>
                            <span className="text-amber-700 ml-2">
                                — click a chip below to filter
                            </span>
                        </div>
                        <button
                            onClick={() => setFilter("all")}
                            className="text-xs text-amber-700 hover:underline"
                            disabled={filter === "all"}
                        >
                            clear filter
                        </button>
                    </div>
                </div>
            )}

            {/* Filter chips */}
            <div className="flex flex-wrap gap-2 mb-3">
                <Chip active={filter === "all"} onClick={() => setFilter("all")} tone="slate">
                    all <Count value={data?.data?.length ?? 0} />
                </Chip>
                <Chip active={filter === "errored"} onClick={() => setFilter("errored")} tone="red" disabled={health.errored.length === 0}>
                    errored <Count value={health.errored.length} />
                </Chip>
                <Chip active={filter === "offline"} onClick={() => setFilter("offline")} tone="red" disabled={health.offline.length === 0}>
                    offline <Count value={health.offline.length} />
                </Chip>
                <Chip active={filter === "in_progress"} onClick={() => setFilter("in_progress")} tone="amber" disabled={health.inProgress.length === 0}>
                    in progress <Count value={health.inProgress.length} />
                </Chip>
                <Chip active={filter === "stale_config"} onClick={() => setFilter("stale_config")} tone="amber" disabled={health.staleConfig.length === 0}>
                    stale config <Count value={health.staleConfig.length} />
                </Chip>
                <Chip active={filter === "update_available"} onClick={() => setFilter("update_available")} tone="blue" disabled={health.updateAvailable.length === 0}>
                    update available <Count value={health.updateAvailable.length} />
                </Chip>
            </div>

            {/* Categorize by tag — combines admin tags + smart auto-labels. */}
            {tagFrequency.length > 0 && (
                <div className="flex flex-wrap items-center gap-1.5 mb-3 text-xs">
                    <span className="text-slate-400 mr-1">filter by tag:</span>
                    {tagFrequency.map(([t, n]) => (
                        <button
                            key={t}
                            onClick={() => setTagFilter(tagFilter === t ? null : t)}
                            className={`px-2 py-0.5 rounded border ${
                                tagFilter === t
                                    ? "bg-slate-700 text-white border-slate-700"
                                    : "border-slate-200 text-slate-600 hover:bg-slate-50"
                            }`}
                        >
                            {t} <span className="opacity-70">{n}</span>
                        </button>
                    ))}
                    {tagFilter && (
                        <button onClick={() => setTagFilter(null)} className="text-slate-500 hover:underline">
                            clear
                        </button>
                    )}
                </div>
            )}

            {isLoading && <p className="text-slate-400 text-sm">Loading…</p>}
            {error && (
                <p className="text-red-600 text-sm">
                    Failed to load workers.{" "}
                    <button onClick={() => refetch()} className="underline">retry</button>
                </p>
            )}

            <div className="border rounded-lg overflow-hidden">
                <div className="overflow-x-auto">
                <table className="w-full text-sm">
                    <thead className="bg-slate-50 text-slate-500 text-xs uppercase">
                        <tr>
                            <th className="text-left px-3 py-2">Name</th>
                            <th className="text-left px-3 py-2">Host</th>
                            <th className="hidden md:table-cell text-left px-3 py-2">Tier</th>
                            <th className="text-left px-3 py-2">Install</th>
                            <th className="text-left px-3 py-2">Pool</th>
                            <th className="hidden md:table-cell text-left px-3 py-2">Version</th>
                            <th className="text-left px-3 py-2">Live</th>
                            <th className="text-left px-3 py-2">Accounts</th>
                            <th className="hidden md:table-cell text-left px-3 py-2">Tags</th>
                            <th className="hidden md:table-cell text-left px-3 py-2">Last seen</th>
                        </tr>
                    </thead>
                    <tbody>
                        {filteredWorkers.map((w) => {
                            const live = liveness(w);
                            return (
                                <tr key={w.id} className="border-t hover:bg-slate-50">
                                    <td className="px-3 py-2">
                                        <Link to={`/app/admin/workers/${w.id}`} className="text-blue-600 hover:underline">
                                            {w.name || w.id.slice(0, 8)}
                                        </Link>
                                        <div className="hidden md:block text-slate-400 text-xs">{w.id}</div>
                                    </td>
                                    <td className="px-3 py-2 font-mono text-xs">
                                        {w.ssh_user}@{w.ssh_host || w.ip_addr}:{w.ssh_port}
                                    </td>
                                    <td className="hidden md:table-cell px-3 py-2">
                                        <span className="text-slate-600">
                                            {w.worker_type}
                                            {w.worker_type === "shared" && (w.free_tier ? " · free" : " · premium")}
                                        </span>
                                    </td>
                                    <td className="px-3 py-2">
                                        <span className={`px-2 py-0.5 rounded text-xs ${stateStyle[w.install_state]}`}>
                                            {w.install_state}
                                        </span>
                                    </td>
                                    <td className="px-3 py-2">
                                        {w.worker_type === "dedicated" ? (
                                            <span className="text-slate-400 text-xs">n/a</span>
                                        ) : (
                                            <RiskPoolBadge pool={w.risk_pool} />
                                        )}
                                    </td>
                                    <td className="hidden md:table-cell px-3 py-2 text-xs font-mono">
                                        {w.image_version || <span className="text-slate-400">—</span>}
                                    </td>
                                    <td className={`px-3 py-2 text-xs font-medium ${live.cls}`}>
                                        {live.label}
                                    </td>
                                    <td className="px-3 py-2">{w.account_count}</td>
                                    <td className="hidden md:table-cell px-3 py-2">
                                        <TagsCell worker={w} />
                                    </td>
                                    <td className="hidden md:table-cell px-3 py-2 text-slate-500 text-xs">
                                        {w.last_seen_at
                                            ? new Date(w.last_seen_at).toLocaleString()
                                            : "—"}
                                    </td>
                                </tr>
                            );
                        })}
                        {filteredWorkers.length === 0 && (
                            <tr>
                                <td colSpan={10} className="px-3 py-8 text-center text-slate-400 text-sm">
                                    {data?.data?.length === 0
                                        ? "No workers yet. Add one to get started."
                                        : "No workers match this filter."}
                                </td>
                            </tr>
                        )}
                    </tbody>
                </table>
                </div>
            </div>
        </div>
    );
}

function Chip({
    active,
    onClick,
    disabled,
    tone,
    children,
}: {
    active: boolean;
    onClick: () => void;
    disabled?: boolean;
    tone: "slate" | "red" | "amber" | "blue";
    children: React.ReactNode;
}) {
    const toneCls = {
        slate: active ? "bg-slate-700 text-white border-slate-700" : "border-slate-200 text-slate-600 hover:bg-slate-50",
        red:   active ? "bg-red-600 text-white border-red-600"     : "border-red-200 text-red-700 hover:bg-red-50",
        amber: active ? "bg-amber-600 text-white border-amber-600" : "border-amber-200 text-amber-700 hover:bg-amber-50",
        blue:  active ? "bg-blue-600 text-white border-blue-600"   : "border-blue-200 text-blue-700 hover:bg-blue-50",
    }[tone];
    return (
        <button
            onClick={onClick}
            disabled={disabled}
            className={`px-2.5 py-1 text-xs rounded-full border ${toneCls} disabled:opacity-40 disabled:cursor-not-allowed`}
        >
            {children}
        </button>
    );
}

function Count({ value }: { value: number }) {
    return <span className="ml-1 font-semibold tabular-nums">{value}</span>;
}

function RiskPoolBadge({ pool }: { pool: "clean" | "risky" | "quarantine" }) {
    const cls = {
        clean:      "bg-green-100 text-green-700",
        risky:      "bg-amber-100 text-amber-700",
        quarantine: "bg-red-100 text-red-700",
    }[pool];
    return <span className={`px-2 py-0.5 rounded text-xs ${cls}`}>{pool}</span>;
}
