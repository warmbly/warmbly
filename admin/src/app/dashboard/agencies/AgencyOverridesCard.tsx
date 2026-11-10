// A send pool and AI allowance set for one agency in place of its plan's own,
// for Agency Scale and other negotiated deals.
//
// The endpoint replaces both values, so the editor always starts from what is
// stored and sends null for a field left empty.

import { useState, type FormEvent } from "react";
import { SlidersHorizontal } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog";
import { StatusBadge } from "@/components/ui/kit";
import { useAdminPerm } from "@/hooks/useAdminPerm";
import { AdminPerm } from "@/lib/auth/permissions";
import { setAgencyOverrides, type AdminAgency } from "@/lib/api/client/admin/agencies";
import { fmtDay } from "../grantDates";
import { parseAmount, usd } from "./agencyFormat";
import { useAgencyWrite } from "./useAgencyWrite";

const POOL_MAX = 100_000_000;
const AI_MAX_USD = 100_000;

const CELL = "bg-card px-4 py-2.5";
const DT = "text-xs text-muted-foreground";
const DD = "mt-0.5 break-words text-[13px] text-foreground";
const FIELD_LABEL = "text-xs font-medium text-muted-foreground";

function planPool(a: AdminAgency): string {
    const pool = a.plan?.monthly_send_pool;
    if (pool == null) return "Plan default";
    return pool === 0 ? "Plan default (no cap)" : `Plan default (${pool.toLocaleString()})`;
}

function planAllowance(a: AdminAgency): string {
    const allowance = a.plan?.agency_ai_allowance_usd;
    return allowance == null ? "Plan default" : `Plan default (${usd(allowance)})`;
}

export function AgencyOverridesCard({ agency: a }: { agency: AdminAgency }) {
    const canManage = useAdminPerm(AdminPerm.ManageOrganizations);
    const [editing, setEditing] = useState(false);
    const o = a.overrides;

    return (
        <div className="overflow-hidden surface-lit rounded-xl border border-border bg-card">
            <div className="flex flex-wrap items-start justify-between gap-3 px-4 py-3">
                <div className="min-w-0">
                    <div className="flex items-center gap-2">
                        <SlidersHorizontal className="size-4 text-subtle-foreground" />
                        <span className="text-[13px] font-medium text-foreground">Custom pool and AI allowance</span>
                        {o && (
                            <StatusBadge tone="info" dot>
                                custom
                            </StatusBadge>
                        )}
                    </div>
                    <p className="mt-1 text-[12.5px] text-muted-foreground">
                        {o
                            ? "Set for this agency in place of its plan's own values."
                            : "This agency uses its plan's own pool and AI allowance."}
                    </p>
                </div>
                {canManage && (
                    <Button size="sm" variant="outline" onClick={() => setEditing(true)}>
                        <SlidersHorizontal className="size-3.5" /> Edit
                    </Button>
                )}
            </div>

            <dl className="grid grid-cols-1 gap-px border-t border-border bg-border sm:grid-cols-3">
                <div className={CELL}>
                    <dt className={DT}>Monthly send pool</dt>
                    <dd className={`${DD} tabular-nums`}>
                        {o?.monthly_send_pool != null ? `${o.monthly_send_pool.toLocaleString()} sends` : planPool(a)}
                    </dd>
                </div>
                <div className={CELL}>
                    <dt className={DT}>Monthly AI allowance</dt>
                    <dd className={`${DD} tabular-nums`}>{o?.ai_allowance_usd != null ? usd(o.ai_allowance_usd) : planAllowance(a)}</dd>
                </div>
                <div className={CELL}>
                    <dt className={DT}>Reason</dt>
                    <dd className={DD}>
                        {o?.reason || "None"}
                        {o?.updated_at && <span className="text-muted-foreground"> · set {fmtDay(o.updated_at)}</span>}
                    </dd>
                </div>
            </dl>

            <AgencyOverridesDialog agency={a} open={editing && canManage} onOpenChange={setEditing} />
        </div>
    );
}

export function AgencyOverridesDialog({
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
                    <DialogTitle>Custom pool and AI allowance</DialogTitle>
                    <DialogDescription>
                        What {agency.name} gets each month in place of its plan's own values. Leave a field empty to use the plan
                        default.
                    </DialogDescription>
                </DialogHeader>
                {/* Mounted per open, so the form starts from the stored values. */}
                <OverridesForm agency={agency} onClose={() => onOpenChange(false)} />
            </DialogContent>
        </Dialog>
    );
}

function poolProblem(n: number | null): string | null {
    if (n === null) return null;
    if (!Number.isInteger(n) || n < 1 || n > POOL_MAX) return `A whole number from 1 to ${POOL_MAX.toLocaleString()}.`;
    return null;
}

function allowanceProblem(n: number | null): string | null {
    if (n === null) return null;
    if (Number.isNaN(n) || n > AI_MAX_USD) return `An amount from $0 to $${AI_MAX_USD.toLocaleString()}.`;
    return null;
}

function OverridesForm({ agency, onClose }: { agency: AdminAgency; onClose: () => void }) {
    const stored = agency.overrides;
    const [pool, setPool] = useState(stored?.monthly_send_pool != null ? String(stored.monthly_send_pool) : "");
    const [allowance, setAllowance] = useState(stored?.ai_allowance_usd != null ? String(stored.ai_allowance_usd) : "");
    const [reason, setReason] = useState(stored?.reason ?? "");

    const poolValue = parseAmount(pool);
    const allowanceValue = parseAmount(allowance);
    const poolError = poolProblem(poolValue);
    const allowanceError = allowanceProblem(allowanceValue);
    // A reason is owed for a custom value, not for going back to the plan's own.
    const needsReason = (poolValue !== null || allowanceValue !== null) && !reason.trim();
    const blocked = !!poolError || !!allowanceError || needsReason;

    const save = useAgencyWrite(
        agency.id,
        () =>
            setAgencyOverrides(agency.id, {
                monthly_send_pool: poolValue,
                ai_allowance_usd: allowanceValue === null ? null : Math.round(allowanceValue * 100) / 100,
                reason: reason.trim(),
            }),
        { done: "Custom values saved", failed: "Failed to save the custom values" },
        onClose,
    );

    function submit(e: FormEvent) {
        e.preventDefault();
        if (!blocked && !save.isPending) save.mutate();
    }

    return (
        <form onSubmit={submit} className="grid gap-4">
            <div className="grid gap-3 sm:grid-cols-2">
                <div className="space-y-1.5">
                    <Label htmlFor="agency-override-pool" className={FIELD_LABEL}>
                        Monthly send pool
                    </Label>
                    <Input
                        id="agency-override-pool"
                        inputMode="numeric"
                        placeholder={planPool(agency)}
                        value={pool}
                        onChange={(e) => setPool(e.target.value)}
                        aria-invalid={!!poolError}
                        className="tabular-nums"
                        autoFocus
                    />
                    {poolError && (
                        <p role="alert" className="text-xs text-[var(--admin-danger)]">
                            {poolError}
                        </p>
                    )}
                </div>
                <div className="space-y-1.5">
                    <Label htmlFor="agency-override-ai" className={FIELD_LABEL}>
                        Monthly AI allowance (USD)
                    </Label>
                    <Input
                        id="agency-override-ai"
                        inputMode="decimal"
                        placeholder={planAllowance(agency)}
                        value={allowance}
                        onChange={(e) => setAllowance(e.target.value)}
                        aria-invalid={!!allowanceError}
                        className="tabular-nums"
                    />
                    {allowanceError && (
                        <p role="alert" className="text-xs text-[var(--admin-danger)]">
                            {allowanceError}
                        </p>
                    )}
                </div>
            </div>
            <div className="space-y-1.5">
                <Label htmlFor="agency-override-reason" className={FIELD_LABEL}>
                    Reason
                </Label>
                <Input
                    id="agency-override-reason"
                    placeholder="The deal these values come from"
                    value={reason}
                    maxLength={500}
                    onChange={(e) => setReason(e.target.value)}
                />
                <p className="text-xs leading-relaxed text-muted-foreground">
                    Required while a custom value is set. Saving replaces both values, so an emptied field goes back to the plan
                    default. A new AI allowance refills from the next period; add AI balance for this one.
                </p>
            </div>
            <DialogFooter>
                <Button type="button" variant="outline" onClick={onClose}>
                    Cancel
                </Button>
                <Button type="submit" disabled={blocked || save.isPending}>
                    {save.isPending ? "Saving…" : "Save"}
                </Button>
            </DialogFooter>
        </form>
    );
}
