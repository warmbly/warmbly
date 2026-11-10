// One-off credit from an operator: sends added to the current period, and
// money added to the shared AI balance. Neither changes what the plan refills.

import { useState, type FormEvent } from "react";
import { Plus } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog";
import { useAdminPerm } from "@/hooks/useAdminPerm";
import { AdminPerm } from "@/lib/auth/permissions";
import { creditAgencyAI, creditAgencyPool, type AdminAgencyDetail } from "@/lib/api/client/admin/agencies";
import { newIdempotencyKey, parseAmount, usd } from "./agencyFormat";
import { useAgencyWrite } from "./useAgencyWrite";

const SENDS_MAX = 10_000_000;
const AI_MAX_USD = 100_000;
const FIELD_LABEL = "text-xs font-medium text-muted-foreground";

// The balance is kept in cents, so an amount is rounded to them before it is sent.
const cents = (n: number) => Math.round(n * 100) / 100;

// Why sends cannot be added, when that is already certain.
function noPoolReason(d: AdminAgencyDetail): string | null {
    if (d.usage.unlimited) return "The pool has no cap, so there are no sends to add.";
    if (d.agency.entitled === false) return "No plan is in force, so there is no pool to add sends to.";
    return null;
}

export function AgencyCreditCard({ detail: d }: { detail: AdminAgencyDetail }) {
    const canManage = useAdminPerm(AdminPerm.ManageOrganizations);
    const [adding, setAdding] = useState<"sends" | "ai" | null>(null);
    const noPool = noPoolReason(d);
    // Nothing to add to where the instance does not charge for AI.
    const metered = !!d.ai_balance && !d.ai_balance.unmetered;
    const close = (v: boolean) => !v && setAdding(null);

    return (
        <div className="overflow-hidden surface-lit rounded-xl border border-border bg-card">
            <div className="flex flex-wrap items-start justify-between gap-3 px-4 py-3">
                <div className="min-w-0">
                    <div className="flex items-center gap-2">
                        <Plus className="size-4 text-subtle-foreground" />
                        <span className="text-[13px] font-medium text-foreground">One-off credit</span>
                    </div>
                    <p className="mt-1 text-[12.5px] text-muted-foreground">
                        {metered
                            ? "Added sends last until the period ends. Added AI balance does not expire."
                            : "Added sends last until the period ends."}
                        {noPool && ` ${noPool}`}
                    </p>
                </div>
                {canManage && (
                    <div className="flex shrink-0 items-center gap-1.5">
                        <Button size="sm" variant="outline" onClick={() => setAdding("sends")} disabled={!!noPool}>
                            <Plus className="size-3.5" /> Add sends
                        </Button>
                        {metered && (
                            <Button size="sm" variant="outline" onClick={() => setAdding("ai")}>
                                <Plus className="size-3.5" /> Add AI balance
                            </Button>
                        )}
                    </div>
                )}
            </div>

            <Dialog open={adding === "sends" && canManage} onOpenChange={close}>
                <DialogContent>
                    <DialogHeader>
                        <DialogTitle>Add sends</DialogTitle>
                        <DialogDescription>
                            Adds sends to {d.agency.name}'s pool for this period only. They are gone when the period ends on{" "}
                            {new Date(d.usage.period_end).toLocaleDateString()}.
                        </DialogDescription>
                    </DialogHeader>
                    <SendsForm agencyId={d.agency.id} onClose={() => setAdding(null)} />
                </DialogContent>
            </Dialog>

            <Dialog open={adding === "ai" && canManage} onOpenChange={close}>
                <DialogContent>
                    <DialogHeader>
                        <DialogTitle>Add AI balance</DialogTitle>
                        <DialogDescription>
                            Adds money to {d.agency.name}'s shared AI balance. It does not expire. You may be asked to confirm it
                            is you.
                        </DialogDescription>
                    </DialogHeader>
                    <AIForm agencyId={d.agency.id} onClose={() => setAdding(null)} />
                </DialogContent>
            </Dialog>
        </div>
    );
}

function SendsForm({ agencyId, onClose }: { agencyId: string; onClose: () => void }) {
    const add = useAgencyWrite(
        agencyId,
        (v: { amount: number; reason: string }) => creditAgencyPool(agencyId, { sends: v.amount, reason: v.reason }),
        { done: "Sends added", failed: "Failed to add the sends" },
        onClose,
    );
    return (
        <CreditForm
            amountLabel="Sends"
            placeholder="50,000"
            inputMode="numeric"
            problem={(n) =>
                !Number.isInteger(n) || n < 1 || n > SENDS_MAX ? `A whole number from 1 to ${SENDS_MAX.toLocaleString()}.` : null
            }
            submitLabel={(n) => (n ? `Add ${n.toLocaleString()} sends` : "Add sends")}
            pending={add.isPending}
            onSubmit={(amount, reason) => add.mutate({ amount, reason })}
            onClose={onClose}
        />
    );
}

function AIForm({ agencyId, onClose }: { agencyId: string; onClose: () => void }) {
    // One key per dialog open, so a retried request never adds the money twice.
    const [key] = useState(newIdempotencyKey);
    const add = useAgencyWrite(
        agencyId,
        (v: { amount: number; reason: string }) => creditAgencyAI(agencyId, { amount_usd: v.amount, reason: v.reason }, key),
        { done: "AI balance added", failed: "Failed to add the AI balance" },
        onClose,
    );
    return (
        <CreditForm
            amountLabel="Amount (USD)"
            placeholder="25.00"
            inputMode="decimal"
            problem={(n) =>
                Number.isNaN(n) || cents(n) < 0.01 || n > AI_MAX_USD ? `An amount from $0.01 to $${AI_MAX_USD.toLocaleString()}.` : null
            }
            submitLabel={(n) => (n ? `Add ${usd(cents(n))}` : "Add AI balance")}
            pending={add.isPending}
            onSubmit={(amount, reason) => add.mutate({ amount: cents(amount), reason })}
            onClose={onClose}
        />
    );
}

function CreditForm({
    amountLabel,
    placeholder,
    inputMode,
    problem,
    submitLabel,
    pending,
    onSubmit,
    onClose,
}: {
    amountLabel: string;
    placeholder: string;
    inputMode: "numeric" | "decimal";
    problem: (n: number) => string | null;
    // Names the amount on the button once it is valid.
    submitLabel: (n: number | null) => string;
    pending: boolean;
    onSubmit: (amount: number, reason: string) => void;
    onClose: () => void;
}) {
    const [amount, setAmount] = useState("");
    const [reason, setReason] = useState("");
    const value = parseAmount(amount);
    const error = value === null ? null : problem(value);
    const ready = value !== null && !error && !!reason.trim();

    function submit(e: FormEvent) {
        e.preventDefault();
        if (ready && !pending) onSubmit(value, reason.trim());
    }

    return (
        <form onSubmit={submit} className="grid gap-4">
            <div className="space-y-1.5">
                <Label htmlFor="agency-credit-amount" className={FIELD_LABEL}>
                    {amountLabel}
                </Label>
                <Input
                    id="agency-credit-amount"
                    inputMode={inputMode}
                    placeholder={placeholder}
                    value={amount}
                    onChange={(e) => setAmount(e.target.value)}
                    aria-invalid={!!error}
                    className="tabular-nums"
                    autoFocus
                />
                {error && (
                    <p role="alert" className="text-xs text-[var(--admin-danger)]">
                        {error}
                    </p>
                )}
            </div>
            <div className="space-y-1.5">
                <Label htmlFor="agency-credit-reason" className={FIELD_LABEL}>
                    Reason
                </Label>
                <Input
                    id="agency-credit-reason"
                    placeholder="Why this agency gets it"
                    value={reason}
                    maxLength={500}
                    onChange={(e) => setReason(e.target.value)}
                />
            </div>
            <DialogFooter>
                <Button type="button" variant="outline" onClick={onClose}>
                    Cancel
                </Button>
                <Button type="submit" disabled={!ready || pending}>
                    {pending ? "Adding…" : submitLabel(ready ? value : null)}
                </Button>
            </DialogFooter>
        </form>
    );
}
