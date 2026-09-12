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
import { toast } from "sonner";
import { BadgeCheck, CalendarClock, Gift, X } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Badge } from "@/components/ui/badge";
import { Skeleton } from "@/components/ui/skeleton";
import { useAdminPerm } from "@/hooks/useAdminPerm";
import { AdminPerm } from "@/lib/auth/permissions";
import {
    getOrganizationManagedPlan,
    grantOrganizationManagedPlan,
    listAdminPlans,
    revokeOrganizationManagedPlan,
} from "@/lib/api/client/admin/organizations";
import type { ManagedPlan } from "@/lib/api/models/admin";

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
            <Badge variant="outline" className="text-[10px] border-sky-300 bg-sky-50 text-sky-700">
                managed
            </Badge>
        );
    }
    if (expired) {
        return (
            <Badge variant="outline" className="text-[10px] border-amber-300 bg-amber-50 text-amber-700">
                grant lapsed
            </Badge>
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

    if (managedQuery.isLoading) return <Skeleton className="h-28 w-full" />;
    if (managedQuery.error || !managedQuery.data) {
        return (
            <div className="text-sm text-red-600 border border-red-200 bg-red-50 rounded-md p-3">
                Failed to load the plan grant.
            </div>
        );
    }

    const m = managedQuery.data;
    const plans = plansQuery.data?.plans ?? [];

    return (
        <div className="border border-border rounded-lg bg-card">
            <div className="flex items-start justify-between gap-3 p-3 border-b border-border">
                <div>
                    <div className="flex items-center gap-2">
                        <Gift className="size-4 text-muted-foreground" />
                        <span className="text-sm font-medium">Managed plan</span>
                        <ManagedPlanBadge managed={m.managed} expired={m.expired} />
                    </div>
                    <div className="text-xs text-muted-foreground mt-1">
                        {m.managed
                            ? "Paid because we granted it, not because Stripe says so."
                            : m.expired
                              ? "The grant has lapsed; the workspace is back on what it pays for."
                              : "No grant. This workspace is entitled by Stripe alone."}
                    </div>
                </div>
                {canManage && (
                    <div className="flex items-center gap-2">
                        {(m.managed || m.expired) && (
                            <Button
                                size="sm"
                                variant="outline"
                                onClick={() => revokeMutation.mutate()}
                                disabled={revokeMutation.isPending}
                            >
                                <X className="size-3.5 mr-1" /> Revoke
                            </Button>
                        )}
                        <Button size="sm" variant="outline" onClick={() => setGranting((v) => !v)}>
                            <BadgeCheck className="size-3.5 mr-1" />
                            {m.managed ? "Change" : "Grant"}
                        </Button>
                    </div>
                )}
            </div>

            {(m.managed || m.expired) && (
                <dl className="p-3 grid grid-cols-2 gap-x-4 gap-y-2 text-xs border-b border-border">
                    <div>
                        <dt className="text-muted-foreground">Reason</dt>
                        <dd className="mt-0.5">{m.reason || "—"}</dd>
                    </div>
                    <div>
                        <dt className="text-muted-foreground">Granted</dt>
                        <dd className="mt-0.5">{fmt(m.granted_at) ?? "—"}</dd>
                    </div>
                    <div>
                        <dt className="text-muted-foreground">Expires</dt>
                        <dd className="mt-0.5 inline-flex items-center gap-1">
                            {m.until ? (
                                <>
                                    <CalendarClock className="size-3" /> {fmt(m.until)}
                                </>
                            ) : (
                                "open-ended"
                            )}
                        </dd>
                    </div>
                </dl>
            )}

            {granting && canManage && (
                <div className="p-3 space-y-2">
                    <label className="block text-xs text-muted-foreground">
                        Plan
                        <select
                            className="mt-1 w-full h-8 rounded-md border border-border bg-background px-2 text-xs"
                            value={planId}
                            onChange={(e) => setPlanId(e.target.value)}
                        >
                            <option value="">Choose a plan…</option>
                            {plans.map((p) => (
                                <option key={p.id} value={p.id}>
                                    {p.name}
                                    {p.public === false ? " (private)" : ""}
                                </option>
                            ))}
                        </select>
                    </label>
                    <label className="block text-xs text-muted-foreground">
                        Reason
                        <input
                            className="mt-1 w-full h-8 rounded-md border border-border bg-background px-2 text-xs"
                            placeholder="Why this workspace is paid without paying"
                            value={reason}
                            onChange={(e) => setReason(e.target.value)}
                        />
                    </label>
                    <label className="block text-xs text-muted-foreground">
                        Expires (optional)
                        <input
                            type="date"
                            className="mt-1 w-full h-8 rounded-md border border-border bg-background px-2 text-xs"
                            value={until}
                            onChange={(e) => setUntil(e.target.value)}
                        />
                        <span className="block mt-1 text-[11px]">
                            Leave empty for an open-ended grant. A date lapses on its own, so nobody has to
                            remember to revoke it.
                        </span>
                    </label>
                    <div className="flex items-center gap-2 pt-1">
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
