// Warmup appeals review surface. Where the Warmup page is the at-a-glance
// pool monitor, this page is the focused enforcement queue: admins triage
// warmup-ban appeals (approve = unblock the mailbox, reject = stays blocked)
// and can unblock blocked mailboxes directly.
//
//   - Appeals tab: filterable by status (pending/approved/rejected), with
//     approve/reject actions on pending rows that capture optional review
//     notes. The pending view polls so the queue stays current.
//   - Blocked mailboxes tab: every mailbox currently blocked from the pool,
//     with whether it has an open appeal and a direct (confirmed) unblock.
//
// Mirrors LimitRequestsPage's review pattern so the two enforcement queues
// read the same way.

import { useEffect, useState } from "react";
import {
    keepPreviousData,
    useMutation,
    useQuery,
    useQueryClient,
} from "@tanstack/react-query";
import { toast } from "sonner";
import { CheckCircle2, ShieldCheck, XCircle } from "lucide-react";
import { PageHeader } from "@/components/layout/PageHeader";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Label } from "@/components/ui/label";
import { Textarea } from "@/components/ui/textarea";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import {
    Dialog,
    DialogContent,
    DialogDescription,
    DialogFooter,
    DialogHeader,
    DialogTitle,
} from "@/components/ui/dialog";
import {
    Explorer,
    FilterGroup,
    SelectFilter,
} from "@/components/data/Explorer";
import { DataTable, type Column } from "@/components/data/DataTable";
import { useCursorPager } from "@/lib/useCursorPager";
import {
    approveAppeal,
    listBlockedWarmupAccounts,
    listWarmupAppeals,
    rejectAppeal,
    unblockWarmupAccount,
} from "@/lib/api/client/admin/warmup";
import type {
    AdminBlockedAccount,
    WarmupAppeal,
    WarmupAppealStatus,
} from "@/lib/api/models/admin";

const STATUS_TONE: Record<WarmupAppealStatus, string> = {
    pending: "border-amber-300 text-amber-700 bg-amber-50",
    approved: "border-emerald-300 text-emerald-700 bg-emerald-50",
    rejected: "border-red-300 text-red-700 bg-red-50",
};

type AppealStatusFilter = WarmupAppealStatus | "all";

const STATUS_OPTIONS: { value: AppealStatusFilter; label: string }[] = [
    { value: "pending", label: "Pending" },
    { value: "approved", label: "Approved" },
    { value: "rejected", label: "Rejected" },
    { value: "all", label: "All statuses" },
];

export default function WarmupAppealsPage() {
    const [tab, setTab] = useState("appeals");

    return (
        <div>
            <PageHeader
                title="Warmup appeals"
                description="Review warmup-ban appeals and unblock mailboxes. Approving an appeal unblocks the mailbox and re-admits it to the pool; rejecting keeps it blocked. Shared paid-pool reputation matters more than any single mailbox."
            />

            <Tabs value={tab} onValueChange={setTab}>
                <TabsList variant="line">
                    <TabsTrigger value="appeals">Appeals</TabsTrigger>
                    <TabsTrigger value="blocked">Blocked mailboxes</TabsTrigger>
                </TabsList>

                <TabsContent value="appeals" className="mt-5">
                    <AppealsTab />
                </TabsContent>
                <TabsContent value="blocked" className="mt-5">
                    <BlockedTab />
                </TabsContent>
            </Tabs>
        </div>
    );
}

function AppealsTab() {
    const [status, setStatus] = useState<AppealStatusFilter>("pending");
    const pager = useCursorPager();
    const { reset } = pager;

    const [reviewing, setReviewing] = useState<{
        appeal: WarmupAppeal;
        mode: "approve" | "reject";
    } | null>(null);

    useEffect(() => {
        reset();
    }, [status, reset]);

    const { data, isLoading, error, refetch } = useQuery({
        queryKey: ["admin", "warmup", "appeals", status, pager.cursor],
        queryFn: () => listWarmupAppeals(status, pager.cursor),
        // Keep the pending queue current without a manual refresh.
        refetchInterval: status === "pending" ? 15_000 : false,
        placeholderData: keepPreviousData,
        staleTime: 10_000,
    });

    const rows = data?.data ?? [];
    const activeCount = status !== "pending" ? 1 : 0;

    const columns: Column<WarmupAppeal>[] = [
        {
            id: "mailbox",
            header: "Mailbox",
            cell: (a) => (
                <span className="font-mono text-xs">
                    {a.email_account?.email ?? a.email_account_id}
                </span>
            ),
            csv: (a) => a.email_account?.email ?? a.email_account_id,
        },
        {
            id: "user",
            header: "User",
            cell: (a) => (
                <span className="text-xs">{a.user?.email ?? a.user_id}</span>
            ),
            csv: (a) => a.user?.email ?? a.user_id,
        },
        {
            id: "reason",
            header: "Reason",
            cell: (a) => (
                <span className="block max-w-md truncate text-xs" title={a.reason}>
                    {a.reason}
                </span>
            ),
            csv: (a) => a.reason,
        },
        {
            id: "status",
            header: "Status",
            cell: (a) => (
                <div>
                    <Badge
                        variant="outline"
                        className={`text-[10px] ${STATUS_TONE[a.status]}`}
                    >
                        {a.status}
                    </Badge>
                    {a.review_notes && a.status !== "pending" && (
                        <div
                            className="mt-1 max-w-xs truncate text-[10px] text-muted-foreground"
                            title={a.review_notes}
                        >
                            "{a.review_notes}"
                        </div>
                    )}
                </div>
            ),
            csv: (a) => a.status,
        },
        {
            id: "created",
            header: "Submitted",
            cell: (a) => (
                <span className="text-xs text-muted-foreground">
                    {new Date(a.created_at).toLocaleDateString()}
                </span>
            ),
            csv: (a) => a.created_at,
        },
        {
            id: "reviewed",
            header: "Reviewed",
            defaultHidden: true,
            cell: (a) => (
                <span className="text-xs text-muted-foreground">
                    {a.reviewed_at
                        ? new Date(a.reviewed_at).toLocaleDateString()
                        : "—"}
                </span>
            ),
            csv: (a) => a.reviewed_at ?? "",
        },
        {
            id: "actions",
            header: "",
            align: "right",
            cell: (a) => {
                const canReview = a.status === "pending";
                return (
                    <div className="space-x-1.5 whitespace-nowrap">
                        <Button
                            size="sm"
                            disabled={!canReview}
                            onClick={() => setReviewing({ appeal: a, mode: "approve" })}
                            className="bg-emerald-600 hover:bg-emerald-700 text-white text-xs disabled:bg-zinc-200"
                        >
                            <CheckCircle2 className="size-3" /> Approve
                        </Button>
                        <Button
                            size="sm"
                            disabled={!canReview}
                            onClick={() => setReviewing({ appeal: a, mode: "reject" })}
                            className="bg-red-600 hover:bg-red-700 text-white text-xs disabled:bg-zinc-200"
                        >
                            <XCircle className="size-3" /> Reject
                        </Button>
                    </div>
                );
            },
        },
    ];

    return (
        <>
            <Explorer
                activeCount={activeCount}
                onReset={() => setStatus("pending")}
                filters={
                    <FilterGroup label="Status">
                        <SelectFilter
                            value={status}
                            onChange={(v) => setStatus(v as AppealStatusFilter)}
                            options={STATUS_OPTIONS}
                            placeholder="Pending"
                        />
                    </FilterGroup>
                }
            >
                <DataTable
                    columns={columns}
                    rows={rows}
                    getRowId={(a) => a.id}
                    loading={isLoading}
                    error={error}
                    onRetry={() => refetch()}
                    errorTitle="Failed to load appeals"
                    storageKey="admin.warmup-appeals"
                    csvName="warmbly-warmup-appeals"
                    noun="appeals"
                    emptyTitle="No appeals"
                    emptyHint={
                        status === "pending"
                            ? "No pending appeals to review."
                            : "No appeals match this status."
                    }
                    pager={{
                        canPrev: pager.canPrev,
                        canNext: !!data?.pagination?.has_more,
                        onPrev: pager.prev,
                        onNext: () => pager.next(data?.pagination?.next_cursor),
                        page: pager.page,
                        shown: rows.length,
                        total: data?.pagination?.total ?? null,
                    }}
                />
            </Explorer>

            {reviewing && (
                <ReviewAppealDialog
                    appeal={reviewing.appeal}
                    mode={reviewing.mode}
                    open
                    onOpenChange={(v) => !v && setReviewing(null)}
                    onDone={() => setReviewing(null)}
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
    const qc = useQueryClient();
    const [notes, setNotes] = useState("");
    const mailbox = appeal.email_account?.email ?? appeal.email_account_id;

    const mutation = useMutation({
        mutationFn: () =>
            mode === "approve"
                ? approveAppeal(appeal.id, { approved: true, notes })
                : rejectAppeal(appeal.id, { approved: false, notes }),
        onSuccess: () => {
            toast.success(
                `Appeal ${mode === "approve" ? "approved" : "rejected"}`,
            );
            // Approving unblocks the mailbox, so refresh both queues.
            qc.invalidateQueries({ queryKey: ["admin", "warmup"] });
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
                        {mode === "approve" ? (
                            <>
                                Approving unblocks{" "}
                                <span className="font-mono">{mailbox}</span> and
                                re-admits it to the warmup pool. Notes are recorded
                                for audit.
                            </>
                        ) : (
                            <>
                                Rejecting keeps{" "}
                                <span className="font-mono">{mailbox}</span>{" "}
                                blocked. Notes are recorded for audit and may be
                                shown to the user.
                            </>
                        )}
                    </DialogDescription>
                </DialogHeader>
                <div className="rounded-md border border-border bg-muted/40 p-2.5 text-xs text-muted-foreground">
                    <span className="font-medium text-foreground">
                        Appeal reason:
                    </span>{" "}
                    {appeal.reason}
                </div>
                <div>
                    <Label htmlFor="notes" className="text-xs font-medium">
                        Review notes (optional)
                    </Label>
                    <Textarea
                        id="notes"
                        placeholder={
                            mode === "approve"
                                ? "Optional: why the mailbox is being re-admitted"
                                : "Optional: why the appeal is being rejected"
                        }
                        value={notes}
                        onChange={(e) => setNotes(e.target.value)}
                        className="mt-1"
                        autoFocus
                    />
                </div>
                <DialogFooter>
                    <Button variant="outline" onClick={() => onOpenChange(false)}>
                        Cancel
                    </Button>
                    <Button
                        onClick={() => mutation.mutate()}
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
                              ? "Approve & unblock"
                              : "Reject"}
                    </Button>
                </DialogFooter>
            </DialogContent>
        </Dialog>
    );
}

function BlockedTab() {
    const pager = useCursorPager();
    const [unblocking, setUnblocking] = useState<AdminBlockedAccount | null>(
        null,
    );

    const { data, isLoading, error, refetch } = useQuery({
        queryKey: ["admin", "warmup", "blocked", pager.cursor],
        queryFn: () => listBlockedWarmupAccounts(pager.cursor),
        refetchInterval: 30_000,
        placeholderData: keepPreviousData,
        staleTime: 10_000,
    });

    const rows = data?.data ?? [];

    const columns: Column<AdminBlockedAccount>[] = [
        {
            id: "mailbox",
            header: "Mailbox",
            cell: (a) => <span className="font-mono text-xs">{a.email}</span>,
            csv: (a) => a.email,
        },
        {
            id: "owner",
            header: "Owner",
            cell: (a) => (
                <span className="text-xs">{a.user?.email ?? a.user_id}</span>
            ),
            csv: (a) => a.user?.email ?? a.user_id,
        },
        {
            id: "reason",
            header: "Reason",
            cell: (a) => (
                <span
                    className="block max-w-md truncate text-xs"
                    title={a.block_reason}
                >
                    {a.block_reason}
                </span>
            ),
            csv: (a) => a.block_reason,
        },
        {
            id: "blocked",
            header: "Blocked",
            cell: (a) => (
                <span className="text-xs text-muted-foreground">
                    {new Date(a.blocked_at).toLocaleDateString()}
                </span>
            ),
            csv: (a) => a.blocked_at,
        },
        {
            id: "appeal",
            header: "Appeal",
            cell: (a) =>
                a.has_appeal ? (
                    <Badge
                        variant="outline"
                        className={`text-[10px] ${
                            STATUS_TONE[a.appeal_status ?? "pending"]
                        }`}
                    >
                        {a.appeal_status ?? "pending"}
                    </Badge>
                ) : (
                    <span className="text-muted-foreground">—</span>
                ),
            csv: (a) => (a.has_appeal ? (a.appeal_status ?? "pending") : ""),
        },
        {
            id: "actions",
            header: "",
            align: "right",
            cell: (a) => (
                <Button
                    size="sm"
                    variant="outline"
                    onClick={() => setUnblocking(a)}
                    className="text-xs"
                >
                    <ShieldCheck className="size-3" /> Unblock
                </Button>
            ),
        },
    ];

    return (
        <>
            <DataTable
                columns={columns}
                rows={rows}
                getRowId={(a) => a.id}
                loading={isLoading}
                error={error}
                onRetry={() => refetch()}
                errorTitle="Failed to load blocked mailboxes"
                storageKey="admin.warmup-blocked"
                csvName="warmbly-warmup-blocked"
                noun="mailboxes"
                emptyTitle="No blocked mailboxes"
                emptyHint="No mailboxes are currently blocked from warmup."
                pager={{
                    canPrev: pager.canPrev,
                    canNext: !!data?.pagination?.has_more,
                    onPrev: pager.prev,
                    onNext: () => pager.next(data?.pagination?.next_cursor),
                    page: pager.page,
                    shown: rows.length,
                    total: data?.pagination?.total ?? null,
                }}
            />

            {unblocking && (
                <UnblockDialog
                    account={unblocking}
                    open
                    onOpenChange={(v) => !v && setUnblocking(null)}
                    onDone={() => setUnblocking(null)}
                />
            )}
        </>
    );
}

function UnblockDialog({
    account,
    open,
    onOpenChange,
    onDone,
}: {
    account: AdminBlockedAccount;
    open: boolean;
    onOpenChange: (v: boolean) => void;
    onDone: () => void;
}) {
    const qc = useQueryClient();
    const mutation = useMutation({
        mutationFn: () => unblockWarmupAccount(account.id),
        onSuccess: () => {
            toast.success("Mailbox unblocked");
            qc.invalidateQueries({ queryKey: ["admin", "warmup"] });
            onDone();
        },
        onError: (err: Error) => toast.error(err.message || "Failed to unblock"),
    });

    return (
        <Dialog open={open} onOpenChange={onOpenChange}>
            <DialogContent>
                <DialogHeader>
                    <DialogTitle>Unblock mailbox</DialogTitle>
                    <DialogDescription>
                        Unblock <span className="font-mono">{account.email}</span>{" "}
                        and re-admit it to the warmup pool? This bypasses any open
                        appeal and is recorded for audit.
                    </DialogDescription>
                </DialogHeader>
                {account.has_appeal && (
                    <div className="rounded-md border border-amber-300 bg-amber-50 p-2.5 text-xs text-amber-800">
                        This mailbox has an open appeal
                        {account.appeal_status
                            ? ` (${account.appeal_status})`
                            : ""}
                        . Unblocking here does not record an appeal decision —
                        prefer approving the appeal if you want it tracked as a
                        review outcome.
                    </div>
                )}
                <DialogFooter>
                    <Button variant="outline" onClick={() => onOpenChange(false)}>
                        Cancel
                    </Button>
                    <Button
                        onClick={() => mutation.mutate()}
                        disabled={mutation.isPending}
                        className="bg-emerald-600 hover:bg-emerald-700 text-white"
                    >
                        {mutation.isPending ? "Working…" : "Unblock"}
                    </Button>
                </DialogFooter>
            </DialogContent>
        </Dialog>
    );
}
