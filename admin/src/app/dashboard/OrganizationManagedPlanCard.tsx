// A plan an operator granted rather than Stripe: internal workspaces, design
// partners, support gestures. Before this the only way to make a workspace
// paid was a Stripe subscription id, so the alternative was writing a fake one
// into the database.
//
// Reading needs view_organizations; granting and revoking need
// manage_organizations, so an admin without it sees the record rather than
// buttons that can only answer 403.

import { useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Link } from "react-router-dom";
import { toast } from "sonner";
import { BadgeCheck, CalendarClock, Gift, X } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Skeleton } from "@/components/ui/skeleton";
import {
    Select,
    SelectContent,
    SelectItem,
    SelectTrigger,
    SelectValue,
} from "@/components/ui/select";
import { StatusBadge } from "@/components/ui/kit";
import { ErrorState } from "@/components/ErrorState";
import { useAdminPerm } from "@/hooks/useAdminPerm";
import { AdminPerm } from "@/lib/auth/permissions";
import {
    getOrganizationManagedPlan,
    grantOrganizationManagedPlan,
    listAdminPlans,
    revokeOrganizationManagedPlan,
} from "@/lib/api/client/admin/organizations";
import type { ManagedPlan } from "@/lib/api/models/admin";
import { isAgencyPlan } from "./agencies/agencyFormat";

/** A date input gives "YYYY-MM-DD" with no time. Reading that with `new Date`
 *  yields UTC midnight, so a grant made for today is already expired for
 *  anyone west of UTC, and the date displayed back can be the previous day.
 *  The grant runs to the end of the chosen day in the operator's own zone. */
function endOfLocalDay(day: string): string {
    const [y, m, d] = day.split("-").map(Number);
    return new Date(y, m - 1, d, 23, 59, 59, 999).toISOString();
}

function fmt(ts?: string | null) {
    if (!ts) return null;
    return new Date(ts).toLocaleDateString(undefined, { year: "numeric", month: "short", day: "numeric" });
}

export function ManagedPlanBadge({ managed, expired }: { managed: boolean; expired: boolean }) {
    if (managed) {
        return (
            <StatusBadge tone="info" dot>
                managed
            </StatusBadge>
        );
    }
    if (expired) {
        return (
            <StatusBadge tone="warning" dot>
                grant lapsed
            </StatusBadge>
        );
    }
    return null;
}

export function OrganizationManagedPlanCard({ orgId }: { orgId: string }) {
    const qc = useQueryClient();
    const canManage = useAdminPerm(AdminPerm.ManageOrganizations);
    const [granting, setGranting] = useState(false);
    const [planId, setPlanId] = useState("");
    const [reason, setReason] = useState("");
    const [until, setUntil] = useState("");

    const managedQuery = useQuery({
        queryKey: ["admin", "organizations", orgId, "managed-plan"],
        queryFn: () => getOrganizationManagedPlan(orgId),
        enabled: !!orgId,
    });

    // Only loaded once the form is open: most visits to this card are to read
    // the record, not to grant.
    const plansQuery = useQuery({
        queryKey: ["admin", "plans"],
        queryFn: () => listAdminPlans(),
        enabled: granting,
    });

    function applied(message: string) {
        return (managed: ManagedPlan) => {
            qc.setQueryData(["admin", "organizations", orgId, "managed-plan"], managed);
            // The org detail shows the plan name, and the list inlines the
            // badge, so both go stale on a grant.
            qc.invalidateQueries({ queryKey: ["admin", "organizations", orgId] });
            qc.invalidateQueries({ queryKey: ["admin", "organizations"] });
            toast.success(message);
        };
    }

    const grantMutation = useMutation({
        mutationFn: () =>
            grantOrganizationManagedPlan(orgId, {
                plan_id: planId,
                reason: reason.trim(),
                until: until ? endOfLocalDay(until) : null,
            }),
        onSuccess: (m) => {
            applied("Plan granted")(m);
            setGranting(false);
            setReason("");
            setUntil("");
        },
        onError: (e: Error) => toast.error(e.message || "Failed to grant the plan"),
    });

    const revokeMutation = useMutation({
        mutationFn: () => revokeOrganizationManagedPlan(orgId),
        onSuccess: applied("Grant revoked; the workspace is back on what it pays for"),
        onError: (e: Error) => toast.error(e.message || "Failed to revoke the grant"),
    });

    if (managedQuery.isLoading) return <Skeleton className="h-28 w-full rounded-lg" />;
    if (managedQuery.error || !managedQuery.data) {
        return (
            <ErrorState
                error={managedQuery.error}
                title="Failed to load the plan grant."
                onRetry={() => managedQuery.refetch()}
            />
        );
    }

    const m = managedQuery.data;
    // An agency plan is held by an agency, never by one workspace; the backend refuses it here.
    const plans = (plansQuery.data?.plans ?? []).filter((p) => !isAgencyPlan(p));

    return (
        <div className="overflow-hidden surface-lit rounded-xl border border-border bg-card">
            <div className="flex flex-wrap items-start justify-between gap-3 px-4 py-3">
                <div className="min-w-0">
                    <div className="flex items-center gap-2">
                        <Gift className="size-4 text-subtle-foreground" />
                        <span className="text-[13px] font-medium text-foreground">Managed plan</span>
                        <ManagedPlanBadge managed={m.managed} expired={m.expired} />
                    </div>
                    <p className="mt-1 text-[12.5px] text-muted-foreground">
                        {m.managed
                            ? "Paid because we granted it, not because Stripe says so."
                            : m.expired
                              ? "The grant has lapsed; the workspace is back on what it pays for."
                              : "No grant. This workspace is entitled by Stripe alone."}
                    </p>
                </div>
                {canManage && (
                    <div className="flex shrink-0 items-center gap-1.5">
                        {(m.managed || m.expired) && (
                            <Button
                                size="sm"
                                variant="outline"
                                onClick={() => revokeMutation.mutate()}
                                disabled={revokeMutation.isPending}
                            >
                                <X className="size-3.5" /> Revoke
                            </Button>
                        )}
                        <Button size="sm" variant="outline" onClick={() => setGranting((v) => !v)}>
                            <BadgeCheck className="size-3.5" />
                            {m.managed ? "Change" : "Grant"}
                        </Button>
                    </div>
                )}
            </div>

            {(m.managed || m.expired) && (
                <dl className="grid grid-cols-1 gap-px border-t border-border bg-border sm:grid-cols-3">
                    <div className="bg-card px-4 py-2.5">
                        <dt className="text-xs text-muted-foreground">Reason</dt>
                        <dd className="mt-0.5 break-words text-[13px] text-foreground">{m.reason || "—"}</dd>
                    </div>
                    <div className="bg-card px-4 py-2.5">
                        <dt className="text-xs text-muted-foreground">Granted</dt>
                        <dd className="mt-0.5 text-[13px] tabular-nums text-foreground">{fmt(m.granted_at) ?? "—"}</dd>
                    </div>
                    <div className="bg-card px-4 py-2.5">
                        <dt className="text-xs text-muted-foreground">Expires</dt>
                        <dd className="mt-0.5 inline-flex items-center gap-1.5 text-[13px] tabular-nums text-foreground">
                            {m.until ? (
                                <>
                                    <CalendarClock className="size-3.5 text-subtle-foreground" /> {fmt(m.until)}
                                </>
                            ) : (
                                "open-ended"
                            )}
                        </dd>
                    </div>
                </dl>
            )}

            {granting && canManage && (
                <div className="space-y-3 border-t border-border bg-muted/30 px-4 py-4">
                    <div className="grid gap-3 sm:grid-cols-2">
                        <div className="space-y-1.5">
                            <Label htmlFor="managed-plan-plan" className="text-xs font-medium text-muted-foreground">
                                Plan
                            </Label>
                            <Select value={planId} onValueChange={setPlanId}>
                                <SelectTrigger id="managed-plan-plan">
                                    <SelectValue placeholder={plansQuery.isLoading ? "Loading plans…" : "Choose a plan…"} />
                                </SelectTrigger>
                                <SelectContent>
                                    {plans.map((p) => (
                                        <SelectItem key={p.id} value={p.id}>
                                            {p.name}
                                            {p.public === false ? " (private)" : ""}
                                        </SelectItem>
                                    ))}
                                </SelectContent>
                            </Select>
                            <p className="text-xs text-muted-foreground">
                                Agency plans are given from{" "}
                                <Link to="/agencies" className="underline underline-offset-2">
                                    Agencies
                                </Link>
                                .
                            </p>
                        </div>
                        <div className="space-y-1.5">
                            <Label htmlFor="managed-plan-until" className="text-xs font-medium text-muted-foreground">
                                Expires (optional)
                            </Label>
                            <Input
                                id="managed-plan-until"
                                type="date"
                                value={until}
                                onChange={(e) => setUntil(e.target.value)}
                            />
                        </div>
                    </div>
                    <div className="space-y-1.5">
                        <Label htmlFor="managed-plan-reason" className="text-xs font-medium text-muted-foreground">
                            Reason
                        </Label>
                        <Input
                            id="managed-plan-reason"
                            placeholder="Why this workspace is paid without paying"
                            value={reason}
                            onChange={(e) => setReason(e.target.value)}
                        />
                    </div>
                    <p className="text-xs leading-relaxed text-muted-foreground">
                        Leave the expiry empty for an open-ended grant. A date lapses on its own, so nobody has to
                        remember to revoke it.
                    </p>
                    <div className="flex items-center gap-1.5">
                        <Button
                            size="sm"
                            onClick={() => grantMutation.mutate()}
                            disabled={!planId || !reason.trim() || grantMutation.isPending}
                        >
                            Grant
                        </Button>
                        <Button size="sm" variant="ghost" onClick={() => setGranting(false)}>
                            Cancel
                        </Button>
                    </div>
                </div>
            )}
        </div>
    );
}
