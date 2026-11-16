// The worker Auto Scaling Group's size against its cap, with the cap editable.
// Renders nothing on an instance that does not manage a group.

import { useEffect, useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { toast } from "sonner";
import { AlertTriangle, Gauge } from "lucide-react";
import { Callout, StatusDot } from "@/components/ui/kit";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { useConfirm } from "@/components/ConfirmDialog";
import { APIError } from "@/lib/api/client";
import {
    FLEET_AUTOSCALING_NOT_CONFIGURED,
    getFleetScale,
    setFleetMax,
    type AdminFleetScale,
} from "@/lib/api/client/admin/fleet";

const QUERY_KEY = ["admin", "fleet", "scale"] as const;

/** Share of the cap at which the fleet is reported as close to it; matches fleetscale.WarnShare. */
const WARN_SHARE = 0.8;

function capState(s: AdminFleetScale): "at-cap" | "near-cap" | "ok" {
    if (s.desired >= s.max) return "at-cap";
    if (s.desired >= WARN_SHARE * s.max) return "near-cap";
    return "ok";
}

export function FleetScalePanel() {
    const qc = useQueryClient();
    const confirm = useConfirm();
    const { data, error, refetch, isFetching } = useQuery({
        queryKey: QUERY_KEY,
        queryFn: getFleetScale,
        refetchInterval: 30_000,
        retry: (count, err) => !(err instanceof APIError && err.code === FLEET_AUTOSCALING_NOT_CONFIGURED) && count < 2,
    });
    const [draft, setDraft] = useState("");
    // Follows the cap itself, so a refetch does not overwrite what the operator is typing.
    const currentMax = data?.max;
    useEffect(() => {
        if (currentMax !== undefined) setDraft(String(currentMax));
    }, [currentMax]);

    const mutation = useMutation({
        mutationFn: setFleetMax,
        onSuccess: (next) => {
            qc.setQueryData(QUERY_KEY, next);
            toast.success(`Fleet cap set to ${next.max}`);
        },
        onError: (err: Error) => toast.error(err.message || "Failed to change the fleet cap"),
    });

    // Hidden only on an instance that does not manage a group; any other failure is shown.
    if (error instanceof APIError && error.code === FLEET_AUTOSCALING_NOT_CONFIGURED) return null;
    const retry = (
        <Button type="button" size="sm" variant="outline" onClick={() => void refetch()} disabled={isFetching}>
            Retry
        </Button>
    );
    if (!data) {
        if (!error) return null;
        return (
            <Callout tone="danger" icon={AlertTriangle} title="Fleet autoscaling status could not be read" actions={retry} className="mb-4">
                {error.message || "The backend did not answer."}
            </Callout>
        );
    }
    const scale = data;

    const floor = Math.max(1, data.min, data.desired);
    const n = Number(draft);
    const valid = Number.isInteger(n) && n >= floor && data.ceiling > 0 && n <= data.ceiling;
    const changed = valid && n !== data.max;
    const state = capState(data);

    async function save() {
        if (!changed) return;
        const ok = await confirm({
            title: `Set the fleet cap to ${n}?`,
            description:
                n > scale.max
                    ? `The fleet may grow to ${n} workers when CPU or memory calls for it. Each worker is billed while it runs.`
                    : `The fleet will not grow past ${n} workers. Running workers are not stopped.`,
            confirmLabel: "Set cap",
        });
        if (ok) await mutation.mutateAsync(n).catch(() => undefined);
    }

    return (
        <div className="mb-4 space-y-3">
            <div className="flex flex-wrap items-center gap-x-6 gap-y-2 rounded-lg border border-border px-3.5 py-3">
                <span className="flex items-center gap-2 text-[13px]">
                    <Gauge className="size-4 text-muted-foreground" />
                    <span className="font-medium text-foreground">Autoscaling</span>
                </span>
                <StatusDot tone={state === "at-cap" ? "danger" : state === "near-cap" ? "warning" : "success"} className="tabular-nums">
                    {data.in_service} running{data.pending > 0 ? ` (${data.pending} starting)` : ""} of max {data.max}, min {data.min}
                </StatusDot>
                {error && (
                    <span className="flex items-center gap-2 text-xs text-muted-foreground" title={error.message}>
                        Could not refresh; showing the last reading.
                        <Button type="button" size="xs" variant="ghost" onClick={() => void refetch()} disabled={isFetching}>
                            Retry
                        </Button>
                    </span>
                )}
                <form
                    className="ml-auto flex items-center gap-2"
                    onSubmit={(e) => {
                        e.preventDefault();
                        void save();
                    }}
                >
                    <label htmlFor="fleet-cap" className="text-xs text-muted-foreground">
                        Cap
                    </label>
                    <Input
                        id="fleet-cap"
                        inputMode="numeric"
                        className="h-7 w-20 tabular-nums"
                        value={draft}
                        aria-invalid={draft !== "" && !valid}
                        onChange={(e) => setDraft(e.target.value.replace(/[^0-9]/g, ""))}
                        disabled={data.ceiling === 0}
                    />
                    <span className="text-xs text-subtle-foreground tabular-nums">
                        {data.ceiling > 0 ? `${floor} to ${data.ceiling}` : "no ceiling tag"}
                    </span>
                    <Button type="submit" size="sm" variant="outline" disabled={!changed || mutation.isPending}>
                        Save
                    </Button>
                </form>
            </div>
            {state === "at-cap" && (
                <Callout tone="danger" icon={AlertTriangle} title="The fleet is at its cap and cannot grow">
                    New load stays on the running workers. Raise the cap to let the fleet add workers.
                </Callout>
            )}
            {state === "near-cap" && (
                <Callout tone="warning" icon={AlertTriangle} title="The fleet is close to its cap">
                    {data.desired} of {data.max} workers. Raise the cap if load keeps growing.
                </Callout>
            )}
        </div>
    );
}
