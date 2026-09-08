// Convert a shared worker into a dedicated one bound to a workspace. The
// backend refuses a worker that still holds mailboxes unless a drain target
// is named, so the dialog requires one whenever the chosen worker is loaded.

import { useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { toast } from "sonner";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import {
    Dialog,
    DialogContent,
    DialogDescription,
    DialogFooter,
    DialogHeader,
    DialogTitle,
} from "@/components/ui/dialog";
import {
    Select,
    SelectContent,
    SelectItem,
    SelectTrigger,
    SelectValue,
} from "@/components/ui/select";
import { convertWorkerToDedicated } from "@/lib/api/client/admin/fleet";
import { listManagedWorkers } from "@/lib/api/client/admin/workers";
import type { ManagedWorker } from "@/lib/api/models/admin";
import { OrgPicker, type PickedOrg } from "./OrgPicker";

const UUID_RE = /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i;

function workerLabel(w: ManagedWorker): string {
    return `${w.name || w.id.slice(0, 8)} · ${w.free_tier ? "free" : "premium"} · ${w.health_state} · ${w.account_count} mailbox${w.account_count === 1 ? "" : "es"}`;
}

export function ConvertDedicatedDialog({
    open,
    onOpenChange,
}: {
    open: boolean;
    onOpenChange: (v: boolean) => void;
}) {
    const qc = useQueryClient();
    const [workerId, setWorkerId] = useState("");
    const [org, setOrg] = useState<PickedOrg | null>(null);
    const [subscriptionId, setSubscriptionId] = useState("");
    const [drainTo, setDrainTo] = useState("");

    const workersQ = useQuery({
        queryKey: ["admin", "workers", "managed"],
        queryFn: listManagedWorkers,
        enabled: open,
        staleTime: 30_000,
    });
    const workers = workersQ.data?.data ?? [];
    const shared = workers.filter((w) => w.worker_type === "shared");
    const worker = workers.find((w) => w.id === workerId) ?? null;
    const needsDrain = !!worker && worker.account_count > 0;
    // Mailboxes keep their tier when drained, so the target must match it.
    const drainTargets = workers.filter((w) => w.id !== workerId && (!worker || w.free_tier === worker.free_tier));

    const subOk = UUID_RE.test(subscriptionId.trim());
    const canSubmit = !!workerId && !!org && subOk && (!needsDrain || !!drainTo);

    const mutation = useMutation({
        mutationFn: () =>
            convertWorkerToDedicated(workerId, {
                organization_id: org!.id,
                subscription_id: subscriptionId.trim(),
                drain_to_worker_id: drainTo || null,
            }),
        onSuccess: (res) => {
            toast.success(
                res.new_assignment
                    ? `Worker is now dedicated to ${org?.name}${res.accounts_drained ? ` (${res.accounts_drained} mailboxes drained)` : ""}`
                    : "Binding already existed; worker type set to dedicated",
            );
            qc.invalidateQueries({ queryKey: ["admin", "workers"] });
            qc.invalidateQueries({ queryKey: ["admin", "fleet"] });
            reset();
            onOpenChange(false);
        },
        onError: (e: Error) => toast.error(e.message || "Conversion failed"),
    });

    function reset() {
        setWorkerId("");
        setOrg(null);
        setSubscriptionId("");
        setDrainTo("");
    }

    return (
        <Dialog
            open={open}
            onOpenChange={(v) => {
                if (!v && mutation.isPending) return;
                if (!v) reset();
                onOpenChange(v);
            }}
        >
            <DialogContent
                onEscapeKeyDown={(e) => {
                    // A picker popover owns Escape while it is open.
                    if (document.querySelector("[data-floating]")) e.preventDefault();
                }}
            >
                <DialogHeader>
                    <DialogTitle>Convert a worker to dedicated</DialogTitle>
                    <DialogDescription>
                        The worker leaves the shared pool and only this workspace's mailboxes are placed on it.
                        Its existing mailboxes must be drained to another worker of the same tier first.
                    </DialogDescription>
                </DialogHeader>

                <div className="space-y-4">
                    <div className="space-y-1.5">
                        <Label className="text-xs">Shared worker</Label>
                        <Select value={workerId || undefined} onValueChange={(v) => { setWorkerId(v); setDrainTo(""); }}>
                            <SelectTrigger className="h-8 w-full text-[12.5px]">
                                <SelectValue placeholder={workersQ.isLoading ? "Loading workers…" : "Pick a shared worker"} />
                            </SelectTrigger>
                            <SelectContent>
                                {shared.length === 0 && (
                                    <div className="px-2 py-1.5 text-xs text-muted-foreground">No shared workers.</div>
                                )}
                                {shared.map((w) => (
                                    <SelectItem key={w.id} value={w.id} className="text-[12.5px]">
                                        {workerLabel(w)}
                                    </SelectItem>
                                ))}
                            </SelectContent>
                        </Select>
                    </div>

                    <div className="space-y-1.5">
                        <Label className="text-xs">Workspace</Label>
                        <OrgPicker value={org} onChange={setOrg} />
                    </div>

                    <div className="space-y-1.5">
                        <Label htmlFor="sub-id" className="text-xs">
                            Subscription id
                        </Label>
                        <Input
                            id="sub-id"
                            value={subscriptionId}
                            onChange={(e) => setSubscriptionId(e.target.value)}
                            placeholder="00000000-0000-0000-0000-000000000000"
                            className="h-8 font-mono text-[12px]"
                        />
                        <p className="text-[11px] text-muted-foreground">
                            The workspace's subscription row (a UUID). The organization page shows only the plan and
                            status, so read the id from the <code>subscriptions</code> table for this workspace, or from
                            the Stripe subscription's metadata.
                            {subscriptionId && !subOk && <span className="ml-1 text-red-600">Not a UUID.</span>}
                        </p>
                    </div>

                    <div className="space-y-1.5">
                        <Label className="text-xs">
                            Drain mailboxes to{" "}
                            <span className="font-normal text-muted-foreground">
                                {needsDrain ? `(required: ${worker!.account_count} assigned)` : "(optional)"}
                            </span>
                        </Label>
                        <Select value={drainTo || undefined} onValueChange={setDrainTo} disabled={!workerId}>
                            <SelectTrigger className="h-8 w-full text-[12.5px]">
                                <SelectValue placeholder={needsDrain ? "Pick where the current mailboxes go" : "Leave as is"} />
                            </SelectTrigger>
                            <SelectContent>
                                {drainTargets.length === 0 && (
                                    <div className="px-2 py-1.5 text-xs text-muted-foreground">No other worker in this tier.</div>
                                )}
                                {drainTargets.map((w) => (
                                    <SelectItem key={w.id} value={w.id} className="text-[12.5px]">
                                        {workerLabel(w)}
                                    </SelectItem>
                                ))}
                            </SelectContent>
                        </Select>
                    </div>
                </div>

                <DialogFooter>
                    <Button variant="outline" onClick={() => onOpenChange(false)} disabled={mutation.isPending}>
                        Cancel
                    </Button>
                    <Button onClick={() => mutation.mutate()} disabled={!canSubmit || mutation.isPending}>
                        {mutation.isPending ? "Converting…" : "Convert to dedicated"}
                    </Button>
                </DialogFooter>
            </DialogContent>
        </Dialog>
    );
}
