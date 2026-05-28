// Limit-increase request queue. Pending requests are the default
// view — approve writes the corresponding override on the org via the
// same SetLimitOverrides path the manual editor uses; reject stamps
// the row with required review notes.

import { useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Link } from "react-router-dom";
import { toast } from "sonner";
import { CheckCircle2, XCircle } from "lucide-react";
import { PageHeader } from "@/components/layout/PageHeader";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Skeleton } from "@/components/ui/skeleton";
import {
    Dialog,
    DialogContent,
    DialogDescription,
    DialogFooter,
    DialogHeader,
    DialogTitle,
} from "@/components/ui/dialog";
import {
    approveLimitRequest,
    listLimitRequests,
    rejectLimitRequest,
} from "@/lib/api/client/admin/limitRequests";
import type {
    LimitIncreaseRequest,
    LimitRequestStatus,
} from "@/lib/api/models/admin";

type StatusFilter = LimitRequestStatus | "all";

const STATUS_TONE: Record<LimitRequestStatus, string> = {
    pending: "border-amber-300 text-amber-700 bg-amber-50",
    approved: "border-emerald-300 text-emerald-700 bg-emerald-50",
    rejected: "border-red-300 text-red-700 bg-red-50",
    cancelled: "border-zinc-300 text-zinc-600 bg-zinc-50",
};

const FIELD_LABEL: Record<string, string> = {
    max_email_accounts: "Mailboxes",
    max_campaigns: "Campaigns (lifetime)",
    max_active_campaigns: "Active campaigns",
    max_team_members: "Team members",
    max_contacts: "Contacts",
    daily_campaign_limit: "Daily sends",
};

export default function LimitRequestsPage() {
    const [status, setStatus] = useState<StatusFilter>("pending");
    const [reviewing, setReviewing] = useState<{
        req: LimitIncreaseRequest;
        mode: "approve" | "reject";
    } | null>(null);

    const { data, isLoading, error } = useQuery({
        queryKey: ["admin", "limit-requests", status],
        queryFn: () => listLimitRequests(status),
        refetchInterval: 30_000,
    });

    const rows = data?.data ?? [];

    return (
        <div>
            <PageHeader
                title="Limit-increase requests"
                description="Customer-submitted requests for more capacity than their plan or product hard cap allows. Approving rewrites the per-org override; rejecting stamps the row with notes."
            >
                <StatusToggle value={status} onChange={setStatus} />
            </PageHeader>

            {isLoading && <Skeleton className="h-32 w-full" />}
            {error && (
                <div className="text-sm text-red-600 border border-red-200 bg-red-50 rounded-md p-3">
                    Failed to load limit requests.
                </div>
            )}

            {!isLoading && !error && rows.length === 0 && (
                <div className="text-sm text-muted-foreground border border-border rounded-md p-4 bg-card">
                    No {status === "all" ? "" : status} requests.
                </div>
            )}

            {rows.length > 0 && (
                <div className="border border-border rounded-lg overflow-hidden bg-card">
                    <table className="w-full text-sm">
                        <thead className="bg-muted/50 text-muted-foreground text-xs uppercase">
                            <tr>
                                <th className="text-left px-3 py-2 font-medium">Workspace</th>
                                <th className="text-left px-3 py-2 font-medium">Requester</th>
                                <th className="text-left px-3 py-2 font-medium">Field</th>
                                <th className="text-right px-3 py-2 font-medium">Current</th>
                                <th className="text-right px-3 py-2 font-medium">Requested</th>
                                <th className="text-left px-3 py-2 font-medium">Reason</th>
                                <th className="text-left px-3 py-2 font-medium">Status</th>
                                <th className="text-left px-3 py-2 font-medium">Submitted</th>
                                <th className="text-right px-3 py-2 font-medium">Action</th>
                            </tr>
                        </thead>
                        <tbody>
                            {rows.map((r) => (
                                <RequestRow
                                    key={r.id}
                                    req={r}
                                    onApprove={() => setReviewing({ req: r, mode: "approve" })}
                                    onReject={() => setReviewing({ req: r, mode: "reject" })}
                                />
                            ))}
                        </tbody>
                    </table>
                </div>
            )}

            {reviewing && (
                <ReviewDialog
                    req={reviewing.req}
                    mode={reviewing.mode}
                    open
                    onOpenChange={(v) => !v && setReviewing(null)}
                />
            )}
        </div>
    );
}

function RequestRow({
    req,
    onApprove,
    onReject,
}: {
    req: LimitIncreaseRequest;
    onApprove: () => void;
    onReject: () => void;
}) {
    const fieldLabel = FIELD_LABEL[req.field] ?? req.field;
    const canReview = req.status === "pending";
    const delta = req.requested - req.current_effective;
    return (
        <tr className="border-t border-border align-top">
            <td className="px-3 py-2">
                {req.organization && (
                    <Link
                        to={`/organizations/${req.organization_id}`}
                        className="text-[var(--admin-accent-strong)] hover:underline font-medium"
                    >
                        {req.organization.name}
                    </Link>
                )}
            </td>
            <td className="px-3 py-2 text-xs">
                {req.submitted_by_user?.email ?? req.submitted_by}
            </td>
            <td className="px-3 py-2 text-xs">{fieldLabel}</td>
            <td className="px-3 py-2 text-right tabular-nums text-muted-foreground">
                {req.current_effective.toLocaleString()}
            </td>
            <td className="px-3 py-2 text-right tabular-nums font-medium">
                {req.requested.toLocaleString()}
                <span className="text-[10px] text-emerald-600 ml-1">
                    (+{delta.toLocaleString()})
                </span>
            </td>
            <td className="px-3 py-2 text-xs max-w-md truncate" title={req.reason}>
                {req.reason}
            </td>
            <td className="px-3 py-2">
                <Badge variant="outline" className={`text-[10px] ${STATUS_TONE[req.status]}`}>
                    {req.status}
                </Badge>
                {req.review_notes && req.status !== "pending" && (
                    <div className="text-[10px] text-muted-foreground mt-1 max-w-xs truncate"
                        title={req.review_notes}>
                        "{req.review_notes}"
                    </div>
                )}
            </td>
            <td className="px-3 py-2 text-xs text-muted-foreground">
                {new Date(req.submitted_at).toLocaleDateString()}
            </td>
            <td className="px-3 py-2 text-right space-x-1.5 whitespace-nowrap">
                <Button
                    size="sm"
                    onClick={onApprove}
                    disabled={!canReview}
                    className="bg-emerald-600 hover:bg-emerald-700 text-white text-xs disabled:bg-zinc-200"
                >
                    <CheckCircle2 className="size-3" /> Approve
                </Button>
                <Button
                    size="sm"
                    onClick={onReject}
                    disabled={!canReview}
                    className="bg-red-600 hover:bg-red-700 text-white text-xs disabled:bg-zinc-200"
                >
                    <XCircle className="size-3" /> Reject
                </Button>
            </td>
        </tr>
    );
}

function StatusToggle({
    value,
    onChange,
}: {
    value: StatusFilter;
    onChange: (v: StatusFilter) => void;
}) {
    const options: { value: StatusFilter; label: string }[] = [
        { value: "pending", label: "Pending" },
        { value: "approved", label: "Approved" },
        { value: "rejected", label: "Rejected" },
        { value: "cancelled", label: "Cancelled" },
        { value: "all", label: "All" },
    ];
    return (
        <div className="inline-flex rounded-md border border-border bg-card p-0.5 text-xs">
            {options.map((opt) => (
                <button
                    key={opt.value}
                    type="button"
                    onClick={() => onChange(opt.value)}
                    className={`px-2 py-1 rounded ${
                        value === opt.value
                            ? "bg-[var(--admin-accent)] text-white"
                            : "text-muted-foreground hover:text-foreground"
                    }`}
                >
                    {opt.label}
                </button>
            ))}
        </div>
    );
}

function ReviewDialog({
    req,
    mode,
    open,
    onOpenChange,
}: {
    req: LimitIncreaseRequest;
    mode: "approve" | "reject";
    open: boolean;
    onOpenChange: (v: boolean) => void;
}) {
    const qc = useQueryClient();
    const [notes, setNotes] = useState("");
    const mutation = useMutation({
        mutationFn: () =>
            mode === "approve"
                ? approveLimitRequest(req.id, notes)
                : rejectLimitRequest(req.id, notes),
        onSuccess: () => {
            toast.success(`Request ${mode === "approve" ? "approved" : "rejected"}`);
            qc.invalidateQueries({ queryKey: ["admin", "limit-requests"] });
            qc.invalidateQueries({ queryKey: ["admin", "organizations", req.organization_id] });
            onOpenChange(false);
        },
        onError: (err: Error) => toast.error(err.message || "Action failed"),
    });

    const fieldLabel = FIELD_LABEL[req.field] ?? req.field;

    return (
        <Dialog open={open} onOpenChange={onOpenChange}>
            <DialogContent>
                <DialogHeader>
                    <DialogTitle>
                        {mode === "approve" ? "Approve request" : "Reject request"}
                    </DialogTitle>
                    <DialogDescription>
                        {mode === "approve" ? (
                            <>
                                Approving raises <strong>{fieldLabel}</strong> for{" "}
                                <span className="font-mono">
                                    {req.organization?.name ?? req.organization_id}
                                </span>{" "}
                                from {req.current_effective.toLocaleString()} to{" "}
                                {req.requested.toLocaleString()}. This writes the
                                corresponding override on the org and is auditable.
                            </>
                        ) : (
                            <>
                                Rejecting the request stamps it with your notes for the
                                customer to see. The org keeps its current effective
                                limit ({req.current_effective.toLocaleString()}).
                            </>
                        )}
                    </DialogDescription>
                </DialogHeader>
                <div>
                    <Label htmlFor="notes" className="text-xs font-medium">
                        Notes {mode === "reject" ? "(required)" : "(optional)"}
                    </Label>
                    <Input
                        id="notes"
                        placeholder={
                            mode === "approve"
                                ? "Optional: business reason for the bump"
                                : "Required: tell the customer why"
                        }
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
                            if (mode === "reject" && notes.trim() === "") {
                                toast.error("Notes are required when rejecting");
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
