// The agency's plan and where it comes from: a Stripe subscription, or a plan
// an operator gave it without Stripe. Its client workspaces receive whichever
// is in force.
//
// Reading needs view_organizations; giving and ending need manage_organizations.

import { useState, type FormEvent } from "react";
import { useQuery } from "@tanstack/react-query";
import { Link } from "react-router-dom";
import { BadgeCheck, CalendarClock, Gift, X } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { StatusBadge } from "@/components/ui/kit";
import { useConfirm } from "@/components/ConfirmDialog";
import { useAdminPerm } from "@/hooks/useAdminPerm";
import { AdminPerm } from "@/lib/auth/permissions";
import { listAdminPlans } from "@/lib/api/client/admin/organizations";
import { grantAgencyPlan, revokeAgencyPlan, type AdminAgency, type AdminAgencyManagedPlan } from "@/lib/api/client/admin/agencies";
import type { AdminPlan } from "@/lib/api/models/admin";
import { endOfLocalDay, fmtDay } from "../grantDates";
import { AGENCY_STATUS_TONE, hasLiveSubscription, isAgencyPlan } from "./agencyFormat";
import { useAgencyWrite } from "./useAgencyWrite";

const CELL = "bg-card px-4 py-2.5";
const DT = "text-xs text-muted-foreground";
const DD = "mt-0.5 break-words text-[13px] text-foreground";

function summary(a: AdminAgency, live: boolean): string {
    if (a.granted) return "Given by an operator, not bought through Stripe. Its client workspaces receive this plan.";
    if (live) return "Bought through Stripe. Its client workspaces receive this plan.";
    if (a.managed_plan) return "The plan an operator gave has lapsed, and no subscription is in force.";
    // Entitled with neither source only happens where the instance does not bill.
    if (a.entitled) return "No plan is recorded. Billing is off on this instance, so the agency works without one.";
    return "No plan in force. Its client workspaces receive nothing from the agency.";
}

export function AgencyPlanCard({ agency: a }: { agency: AdminAgency }) {
    const confirm = useConfirm();
    const canManage = useAdminPerm(AdminPerm.ManageOrganizations);
    const [giving, setGiving] = useState(false);
    const managed = a.managed_plan;
    const live = hasLiveSubscription(a);

    const end = useAgencyWrite(a.id, () => revokeAgencyPlan(a.id), {
        done: "Plan ended",
        failed: "Failed to end the plan",
    });

    async function endPlan() {
        const ok = await confirm({
            title: `End the plan given to ${a.name}?`,
            description: live
                ? "The agency falls back to its own Stripe subscription, and its client workspaces follow it."
                : "The agency has no subscription of its own, so its client workspaces lose the plan.",
            confirmLabel: "End plan",
            destructive: true,
        });
        if (ok) await end.mutateAsync().catch(() => undefined);
    }

    return (
        <div className="overflow-hidden surface-lit rounded-xl border border-border bg-card">
            <div className="flex flex-wrap items-start justify-between gap-3 px-4 py-3">
                <div className="min-w-0">
                    <div className="flex flex-wrap items-center gap-2">
                        <Gift className="size-4 text-subtle-foreground" />
                        <span className="text-[13px] font-medium text-foreground">Plan</span>
                        <span className="text-[13px] text-foreground">{a.plan?.name ?? "None"}</span>
                        {a.granted ? (
                            <StatusBadge tone="info" dot>
                                given
                            </StatusBadge>
                        ) : (
                            managed && (
                                <StatusBadge tone="warning" dot>
                                    lapsed
                                </StatusBadge>
                            )
                        )}
                    </div>
                    <p className="mt-1 text-[12.5px] text-muted-foreground">{summary(a, live)}</p>
                </div>
                {canManage && (
                    <div className="flex shrink-0 items-center gap-1.5">
                        {managed && (
                            <Button size="sm" variant="outline" onClick={endPlan} disabled={end.isPending}>
                                <X className="size-3.5" /> {end.isPending ? "Ending…" : "End plan"}
                            </Button>
                        )}
                        <Button size="sm" variant="outline" onClick={() => setGiving(true)}>
                            <BadgeCheck className="size-3.5" />
                            {a.granted ? "Change plan" : "Give a plan"}
                        </Button>
                    </div>
                )}
            </div>

            {managed && (
                <dl className="grid grid-cols-1 gap-px border-t border-border bg-border sm:grid-cols-4">
                    <div className={CELL}>
                        <dt className={DT}>Given by an operator</dt>
                        <dd className={DD}>{managed.plan_name || managed.plan_id}</dd>
                    </div>
                    <div className={CELL}>
                        <dt className={DT}>Reason</dt>
                        <dd className={DD}>{managed.reason || "None"}</dd>
                    </div>
                    <div className={CELL}>
                        <dt className={DT}>Given</dt>
                        <dd className={`${DD} tabular-nums`}>
                            {fmtDay(managed.granted_at)}
                            {managed.granted_by && (
                                <>
                                    {" by "}
                                    <Link to={`/users/${managed.granted_by}`} className="underline underline-offset-2">
                                        this operator
                                    </Link>
                                </>
                            )}
                        </dd>
                    </div>
                    <div className={CELL}>
                        <dt className={DT}>{managed.active ? "Ends" : "Lapsed"}</dt>
                        <dd className={`${DD} inline-flex items-center gap-1.5 tabular-nums`}>
                            {managed.until ? (
                                <>
                                    <CalendarClock className="size-3.5 text-subtle-foreground" /> {fmtDay(managed.until)}
                                </>
                            ) : (
                                "No end date"
                            )}
                        </dd>
                    </div>
                </dl>
            )}

            {a.plan_id && (
                <dl className="grid grid-cols-1 gap-px border-t border-border bg-border sm:grid-cols-4">
                    <div className={CELL}>
                        <dt className={DT}>Stripe subscription</dt>
                        <dd className={DD}>
                            <StatusBadge tone={AGENCY_STATUS_TONE[a.status] ?? "neutral"}>{a.status.replace(/_/g, " ")}</StatusBadge>
                        </dd>
                    </div>
                    <div className={`${CELL} sm:col-span-3`}>
                        <dt className={DT}>{a.cancel_at_period_end ? "Ends" : "Renews"}</dt>
                        <dd className={`${DD} tabular-nums`}>{a.current_period_end ? fmtDay(a.current_period_end) : "No date"}</dd>
                    </div>
                </dl>
            )}

            {a.granted && live && (
                <p className="border-t border-border px-4 py-2.5 text-[12.5px] text-muted-foreground">
                    This agency also has a Stripe subscription in force. The operator's plan is the one that applies.
                </p>
            )}

            <AgencyPlanDialog agency={a} open={giving && canManage} onOpenChange={setGiving} />
        </div>
    );
}

function planLabel(p: AdminPlan): string {
    const pool = p.monthly_send_pool ? `${p.monthly_send_pool.toLocaleString()} sends a month` : "no pool cap";
    return `${p.name ?? p.id} (${pool}${p.public === false ? ", private" : ""})`;
}

export function AgencyPlanDialog({
    agency,
    open,
    onOpenChange,
}: {
    agency: AdminAgency;
    open: boolean;
    onOpenChange: (v: boolean) => void;
}) {
    return (
        <Dialog open={open} onOpenChange={onOpenChange}>
            <DialogContent>
                <DialogHeader>
                    <DialogTitle>Give {agency.name} a plan</DialogTitle>
                    <DialogDescription>
                        Puts the agency on an agency plan without Stripe. Its client workspaces receive the plan right away.
                    </DialogDescription>
                </DialogHeader>
                {/* Mounted per open, so the form starts from the plan as it stands. */}
                <PlanForm agencyId={agency.id} current={agency.managed_plan} onClose={() => onOpenChange(false)} />
            </DialogContent>
        </Dialog>
    );
}

function PlanForm({ agencyId, current, onClose }: { agencyId: string; current?: AdminAgencyManagedPlan; onClose: () => void }) {
    const [planId, setPlanId] = useState(current?.active ? current.plan_id : "");
    const [reason, setReason] = useState("");
    const [until, setUntil] = useState("");

    const plansQuery = useQuery({ queryKey: ["admin", "plans"], queryFn: () => listAdminPlans() });
    const plans = (plansQuery.data?.plans ?? []).filter(isAgencyPlan);

    const give = useAgencyWrite(
        agencyId,
        () => grantAgencyPlan(agencyId, { plan_id: planId, reason: reason.trim(), until: until ? endOfLocalDay(until) : undefined }),
        { done: "Plan given", failed: "Failed to give the plan" },
        onClose,
    );

    function submit(e: FormEvent) {
        e.preventDefault();
        if (planId && reason.trim() && !give.isPending) give.mutate();
    }

    return (
        <form onSubmit={submit} className="grid gap-4">
            <div className="space-y-1.5">
                <Label htmlFor="agency-plan-plan" className="text-xs font-medium text-muted-foreground">
                    Agency plan
                </Label>
                <Select value={planId} onValueChange={setPlanId}>
                    <SelectTrigger id="agency-plan-plan" className="w-full">
                        <SelectValue
                            placeholder={
                                plansQuery.isLoading ? "Loading plans…" : plans.length === 0 ? "No agency plans" : "Choose a plan…"
                            }
                        />
                    </SelectTrigger>
                    <SelectContent>
                        {plans.map((p) => (
                            <SelectItem key={p.id} value={p.id}>
                                {planLabel(p)}
                            </SelectItem>
                        ))}
                    </SelectContent>
                </Select>
                {plansQuery.error && (
                    <p role="alert" className="text-xs text-[var(--admin-danger)]">
                        {plansQuery.error.message || "Failed to load plans."}
                    </p>
                )}
            </div>
            <div className="space-y-1.5">
                <Label htmlFor="agency-plan-reason" className="text-xs font-medium text-muted-foreground">
                    Reason
                </Label>
                <Input
                    id="agency-plan-reason"
                    placeholder="Why this agency has the plan without paying"
                    value={reason}
                    maxLength={500}
                    onChange={(e) => setReason(e.target.value)}
                    autoFocus
                />
            </div>
            <div className="space-y-1.5">
                <Label htmlFor="agency-plan-until" className="text-xs font-medium text-muted-foreground">
                    Ends (optional)
                </Label>
                <Input id="agency-plan-until" type="date" value={until} onChange={(e) => setUntil(e.target.value)} />
                <p className="text-xs leading-relaxed text-muted-foreground">
                    Leave the date empty for a plan with no end. A date lapses on its own, so nobody has to remember to end it.
                </p>
            </div>
            <DialogFooter>
                <Button type="button" variant="outline" onClick={onClose}>
                    Cancel
                </Button>
                <Button type="submit" disabled={!planId || !reason.trim() || give.isPending}>
                    {give.isPending ? "Giving…" : "Give plan"}
                </Button>
            </DialogFooter>
        </form>
    );
}
