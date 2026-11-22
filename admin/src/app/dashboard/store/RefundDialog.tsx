// Refunds a store order to the workspace balance: one quote line's charge, or
// an amount with a reason, capped at what was charged and not yet refunded.
// The idempotency key belongs to the request body, so a retried click after a
// timeout reuses it and the server never pays twice.

import { useRef, useState } from "react";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { toast } from "sonner";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Textarea } from "@/components/ui/textarea";
import { Segmented } from "@/components/ui/kit";
import {
    Dialog,
    DialogContent,
    DialogDescription,
    DialogFooter,
    DialogHeader,
    DialogTitle,
} from "@/components/ui/dialog";
import { ReauthCancelledError } from "@/lib/api/client";
import { refundStoreOrder, type AdminStoreOrder, type StoreRefund } from "@/lib/api/client/admin/store";
import { cn } from "@/lib/utils";
import { dollarsToMicros, newIdempotencyKey, refundableMicros, usd } from "./format";
import { storeErrorText } from "./useStoreAction";

type Mode = "line" | "amount";

export function RefundDialog({
    order,
    open,
    onOpenChange,
    onRefunded,
}: {
    order: AdminStoreOrder;
    open: boolean;
    onOpenChange: (open: boolean) => void;
    onRefunded: (order: AdminStoreOrder) => void;
}) {
    return (
        <Dialog open={open} onOpenChange={onOpenChange}>
            <DialogContent className="sm:max-w-lg">
                {open && <RefundForm order={order} onDone={onRefunded} onCancel={() => onOpenChange(false)} />}
            </DialogContent>
        </Dialog>
    );
}

function RefundForm({
    order,
    onDone,
    onCancel,
}: {
    order: AdminStoreOrder;
    onDone: (order: AdminStoreOrder) => void;
    onCancel: () => void;
}) {
    const qc = useQueryClient();
    const cap = refundableMicros(order);
    const refunded = new Set(order.refunded_lines ?? []);
    const openLines = order.lines.map((l, i) => ({ line: l, index: i })).filter((x) => !refunded.has(x.index) && x.line.amount_micros > 0);
    const [mode, setMode] = useState<Mode>(openLines.length > 0 ? "line" : "amount");
    const [line, setLine] = useState<number | null>(openLines[0]?.index ?? null);
    const [amount, setAmount] = useState("");
    const [reason, setReason] = useState("");
    const keyFor = useRef<{ body: string; key: string } | null>(null);

    const micros = dollarsToMicros(amount);
    const amountError =
        mode !== "amount" || micros === null
            ? null
            : Number.isNaN(micros) || micros <= 0
              ? "Enter an amount in dollars, like 12.50"
              : micros > cap
                ? `At most ${usd(cap)} is left to refund`
                : null;

    const body: StoreRefund | null =
        mode === "line"
            ? line === null
                ? null
                : { line }
            : micros && !amountError && reason.trim()
              ? { amount_micros: micros, reason: reason.trim() }
              : null;

    const refund = useMutation({
        mutationFn: (req: { body: StoreRefund; key: string }) => refundStoreOrder(order.id, req.body, req.key),
        onSuccess: (res) => {
            toast.success(res.refunded_micros > 0 ? `Refunded ${usd(res.refunded_micros)} to the workspace balance` : "Already refunded; nothing more was paid");
            qc.invalidateQueries({ queryKey: ["admin", "store"] });
            onDone(res.order);
        },
        onError: (e) => {
            if (!(e instanceof ReauthCancelledError)) toast.error(storeErrorText(e));
        },
    });

    function submit() {
        if (!body) return;
        const sig = JSON.stringify(body);
        if (keyFor.current?.body !== sig) keyFor.current = { body: sig, key: newIdempotencyKey() };
        refund.mutate({ body, key: keyFor.current.key });
    }

    const lineAmount = line !== null ? order.lines[line]?.amount_micros : undefined;
    const total = mode === "line" ? lineAmount : micros && !Number.isNaN(micros) ? micros : undefined;

    return (
        <>
            <DialogHeader>
                <DialogTitle>Refund to the workspace balance</DialogTitle>
                <DialogDescription>
                    {order.organization_name} was charged {usd(order.charged_micros)} and has had {usd(order.refunded_micros)} back.
                    Up to {usd(cap)} can still be refunded. The money goes to the workspace's balance, not the card.
                </DialogDescription>
            </DialogHeader>

            <div className="grid gap-4">
                <Segmented
                    value={mode}
                    onChange={setMode}
                    ariaLabel="Refund type"
                    options={[
                        { value: "line", label: "A quote line", disabled: openLines.length === 0 },
                        { value: "amount", label: "An amount" },
                    ]}
                />

                {mode === "line" ? (
                    openLines.length === 0 ? (
                        <p className="text-[13px] text-muted-foreground">Every line with a charge has been refunded.</p>
                    ) : (
                        <div role="radiogroup" aria-label="Line to refund" className="divide-y divide-border overflow-hidden rounded-lg border border-border">
                            {openLines.map(({ line: l, index }) => (
                                <button
                                    key={index}
                                    type="button"
                                    role="radio"
                                    aria-checked={line === index}
                                    onClick={() => setLine(index)}
                                    className={cn(
                                        "flex w-full items-center justify-between gap-3 px-3 py-2 text-left text-[13px] transition-colors",
                                        line === index ? "bg-[var(--admin-accent-weak)]" : "hover:bg-accent/60",
                                    )}
                                >
                                    <span className="min-w-0">
                                        <span className="block truncate font-medium text-foreground">{l.domain}</span>
                                        <span className="block text-xs text-muted-foreground">
                                            {l.type === "domain" ? `Domain, ${l.years ?? 1} year${(l.years ?? 1) === 1 ? "" : "s"}` : `${l.count ?? 0} mailbox${l.count === 1 ? "" : "es"}`}
                                        </span>
                                    </span>
                                    <span className="tabular-nums">{usd(l.amount_micros)}</span>
                                </button>
                            ))}
                        </div>
                    )
                ) : (
                    <>
                        <div className="space-y-1.5">
                            <Label htmlFor="refund-amount" className="text-xs font-medium text-muted-foreground">
                                Amount (USD)
                            </Label>
                            <Input
                                id="refund-amount"
                                inputMode="decimal"
                                placeholder={`Up to ${usd(cap)}`}
                                value={amount}
                                onChange={(e) => setAmount(e.target.value)}
                                aria-invalid={!!amountError}
                            />
                            {amountError && <p className="text-xs text-[var(--admin-danger)]">{amountError}</p>}
                        </div>
                        <div className="space-y-1.5">
                            <Label htmlFor="refund-reason" className="text-xs font-medium text-muted-foreground">
                                Reason
                            </Label>
                            <Textarea
                                id="refund-reason"
                                placeholder="Why this refund (kept in the audit log)"
                                value={reason}
                                onChange={(e) => setReason(e.target.value)}
                                rows={3}
                            />
                        </div>
                    </>
                )}
            </div>

            <DialogFooter>
                <Button variant="outline" onClick={onCancel}>
                    Cancel
                </Button>
                <Button variant="destructive" onClick={submit} disabled={!body || refund.isPending || cap <= 0}>
                    {refund.isPending ? "Refunding…" : total ? `Refund ${usd(total)}` : "Refund"}
                </Button>
            </DialogFooter>
        </>
    );
}
