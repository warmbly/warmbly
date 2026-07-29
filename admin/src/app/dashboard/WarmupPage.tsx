// Warmup pool admin. The platform's most critical safety surface per
// CLAUDE.md — shared paid-pool reputation is more valuable than maximum
// access for one risky mailbox, so the page foregrounds:
//
//   1. Aggregate health (counts by state, avg spam-score & placement rate)
//   2. Per-pool participant + blocked counts
//   3. Blocked account list with unblock action
//   4. Pending appeals with one-click approve/reject
//
// Everything refetches on a 30s interval so an ops investigator sees
// pool drift in near-real time without needing to reload the tab.

import { useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { toast } from "sonner";
import {
    Activity,
    AlertTriangle,
    CheckCircle2,
    Flame,
    ShieldOff,
    XCircle,
} from "lucide-react";
import { PageHeader } from "@/components/layout/PageHeader";
import { StateLegend } from "@/components/StateLegend";
import { MAILBOX_HEALTH_LEGEND } from "@/lib/legends";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Skeleton } from "@/components/ui/skeleton";
import { ErrorState } from "@/components/ErrorState";
import {
    Dialog,
    DialogContent,
    DialogDescription,
    DialogFooter,
    DialogHeader,
    DialogTitle,
} from "@/components/ui/dialog";
import {
    approveAppeal,
    getWarmupHealthSummary,
    listBlockedWarmupAccounts,
    listWarmupAppeals,
    listWarmupPools,
    rejectAppeal,
    unblockWarmupAccount,
} from "@/lib/api/client/admin/warmup";
import type {
    AdminBlockedAccount,
    WarmupAppeal,
} from "@/lib/api/models/admin";

export default function WarmupPage() {
    return (
        <div>
            <PageHeader
                title="Warmup pools"
                description="Pool health, blocked mailboxes, and pending appeals. Shared paid-pool reputation matters more than any single mailbox — quarantine early."
            >
                <StateLegend label="Health states explained" entries={MAILBOX_HEALTH_LEGEND} />
            </PageHeader>

            <HealthSummary />

            <section className="mt-6">
                <h2 className="text-sm font-semibold mb-2">Pools</h2>
                <PoolsList />
            </section>

            <section className="mt-6">
                <h2 className="text-sm font-semibold mb-2">Blocked mailboxes</h2>
                <BlockedAccounts />
            </section>

            <section className="mt-6">
                <h2 className="text-sm font-semibold mb-2">Appeals queue</h2>
                <AppealsQueue />
            </section>
        </div>
    );
}

function HealthSummary() {
    const { data, isLoading, error, refetch } = useQuery({
        queryKey: ["admin", "warmup", "health"],
        queryFn: getWarmupHealthSummary,
        refetchInterval: 30_000,
    });

    if (isLoading) {
        return (
            <div className="grid gap-3 md:grid-cols-4 mb-6">
                {Array.from({ length: 4 }).map((_, i) => (
                    <Skeleton key={i} className="h-24" />
                ))}
            </div>
        );
    }
    if (error) {
        return (
            <ErrorState
                error={error}
                title="Failed to load warmup health"
                onRetry={() => refetch()}
                className="mb-6"
            />
        );
    }
    if (!data) return null;

    // Guard every field: a partial response must degrade gracefully, never
    // throw and blank the whole tab.
    //
    // `avg_spam_placement_rate` is ALREADY a percent (the backend computes
    // placements/sent*100), so we render it directly — no second *100.
    const placement = data.avg_spam_placement_rate ?? 0;
    const spamPct = placement.toFixed(1);
    const placementTone =
        placement >= 20 ? "text-red-700" : placement >= 10 ? "text-amber-700" : "text-emerald-600";
    const atRisk = data.at_risk_count ?? 0;
    const blocked = data.blocked_count ?? 0;

    // `spam_placement_by_provider` is a Record of RAW COUNTS (backend does
    // COUNT(*)), not rates. Sort worst-first by count so the riskiest
    // mailbox-provider surface is the first thing an investigator sees.
    const byProvider = Object.entries(data.spam_placement_by_provider ?? {}).sort(
        (a, b) => b[1] - a[1],
    );

    return (
        <div className="mb-6">
        <div className="grid gap-3 md:grid-cols-4">
            <HealthCard
                icon={<Activity className="size-4" />}
                title="Total participants"
                value={(data.total_participants ?? 0).toLocaleString()}
                hint={Object.entries(data.by_state ?? {})
                    .map(([k, v]) => `${k}: ${v}`)
                    .join(" · ")}
            />
            <HealthCard
                icon={<AlertTriangle className="size-4" />}
                title="At risk"
                value={atRisk.toLocaleString()}
                hint={`avg spam score ${(data.avg_spam_score ?? 0).toFixed(1)}`}
                tone={atRisk > 0 ? "text-amber-700" : undefined}
            />
            <HealthCard
                icon={<Flame className="size-4" />}
                title="Spam placement"
                value={`${spamPct}%`}
                hint="avg across pool"
                tone={placementTone}
            />
            <HealthCard
                icon={<ShieldOff className="size-4" />}
                title="Blocked"
                value={blocked.toLocaleString()}
                hint="quarantined or hard-blocked"
                tone={blocked > 0 ? "text-red-700" : undefined}
            />
        </div>

        {byProvider.length > 0 && (
            <div className="mt-3 border border-border rounded-lg p-3 bg-card">
                <div className="flex items-center gap-1.5 text-xs text-muted-foreground mb-2">
                    <Flame className="size-4" />
                    <span>Spam placements by provider (count)</span>
                </div>
                <div className="grid gap-2 sm:grid-cols-2 lg:grid-cols-3">
                    {byProvider.map(([provider, count]) => (
                        <div
                            key={provider}
                            className="flex items-center justify-between text-xs"
                        >
                            <span className="capitalize truncate text-foreground">
                                {provider}
                            </span>
                            <span className="tabular-nums font-medium text-foreground">
                                {(count ?? 0).toLocaleString()}
                            </span>
                        </div>
                    ))}
                </div>
            </div>
        )}
        </div>
    );
}

function HealthCard({
    icon,
    title,
    value,
    hint,
    tone,
}: {
    icon: React.ReactNode;
    title: string;
    value: string;
    hint?: string;
    tone?: string;
}) {
    return (
        <div className="border border-border rounded-lg p-3 bg-card">
            <div className="flex items-center gap-1.5 text-xs text-muted-foreground">
                {icon}
                <span>{title}</span>
            </div>
            <div
                className={`text-2xl font-semibold tabular-nums mt-1 ${
                    tone ?? ""
                }`}
            >
                {value}
            </div>
            {hint && (
                <div className="text-[10px] text-muted-foreground truncate mt-0.5">
                    {hint}
                </div>
            )}
        </div>
    );
}

function PoolsList() {
    const { data, isLoading, error, refetch } = useQuery({
        queryKey: ["admin", "warmup", "pools"],
        queryFn: listWarmupPools,
        refetchInterval: 60_000,
    });

    if (isLoading) return <Skeleton className="h-24" />;
    if (error) {
        return <ErrorState error={error} title="Failed to load pools" onRetry={() => refetch()} />;
    }
    const pools = data ?? [];

    return (
        <div className="border border-border rounded-lg overflow-hidden bg-card">
            <table className="w-full text-sm">
                <thead className="bg-muted/50 text-muted-foreground text-xs uppercase">
                    <tr>
                        <th className="text-left px-3 py-2 font-medium">Pool</th>
                        <th className="text-right px-3 py-2 font-medium">Total</th>
                        <th className="text-right px-3 py-2 font-medium">Active</th>
                        <th className="text-right px-3 py-2 font-medium">Blocked</th>
                    </tr>
                </thead>
                <tbody>
                    {pools.map((p) => (
                        <tr key={p.type} className="border-t border-border">
                            <td className="px-3 py-2">
                                <Badge
                                    variant="outline"
                                    className={`text-[10px] ${
                                        p.type === "premium"
                                            ? "border-purple-300 text-purple-700 bg-purple-50"
                                            : "border-zinc-300 text-zinc-700"
                                    }`}
                                >
                                    {p.type}
                                </Badge>
                            </td>
                            <td className="px-3 py-2 text-right tabular-nums">
                                {p.total_participants.toLocaleString()}
                            </td>
                            <td className="px-3 py-2 text-right tabular-nums text-emerald-600">
                                {p.active_participants.toLocaleString()}
                            </td>
                            <td
                                className={`px-3 py-2 text-right tabular-nums ${
                                    p.blocked_count > 0
                                        ? "text-red-700"
                                        : "text-muted-foreground"
                                }`}
                            >
                                {p.blocked_count.toLocaleString()}
                            </td>
                        </tr>
                    ))}
                    {pools.length === 0 && (
                        <tr>
                            <td
                                colSpan={4}
                                className="text-center text-muted-foreground py-6 text-sm"
                            >
                                No pools.
                            </td>
                        </tr>
                    )}
                </tbody>
            </table>
        </div>
    );
}

function BlockedAccounts() {
    const qc = useQueryClient();
    const { data, isLoading, error, refetch } = useQuery({
        queryKey: ["admin", "warmup", "blocked"],
        queryFn: () => listBlockedWarmupAccounts(),
        refetchInterval: 30_000,
    });

    const unblock = useMutation({
        mutationFn: (accountId: string) => unblockWarmupAccount(accountId),
        onSuccess: () => {
            toast.success("Mailbox unblocked");
            qc.invalidateQueries({ queryKey: ["admin", "warmup"] });
        },
        onError: (err: Error) => toast.error(err.message || "Failed to unblock"),
    });

    if (isLoading) return <Skeleton className="h-32" />;
    if (error) {
        return <ErrorState error={error} title="Failed to load blocked mailboxes" onRetry={() => refetch()} />;
    }
    const rows = data?.data ?? [];

    if (rows.length === 0) {
        return (
            <div className="text-sm text-muted-foreground border border-border rounded-md p-4 bg-card">
                No mailboxes are currently blocked.
            </div>
        );
    }

    return (
        <div className="border border-border rounded-lg overflow-hidden bg-card">
            <table className="w-full text-sm">
                <thead className="bg-muted/50 text-muted-foreground text-xs uppercase">
                    <tr>
                        <th className="text-left px-3 py-2 font-medium">Mailbox</th>
                        <th className="text-left px-3 py-2 font-medium">Owner</th>
                        <th className="text-left px-3 py-2 font-medium">Reason</th>
                        <th className="text-left px-3 py-2 font-medium">Blocked</th>
                        <th className="text-left px-3 py-2 font-medium">Appeal</th>
                        <th className="text-right px-3 py-2 font-medium">Action</th>
                    </tr>
                </thead>
                <tbody>
                    {rows.map((a) => (
                        <BlockedRow key={a.id} account={a} onUnblock={() => unblock.mutate(a.id)} />
                    ))}
                </tbody>
            </table>
        </div>
    );
}

function BlockedRow({
    account,
    onUnblock,
}: {
    account: AdminBlockedAccount;
    onUnblock: () => void;
}) {
    return (
        <tr className="border-t border-border">
            <td className="px-3 py-2 font-mono text-xs">{account.email}</td>
            <td className="px-3 py-2 text-xs">
                {account.user?.email ?? account.user_id}
            </td>
            <td className="px-3 py-2 text-xs">{account.block_reason}</td>
            <td className="px-3 py-2 text-xs text-muted-foreground">
                {new Date(account.blocked_at).toLocaleDateString()}
            </td>
            <td className="px-3 py-2 text-xs">
                {account.has_appeal ? (
                    <Badge
                        variant="outline"
                        className="text-[10px] border-amber-300 text-amber-700 bg-amber-50"
                    >
                        {account.appeal_status ?? "pending"}
                    </Badge>
                ) : (
                    <span className="text-muted-foreground">—</span>
                )}
            </td>
            <td className="px-3 py-2 text-right">
                <Button
                    size="sm"
                    variant="outline"
                    onClick={onUnblock}
                    className="text-xs"
                >
                    <CheckCircle2 className="size-3" />
                    Unblock
                </Button>
            </td>
        </tr>
    );
}

function AppealsQueue() {
    const qc = useQueryClient();
    const [reviewing, setReviewing] = useState<{
        appeal: WarmupAppeal;
        mode: "approve" | "reject";
    } | null>(null);

    const { data, isLoading, error, refetch } = useQuery({
        queryKey: ["admin", "warmup", "appeals", "pending"],
        queryFn: () => listWarmupAppeals("pending"),
        refetchInterval: 30_000,
    });

    if (isLoading) return <Skeleton className="h-32" />;
    if (error) {
        return <ErrorState error={error} title="Failed to load appeals" onRetry={() => refetch()} />;
    }
    const rows = data?.data ?? [];

    return (
        <>
            {rows.length === 0 ? (
                <div className="text-sm text-muted-foreground border border-border rounded-md p-4 bg-card">
                    No pending appeals.
                </div>
            ) : (
                <div className="border border-border rounded-lg overflow-hidden bg-card">
                    <table className="w-full text-sm">
                        <thead className="bg-muted/50 text-muted-foreground text-xs uppercase">
                            <tr>
                                <th className="text-left px-3 py-2 font-medium">Mailbox</th>
                                <th className="text-left px-3 py-2 font-medium">User</th>
                                <th className="text-left px-3 py-2 font-medium">Reason</th>
                                <th className="text-left px-3 py-2 font-medium">Submitted</th>
                                <th className="text-right px-3 py-2 font-medium">Action</th>
                            </tr>
                        </thead>
                        <tbody>
                            {rows.map((a) => (
                                <tr key={a.id} className="border-t border-border">
                                    <td className="px-3 py-2 font-mono text-xs">
                                        {a.email_account?.email ?? a.email_account_id}
                                    </td>
                                    <td className="px-3 py-2 text-xs">
                                        {a.user?.email ?? a.user_id}
                                    </td>
                                    <td className="px-3 py-2 text-xs max-w-md truncate">
                                        {a.reason}
                                    </td>
                                    <td className="px-3 py-2 text-xs text-muted-foreground">
                                        {new Date(a.created_at).toLocaleDateString()}
                                    </td>
                                    <td className="px-3 py-2 text-right space-x-1.5">
                                        <Button
                                            size="sm"
                                            onClick={() => setReviewing({ appeal: a, mode: "approve" })}
                                            className="bg-emerald-600 hover:bg-emerald-700 text-white text-xs"
                                        >
                                            <CheckCircle2 className="size-3" /> Approve
                                        </Button>
                                        <Button
                                            size="sm"
                                            onClick={() => setReviewing({ appeal: a, mode: "reject" })}
                                            className="bg-red-600 hover:bg-red-700 text-white text-xs"
                                        >
                                            <XCircle className="size-3" /> Reject
                                        </Button>
                                    </td>
                                </tr>
                            ))}
                        </tbody>
                    </table>
                </div>
            )}

            {reviewing && (
                <ReviewAppealDialog
                    appeal={reviewing.appeal}
                    mode={reviewing.mode}
                    open
                    onOpenChange={(v) => !v && setReviewing(null)}
                    onDone={() => {
                        qc.invalidateQueries({ queryKey: ["admin", "warmup"] });
                        setReviewing(null);
                    }}
                />
            )}
        </>
    );
}

function ReviewAppealDialog({
    appeal,
    mode,
    open,
    onOpenChange,
    onDone,
}: {
    appeal: WarmupAppeal;
    mode: "approve" | "reject";
    open: boolean;
    onOpenChange: (v: boolean) => void;
    onDone: () => void;
}) {
    const [notes, setNotes] = useState("");
    const mutation = useMutation({
        mutationFn: () =>
            mode === "approve"
                ? approveAppeal(appeal.id, { approved: true, notes })
                : rejectAppeal(appeal.id, { approved: false, notes }),
        onSuccess: () => {
            toast.success(`Appeal ${mode === "approve" ? "approved" : "rejected"}`);
            onDone();
        },
        onError: (err: Error) => toast.error(err.message || "Action failed"),
    });

    return (
        <Dialog open={open} onOpenChange={onOpenChange}>
            <DialogContent>
                <DialogHeader>
                    <DialogTitle>
                        {mode === "approve" ? "Approve appeal" : "Reject appeal"}
                    </DialogTitle>
                    <DialogDescription>
                        {mode === "approve"
                            ? "Approving will unblock the mailbox and re-admit it to the pool. Notes are recorded for audit."
                            : "Rejecting keeps the mailbox blocked. Notes are recorded for audit and may be shown to the user."}
                    </DialogDescription>
                </DialogHeader>
                <div>
                    <Label htmlFor="notes" className="text-xs font-medium">
                        Review notes
                    </Label>
                    <Input
                        id="notes"
                        placeholder="Brief justification (required)"
                        value={notes}
                        onChange={(e) => setNotes(e.target.value)}
                        autoFocus
                    />
                </div>
                <DialogFooter>
                    <Button variant="outline" onClick={() => onOpenChange(false)}>
                        Cancel
                    </Button>
                    <Button
                        onClick={() => {
                            if (notes.trim() === "") {
                                toast.error("Notes are required");
                                return;
                            }
                            mutation.mutate();
                        }}
                        disabled={mutation.isPending}
                        className={
                            mode === "approve"
                                ? "bg-emerald-600 hover:bg-emerald-700 text-white"
                                : "bg-red-600 hover:bg-red-700 text-white"
                        }
                    >
                        {mutation.isPending
                            ? "Working…"
                            : mode === "approve"
                            ? "Approve"
                            : "Reject"}
                    </Button>
                </DialogFooter>
            </DialogContent>
        </Dialog>
    );
}
