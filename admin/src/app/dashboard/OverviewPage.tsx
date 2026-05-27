// Platform overview — count cards backed by /admin/analytics/overview.
//
// The endpoint returns a loose bag of numbers; we pluck the ones we
// know about and gracefully render "—" for anything missing so the
// page never crashes when the backend grows new fields.

import { useQuery } from "@tanstack/react-query";
import {
    Activity,
    AlertTriangle,
    Mailbox,
    Send,
    Server,
    Users,
} from "lucide-react";
import { PageHeader } from "@/components/layout/PageHeader";
import { Card, CardContent, CardHeader, CardTitle, CardDescription } from "@/components/ui/card";
import { Skeleton } from "@/components/ui/skeleton";
import { getPlatformOverview } from "@/lib/api/client/admin/analytics";
import { listManagedWorkers } from "@/lib/api/client/admin/workers";
import type { ManagedWorker } from "@/lib/api/models/admin";

function formatNum(n: number | undefined): string {
    if (n === undefined || n === null || Number.isNaN(n)) return "—";
    return new Intl.NumberFormat("en-US").format(n);
}

function formatPct(n: number | undefined): string {
    if (n === undefined || n === null || Number.isNaN(n)) return "—";
    // Backend may send a fraction (0.012) or a percent (1.2). Detect.
    const pct = Math.abs(n) <= 1 ? n * 100 : n;
    return `${pct.toFixed(2)}%`;
}

export default function OverviewPage() {
    const overviewQ = useQuery({
        queryKey: ["admin", "analytics", "overview"],
        queryFn: getPlatformOverview,
        refetchInterval: 60_000,
    });

    // Workers list — used to render a quick health summary card even
    // when the analytics overview endpoint hasn't been wired yet.
    const workersQ = useQuery({
        queryKey: ["admin", "workers", "managed"],
        queryFn: listManagedWorkers,
        refetchInterval: 30_000,
    });

    const ov = overviewQ.data;
    const workers = workersQ.data?.data ?? [];

    const computed = {
        workers_total: ov?.workers_total ?? workers.length,
        workers_active:
            ov?.workers_active ??
            workers.filter((w: ManagedWorker) => w.install_state === "installed" && isOnline(w)).length,
        workers_offline:
            ov?.workers_offline ??
            workers.filter((w: ManagedWorker) => w.install_state === "installed" && !isOnline(w)).length,
        mailboxes_connected:
            ov?.mailboxes_connected ??
            workers.reduce((a: number, w: ManagedWorker) => a + (w.account_count ?? 0), 0),
    };

    return (
        <div>
            <PageHeader
                title="Platform overview"
                description="Snapshot of the Warmbly control plane. Live counts pull from /admin/analytics/overview every minute."
            />

            <div className="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-4 gap-3 mb-6">
                <StatCard
                    icon={Server}
                    label="Workers active"
                    value={formatNum(computed.workers_active)}
                    sub={`${formatNum(computed.workers_total)} total · ${formatNum(computed.workers_offline)} offline`}
                    loading={overviewQ.isLoading && workersQ.isLoading}
                />
                <StatCard
                    icon={Mailbox}
                    label="Mailboxes connected"
                    value={formatNum(computed.mailboxes_connected)}
                    sub="Across all assigned workers"
                    loading={overviewQ.isLoading && workersQ.isLoading}
                />
                <StatCard
                    icon={Send}
                    label="Emails sent · 24h"
                    value={formatNum(ov?.emails_sent_24h)}
                    sub={`${formatNum(ov?.emails_sent_7d)} over 7 days`}
                    loading={overviewQ.isLoading}
                />
                <StatCard
                    icon={AlertTriangle}
                    label="Bounce rate · 7d"
                    value={formatPct(ov?.bounce_rate_7d)}
                    sub={`Complaint rate ${formatPct(ov?.complaint_rate_7d)}`}
                    loading={overviewQ.isLoading}
                    tone={shouldWarnBounce(ov?.bounce_rate_7d) ? "warn" : "neutral"}
                />
            </div>

            <div className="grid grid-cols-1 lg:grid-cols-3 gap-3">
                <Card className="lg:col-span-2">
                    <CardHeader>
                        <CardTitle>Worker fleet</CardTitle>
                        <CardDescription>
                            Live snapshot from <code>/admin/workers/managed</code>. Click any row in the
                            Workers page for SSH actions + logs.
                        </CardDescription>
                    </CardHeader>
                    <CardContent className="pt-0">
                        <ul className="divide-y divide-border">
                            {workersQ.isLoading && (
                                <li className="py-4">
                                    <Skeleton className="h-4 w-3/4" />
                                </li>
                            )}
                            {!workersQ.isLoading && workers.length === 0 && (
                                <li className="py-6 text-sm text-muted-foreground text-center">
                                    No workers registered yet.
                                </li>
                            )}
                            {workers.slice(0, 6).map((w) => (
                                <li key={w.id} className="py-2 flex items-center gap-3 text-sm">
                                    <span
                                        className={`size-2 rounded-full ${
                                            isOnline(w) ? "bg-emerald-500" : "bg-zinc-300"
                                        }`}
                                    />
                                    <span className="font-medium truncate flex-1">
                                        {w.name || w.id.slice(0, 8)}
                                    </span>
                                    <span className="text-xs text-muted-foreground hidden sm:inline">
                                        {w.worker_type}
                                        {w.worker_type === "shared" &&
                                            (w.free_tier ? " · free" : " · premium")}
                                    </span>
                                    <span className="text-xs text-muted-foreground tabular-nums">
                                        {w.account_count} mailboxes
                                    </span>
                                </li>
                            ))}
                        </ul>
                    </CardContent>
                </Card>

                <Card>
                    <CardHeader>
                        <CardTitle>Activity</CardTitle>
                        <CardDescription>Quick-glance counters.</CardDescription>
                    </CardHeader>
                    <CardContent className="pt-0 space-y-3">
                        <StatRow icon={Users} label="Users (total)" value={formatNum(ov?.users_total)} />
                        <StatRow
                            icon={Activity}
                            label="Users active · 30d"
                            value={formatNum(ov?.users_active_30d)}
                        />
                        <StatRow
                            icon={Send}
                            label="Campaigns running"
                            value={formatNum(ov?.campaigns_running)}
                        />
                    </CardContent>
                </Card>
            </div>
        </div>
    );
}

function isOnline(w: ManagedWorker): boolean {
    if (!w.last_seen_at) return false;
    return Date.now() - new Date(w.last_seen_at).getTime() < 5 * 60_000;
}

// CLAUDE.md sets the deliverability ceiling: stay well below the 0.1% /
// 0.3% complaint thresholds. We start warning at 3% bounce so an admin
// gets a visible nudge before provider-level enforcement kicks in.
function shouldWarnBounce(rate?: number): boolean {
    if (rate === undefined) return false;
    const pct = Math.abs(rate) <= 1 ? rate * 100 : rate;
    return pct >= 3;
}

interface StatCardProps {
    icon: React.ComponentType<{ className?: string }>;
    label: string;
    value: string;
    sub?: string;
    loading?: boolean;
    tone?: "neutral" | "warn";
}

function StatCard({ icon: Icon, label, value, sub, loading, tone = "neutral" }: StatCardProps) {
    return (
        <Card className={tone === "warn" ? "border-amber-300 bg-amber-50/40" : undefined}>
            <CardContent className="p-4">
                <div className="flex items-center gap-2 text-xs font-medium uppercase tracking-wider text-muted-foreground">
                    <Icon className="size-3.5" />
                    {label}
                </div>
                <div className="mt-2 text-2xl font-semibold tabular-nums">
                    {loading ? <Skeleton className="h-7 w-16" /> : value}
                </div>
                {sub && <div className="text-xs text-muted-foreground mt-1">{sub}</div>}
            </CardContent>
        </Card>
    );
}

function StatRow({
    icon: Icon,
    label,
    value,
}: {
    icon: React.ComponentType<{ className?: string }>;
    label: string;
    value: string;
}) {
    return (
        <div className="flex items-center justify-between">
            <span className="flex items-center gap-2 text-sm text-muted-foreground">
                <Icon className="size-4" />
                {label}
            </span>
            <span className="font-medium tabular-nums">{value}</span>
        </div>
    );
}
