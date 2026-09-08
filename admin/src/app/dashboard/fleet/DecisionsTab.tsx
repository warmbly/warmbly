// Decision log: what the placement and rebalance loops did and why. Filtered
// server-side by kind and worker; before/after JSON expands inline. No event
// fires when a loop writes a decision, so the list polls at 30s.

import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { Link } from "react-router-dom";
import { ChevronDown, ChevronRight } from "lucide-react";
import { Badge } from "@/components/ui/badge";
import { Skeleton } from "@/components/ui/skeleton";
import { ErrorState } from "@/components/ErrorState";
import { SelectFilter } from "@/components/data/Explorer";
import { listFleetDecisions, type AdminFleetDecision } from "@/lib/api/client/admin/fleet";
import { listManagedWorkers } from "@/lib/api/client/admin/workers";
import { fmtAgo, fmtDateTime, shortId } from "./format";

const LIMIT = 200;

function pretty(v: unknown): string {
    if (v == null) return "";
    try {
        return JSON.stringify(v, null, 2);
    } catch {
        return String(v);
    }
}

function DecisionRow({ d }: { d: AdminFleetDecision }) {
    const [open, setOpen] = useState(false);
    const hasDiff = d.before != null || d.after != null;
    return (
        <>
            <tr className="border-t border-border">
                <td className="px-3 py-2 text-xs text-muted-foreground whitespace-nowrap" title={fmtDateTime(d.created_at)}>
                    {fmtAgo(d.created_at)}
                </td>
                <td className="px-3 py-2">
                    <Badge variant="outline" className="text-[10px] font-mono">
                        {d.kind}
                    </Badge>
                </td>
                <td className="px-3 py-2 text-xs">
                    {d.worker_id ? (
                        <Link to={`/workers/${d.worker_id}`} className="font-medium text-[var(--admin-accent-strong)] hover:underline">
                            {d.worker_name || shortId(d.worker_id)}
                        </Link>
                    ) : (
                        <span className="text-muted-foreground">—</span>
                    )}
                </td>
                <td className="px-3 py-2 font-mono text-[11px] text-muted-foreground">{shortId(d.mailbox_id)}</td>
                <td className="px-3 py-2 text-xs max-w-md">{d.reason || <span className="text-muted-foreground">—</span>}</td>
                <td className="px-3 py-2 text-xs text-muted-foreground">{d.triggered_by || "—"}</td>
                <td className="px-3 py-2 text-right">
                    {hasDiff && (
                        <button
                            type="button"
                            onClick={() => setOpen((v) => !v)}
                            className="inline-flex items-center gap-1 rounded px-1.5 py-1 text-[11px] text-muted-foreground hover:bg-muted hover:text-foreground"
                        >
                            {open ? <ChevronDown className="size-3" /> : <ChevronRight className="size-3" />}
                            {open ? "Hide" : "Diff"}
                        </button>
                    )}
                </td>
            </tr>
            {open && hasDiff && (
                <tr className="border-t border-border/60 bg-muted/30">
                    <td colSpan={7} className="px-3 py-2">
                        <div className="grid gap-3 md:grid-cols-2">
                            <div>
                                <div className="mb-1 text-[10px] font-semibold uppercase tracking-wider text-muted-foreground">Before</div>
                                <pre className="max-h-64 overflow-auto rounded border border-border bg-card p-2 font-mono text-[11px] leading-relaxed">
                                    {pretty(d.before) || "(none)"}
                                </pre>
                            </div>
                            <div>
                                <div className="mb-1 text-[10px] font-semibold uppercase tracking-wider text-muted-foreground">After</div>
                                <pre className="max-h-64 overflow-auto rounded border border-border bg-card p-2 font-mono text-[11px] leading-relaxed">
                                    {pretty(d.after) || "(none)"}
                                </pre>
                            </div>
                        </div>
                    </td>
                </tr>
            )}
        </>
    );
}

export function DecisionsTab() {
    const [kind, setKind] = useState("");
    const [workerId, setWorkerId] = useState("");

    const { data, isLoading, error, refetch } = useQuery({
        queryKey: ["admin", "fleet", "decisions", kind, workerId],
        queryFn: () => listFleetDecisions({ kind: kind || undefined, worker_id: workerId || undefined, limit: LIMIT }),
        refetchInterval: 30_000,
    });

    // The kind facet comes from the unfiltered log so an option never
    // disappears when it is selected; same key as the base query, so it is
    // one request when no filter is active.
    const kindsQ = useQuery({
        queryKey: ["admin", "fleet", "decisions", "", ""],
        queryFn: () => listFleetDecisions({ limit: LIMIT }),
        staleTime: 60_000,
    });
    const kinds = Array.from(new Set((kindsQ.data?.data ?? []).map((d) => d.kind))).sort();

    const workersQ = useQuery({
        queryKey: ["admin", "workers", "managed"],
        queryFn: listManagedWorkers,
        staleTime: 60_000,
    });
    const workers = workersQ.data?.data ?? [];

    const rows = data?.data ?? [];

    return (
        <div>
            <div className="mb-3 flex flex-wrap items-end gap-3">
                <div className="w-52">
                    <div className="mb-1 text-[10px] font-semibold uppercase tracking-wider text-muted-foreground">Kind</div>
                    <SelectFilter
                        value={kind || "any"}
                        onChange={(v) => setKind(v === "any" ? "" : v)}
                        options={[{ value: "any", label: "Any kind" }, ...kinds.map((k) => ({ value: k, label: k }))]}
                    />
                </div>
                <div className="w-64">
                    <div className="mb-1 text-[10px] font-semibold uppercase tracking-wider text-muted-foreground">Worker</div>
                    <SelectFilter
                        value={workerId || "any"}
                        onChange={(v) => setWorkerId(v === "any" ? "" : v)}
                        options={[
                            { value: "any", label: "Any worker" },
                            ...workers.map((w) => ({ value: w.id, label: w.name || w.id.slice(0, 8) })),
                        ]}
                    />
                </div>
                <div className="pb-2 text-[12.5px] text-muted-foreground">
                    {isLoading ? "Loading…" : `${rows.length} decision${rows.length === 1 ? "" : "s"}`}
                    {rows.length >= LIMIT && " (newest " + LIMIT + ")"}
                </div>
            </div>

            {error ? (
                <ErrorState error={error} title="Failed to load the decision log" onRetry={() => refetch()} />
            ) : isLoading ? (
                <Skeleton className="h-40 w-full" />
            ) : rows.length === 0 ? (
                <div className="rounded-md border border-border bg-card p-4 text-sm text-muted-foreground">
                    No decisions recorded{kind || workerId ? " for these filters" : ""}. Rows appear when the assignment
                    loop places a mailbox, the rebalancer moves one, or a worker's health changes.
                </div>
            ) : (
                <div className="overflow-hidden rounded-lg border border-border bg-card">
                    <div className="overflow-x-auto">
                        <table className="w-full text-sm">
                            <thead className="bg-muted/40 text-[10.5px] font-semibold uppercase tracking-wider text-muted-foreground">
                                <tr>
                                    <th className="px-3 py-2 text-left">When</th>
                                    <th className="px-3 py-2 text-left">Kind</th>
                                    <th className="px-3 py-2 text-left">Worker</th>
                                    <th className="px-3 py-2 text-left">Mailbox</th>
                                    <th className="px-3 py-2 text-left">Reason</th>
                                    <th className="px-3 py-2 text-left">Triggered by</th>
                                    <th className="px-3 py-2 text-right" />
                                </tr>
                            </thead>
                            <tbody>
                                {rows.map((d) => (
                                    <DecisionRow key={d.id} d={d} />
                                ))}
                            </tbody>
                        </table>
                    </div>
                </div>
            )}
        </div>
    );
}
