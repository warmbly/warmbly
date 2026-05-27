// Egresses — sending identities.
//
// Today an egress and a worker are 1:1 (one IP per worker process), so
// this view is fed by /admin/workers/managed and re-frames the same
// rows around the IP/identity instead of the SSH machine. When the
// backend ships a real /admin/egresses endpoint, swap the data source
// and drop the TODO below — the rest of the column model stays the
// same.

import { useMemo, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { Network } from "lucide-react";
import { PageHeader } from "@/components/layout/PageHeader";
import { Badge } from "@/components/ui/badge";
import { Input } from "@/components/ui/input";
import { Skeleton } from "@/components/ui/skeleton";
import { listManagedWorkers } from "@/lib/api/client/admin/workers";
import type { ManagedWorker } from "@/lib/api/models/admin";

interface EgressRow {
    id: string;
    ip: string;
    bind_label: string;
    worker_id: string;
    worker_name: string;
    worker_type: string;
    risk_pool: string;
    mailbox_count: number;
    health_score: number; // 0-100, higher = healthier
    health_reason: string;
}

function computeEgress(w: ManagedWorker): EgressRow {
    // Until the backend exposes per-egress deliverability scores, derive
    // a coarse health score from what we *do* know: install state,
    // liveness, risk pool. This is a placeholder, not the real source
    // of truth — replace once /admin/egresses ships its own score.
    let score = 100;
    let reason = "healthy";
    if (w.install_state !== "installed") {
        score -= 50;
        reason = `install ${w.install_state}`;
    }
    if (w.last_seen_at) {
        const age = Date.now() - new Date(w.last_seen_at).getTime();
        if (age > 5 * 60_000) {
            score -= 25;
            reason = "stale heartbeat";
        }
        if (age > 30 * 60_000) {
            score -= 20;
            reason = "offline";
        }
    } else {
        score -= 30;
        reason = "no heartbeat";
    }
    if (w.risk_pool === "risky") {
        score -= 10;
        reason = "risky pool";
    } else if (w.risk_pool === "quarantine") {
        score -= 40;
        reason = "quarantined";
    }
    return {
        id: `eg-${w.id}`,
        ip: w.ip_addr || "—",
        bind_label: w.ssh_host || w.ip_addr || w.id.slice(0, 8),
        worker_id: w.id,
        worker_name: w.name || w.id.slice(0, 8),
        worker_type: w.worker_type,
        risk_pool: w.risk_pool,
        mailbox_count: w.account_count ?? 0,
        health_score: Math.max(0, Math.min(100, score)),
        health_reason: reason,
    };
}

function scoreTone(score: number): { cls: string; label: string } {
    if (score >= 80) return { cls: "bg-emerald-100 text-emerald-700 border-emerald-200", label: "good" };
    if (score >= 50) return { cls: "bg-amber-100 text-amber-700 border-amber-200", label: "watch" };
    return { cls: "bg-red-100 text-red-700 border-red-200", label: "at risk" };
}

export default function EgressesPage() {
    const { data, isLoading } = useQuery({
        queryKey: ["admin", "workers", "managed"],
        queryFn: listManagedWorkers,
        refetchInterval: 30_000,
    });

    const [filter, setFilter] = useState("");

    const rows = useMemo<EgressRow[]>(() => {
        const all = (data?.data ?? []).map(computeEgress);
        const q = filter.trim().toLowerCase();
        if (!q) return all;
        return all.filter((e) =>
            `${e.ip} ${e.bind_label} ${e.worker_name}`.toLowerCase().includes(q),
        );
    }, [data, filter]);

    const totalMailboxes = rows.reduce((a, r) => a + r.mailbox_count, 0);

    return (
        <div>
            <PageHeader
                title="Egresses"
                description="Sending identities (one IP per worker process today). Distinct from the Workers page in framing: this view is about deliverability surface, not SSH lifecycle."
            >
                <Input
                    placeholder="Filter by IP, host, name…"
                    value={filter}
                    onChange={(e) => setFilter(e.target.value)}
                    className="w-72"
                />
            </PageHeader>

            <div className="grid grid-cols-1 sm:grid-cols-3 gap-3 mb-4">
                <SummaryCard label="Active egresses" value={rows.length} />
                <SummaryCard label="Mailboxes routed" value={totalMailboxes} />
                <SummaryCard
                    label="Avg health"
                    value={
                        rows.length === 0
                            ? "—"
                            : `${Math.round(
                                  rows.reduce((a, r) => a + r.health_score, 0) / rows.length,
                              )} / 100`
                    }
                />
            </div>

            <div className="rounded-md border border-amber-200 bg-amber-50 text-amber-800 text-xs p-2.5 mb-3 flex items-start gap-2">
                <Network className="size-4 mt-0.5 shrink-0" />
                <div>
                    <strong>TODO (backend):</strong> this page proxies <code>/admin/workers/managed</code>{" "}
                    while a dedicated <code>/admin/egresses</code> endpoint is implemented. Health scores
                    are derived client-side from worker liveness + risk pool; replace with real
                    deliverability metrics once the backend exposes them.
                </div>
            </div>

            {isLoading && <Skeleton className="h-40 w-full" />}

            {!isLoading && (
                <div className="border border-border rounded-lg overflow-hidden bg-card">
                    <table className="w-full text-sm">
                        <thead className="bg-muted/50 text-muted-foreground text-xs uppercase">
                            <tr>
                                <th className="text-left px-3 py-2 font-medium">Bind IP</th>
                                <th className="text-left px-3 py-2 font-medium">Identity</th>
                                <th className="text-left px-3 py-2 font-medium">Type</th>
                                <th className="text-left px-3 py-2 font-medium">Pool</th>
                                <th className="text-left px-3 py-2 font-medium">Mailboxes</th>
                                <th className="text-left px-3 py-2 font-medium">Health</th>
                            </tr>
                        </thead>
                        <tbody>
                            {rows.map((r) => {
                                const tone = scoreTone(r.health_score);
                                return (
                                    <tr key={r.id} className="border-t border-border hover:bg-muted/30">
                                        <td className="px-3 py-2 font-mono text-xs">{r.ip}</td>
                                        <td className="px-3 py-2">
                                            <div className="font-medium">{r.worker_name}</div>
                                            <div className="text-[10px] text-muted-foreground font-mono">
                                                {r.bind_label}
                                            </div>
                                        </td>
                                        <td className="px-3 py-2 text-xs">{r.worker_type}</td>
                                        <td className="px-3 py-2 text-xs">{r.risk_pool}</td>
                                        <td className="px-3 py-2 tabular-nums">{r.mailbox_count}</td>
                                        <td className="px-3 py-2">
                                            <Badge
                                                variant="outline"
                                                className={`${tone.cls} text-xs`}
                                                title={r.health_reason}
                                            >
                                                {tone.label} · {r.health_score}
                                            </Badge>
                                        </td>
                                    </tr>
                                );
                            })}
                            {rows.length === 0 && (
                                <tr>
                                    <td colSpan={6} className="text-center text-muted-foreground py-8 text-sm">
                                        No egresses match this filter.
                                    </td>
                                </tr>
                            )}
                        </tbody>
                    </table>
                </div>
            )}
        </div>
    );
}

function SummaryCard({ label, value }: { label: string; value: number | string }) {
    return (
        <div className="rounded-lg border border-border bg-card p-4">
            <div className="text-xs font-medium uppercase tracking-wider text-muted-foreground">
                {label}
            </div>
            <div className="mt-1 text-2xl font-semibold tabular-nums">{value}</div>
        </div>
    );
}
