// One machine in the fleet.
//
// Everything shown here is reported BY the node or resolved FOR it. There is
// no install, restart or reboot button, because nothing reaches into a
// machine any more: a node enrols with a token, heartbeats, and pulls the
// version it should run. What an operator can actually do is rename it, hold
// it at a version, and forget it.

import { useState } from "react";
import { useNavigate, useParams } from "react-router-dom";
import { useInfiniteQuery, useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { toast } from "sonner";
import { AlertTriangle, ArrowRightLeft, Inbox, Pin, PinOff, RefreshCw, ServerOff, Trash2 } from "lucide-react";
import { PageHeader } from "@/components/layout/PageHeader";
import { Button } from "@/components/ui/button";
import { Skeleton } from "@/components/ui/skeleton";
import { Input } from "@/components/ui/input";
import { Checkbox } from "@/components/ui/checkbox";
import {
    Callout,
    EmptyState,
    Panel,
    Property,
    PropertyList,
    Section,
    Stat,
    StatGrid,
    StatusBadge,
} from "@/components/ui/kit";
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
import { getWorkerEmails, getWorkerStats, reassignWorkerEmails } from "@/lib/api/client/admin/workers";
import {
    deleteFleetNode,
    listFleetNodes,
    nodeNeedsUpdate,
    nodeState,
    patchFleetNode,
    type FleetNode,
} from "@/lib/api/client/admin/fleetNodes";
import type { AdminWorkerEmail } from "@/lib/api/models/admin";
import { TONE_PANEL, TONE_TEXT } from "@/lib/tones";
import { cn } from "@/lib/utils";
import { NodeStatePill } from "./fleet/tones";
import { ResourceUsage } from "./fleet/ResourceUsage";
import { NodeDiagnostics } from "./fleet/NodeDiagnostics";
import { ErrorState } from "@/components/ErrorState";

const CRUMBS = [{ label: "Workers", to: "/workers" }];

function Mono({ children }: { children: React.ReactNode }) {
    return <span className="font-mono text-xs">{children}</span>;
}

function uptime(seconds?: number): string {
    if (seconds === undefined) return "—";
    const h = Math.floor(seconds / 3600);
    if (h >= 24) return `${Math.floor(h / 24)}d ${h % 24}h`;
    if (h >= 1) return `${h}h ${Math.floor((seconds % 3600) / 60)}m`;
    return `${Math.floor(seconds / 60)}m`;
}

export default function WorkerDetailPage() {
    const { id = "" } = useParams();
    const nav = useNavigate();
    const qc = useQueryClient();

    const [pinDraft, setPinDraft] = useState("");
    const [selected, setSelected] = useState<Set<string>>(new Set());
    const [reassignOpen, setReassignOpen] = useState(false);

    const nodeQ = useQuery({
        queryKey: ["admin", "fleet", "nodes"],
        queryFn: () => listFleetNodes(),
        refetchInterval: 30_000,
    });
    const node = (nodeQ.data?.data ?? []).find((n) => n.id === id) ?? null;

    const statsQ = useQuery({
        queryKey: ["admin", "workers", id, "stats"],
        queryFn: () => getWorkerStats(id),
        enabled: !!node && node.role === "worker",
    });

    const emailsQ = useInfiniteQuery({
        queryKey: ["admin", "workers", id, "emails"],
        queryFn: ({ pageParam }) => getWorkerEmails(id, pageParam),
        initialPageParam: undefined as string | undefined,
        getNextPageParam: (lastPage) =>
            lastPage.pagination.has_more ? lastPage.pagination.next_cursor ?? undefined : undefined,
        enabled: !!node && node.role === "worker",
    });

    const patch = useMutation({
        mutationFn: (body: { name?: string; pinned_version?: string }) => patchFleetNode(id, body),
        onSuccess: () => {
            qc.invalidateQueries({ queryKey: ["admin", "fleet"] });
            toast.success("Node updated");
        },
        onError: (e: Error) => toast.error(e.message || "Update failed"),
    });

    const remove = useMutation({
        mutationFn: () => deleteFleetNode(id),
        onSuccess: (res) => {
            toast.success(res.note || "Node removed");
            nav("/workers");
        },
        onError: (e: Error) => toast.error(e.message || "Remove failed"),
    });

    if (nodeQ.isLoading) {
        return (
            <div>
                <PageHeader breadcrumbs={CRUMBS} title={<Skeleton className="h-4 w-32" />} />
                <div className="grid gap-8 lg:grid-cols-[minmax(0,1fr)_300px]">
                    <div className="space-y-4">
                        <Skeleton className="h-24 w-full" />
                        <Skeleton className="h-48 w-full" />
                    </div>
                    <Skeleton className="h-72 w-full" />
                </div>
            </div>
        );
    }
    if (nodeQ.isError) return <div><PageHeader breadcrumbs={CRUMBS} title="Node unavailable" /><ErrorState error={nodeQ.error} onRetry={() => void nodeQ.refetch()} /></div>;
    if (!node) {
        return (
            <div>
                <PageHeader breadcrumbs={CRUMBS} title="Node not found" />
                <EmptyState
                    icon={ServerOff}
                    title="Node not found"
                    hint="It may have been removed."
                    action={
                        <Button size="sm" variant="outline" onClick={() => nav("/workers")}>
                            Back to workers
                        </Button>
                    }
                />
            </div>
        );
    }

    const state = nodeState(node);
    const mailboxes = emailsQ.data?.pages.flatMap((page) => page.data) ?? [];
    const mailboxTotal = Math.max(node.mailbox_count ?? 0, mailboxes.length);

    return (
        <div>
            <PageHeader
                breadcrumbs={CRUMBS}
                title={node.name || node.id.slice(0, 8)}
                meta={
                    <>
                        <NodeStatePill state={state} />
                        <StatusBadge tone={node.role === "worker" ? "accent" : "strong"}>{node.role}</StatusBadge>
                    </>
                }
            >
                <Button size="sm" variant="ghost" onClick={() => { void nodeQ.refetch(); void qc.invalidateQueries({ queryKey: ["admin", "node-diagnostics", id] }); }}>
                    <RefreshCw className={cn("size-3.5", nodeQ.isFetching && "animate-spin")} />
                    Refresh
                </Button>
            </PageHeader>

            <div className="grid gap-8 lg:grid-cols-[minmax(0,1fr)_300px]">
                <div className="min-w-0">
                    {node.last_error && (
                        <Callout tone="danger" icon={AlertTriangle} title="Last error" className="mb-8">
                            <span>The node reported an error on its last heartbeat. Inspect redacted evidence below; raw node error text is not displayed.</span>
                        </Callout>
                    )}

                    {node.role === "worker" && (
                        <Section
                            title="Sending"
                            description="Placement assigns these mailboxes; you never have to. Moving one by hand is temporary: the rotation loop re-places it if it disagrees."
                        >
                            {(statsQ.isLoading || statsQ.data) && (
                                <StatGrid className="mb-4">
                                    <Stat label="Sent today" value={statsQ.data?.emails_sent_today.toLocaleString()} loading={statsQ.isLoading} />
                                    <Stat label="Sent this week" value={statsQ.data?.emails_sent_this_week.toLocaleString()} loading={statsQ.isLoading} />
                                    <Stat label="Sent total" value={statsQ.data?.total_emails_sent.toLocaleString()} loading={statsQ.isLoading} />
                                    {/* Already a percentage in SQL; multiplying again gives 10000%. */}
                                    <Stat
                                        label="Success rate"
                                        value={statsQ.data ? `${Math.round(statsQ.data.success_rate)}%` : undefined}
                                        loading={statsQ.isLoading}
                                    />
                                </StatGrid>
                            )}

                            <Panel
                                title={
                                    <span className="flex items-center gap-2">
                                        Mailboxes
                                        {mailboxTotal > 0 && (
                                            <span className="text-xs font-normal text-muted-foreground tabular-nums">
                                                {mailboxes.length} of {mailboxTotal}
                                            </span>
                                        )}
                                    </span>
                                }
                                actions={
                                    selected.size > 0 ? (
                                        <Button size="xs" onClick={() => setReassignOpen(true)}>
                                            <ArrowRightLeft className="size-3" />
                                            Move {selected.size} elsewhere
                                        </Button>
                                    ) : undefined
                                }
                                bodyClassName="p-0"
                            >
                                {emailsQ.isLoading ? (
                                    <div className="space-y-2 p-4">
                                        <Skeleton className="h-5 w-full" />
                                        <Skeleton className="h-5 w-4/5" />
                                        <Skeleton className="h-5 w-3/5" />
                                    </div>
                                ) : mailboxes.length === 0 ? (
                                    <EmptyState icon={Inbox} title="No mailboxes on this worker yet." className="py-10" />
                                ) : (
                                    <div>
                                        <div className="flex h-9 items-center gap-3 border-b border-border px-4 text-xs font-medium text-muted-foreground">
                                            <span className="size-4 shrink-0" aria-hidden />
                                            Mailbox
                                            <span className="ml-auto">Provider</span>
                                        </div>
                                        {mailboxes.map((m: AdminWorkerEmail) => (
                                            <label
                                                key={m.id}
                                                className={cn(
                                                    "flex h-10 cursor-pointer items-center gap-3 border-b border-border/70 px-4 text-[13px] transition-colors last:border-b-0 hover:bg-accent/50",
                                                    selected.has(m.id) && "bg-[var(--admin-accent-weak)]",
                                                )}
                                            >
                                                <Checkbox
                                                    checked={selected.has(m.id)}
                                                    onCheckedChange={(v) => {
                                                        const next = new Set(selected);
                                                        if (v) next.add(m.id);
                                                        else next.delete(m.id);
                                                        setSelected(next);
                                                    }}
                                                />
                                                <span className="min-w-0 truncate font-mono text-[12.5px]">{m.email}</span>
                                                <StatusBadge className="ml-auto">{m.provider}</StatusBadge>
                                            </label>
                                        ))}
                                    </div>
                                )}

                                {emailsQ.hasNextPage && (
                                    <div className="border-t border-border px-4 py-2">
                                        <Button
                                            size="xs"
                                            variant="ghost"
                                            disabled={emailsQ.isFetchingNextPage}
                                            onClick={() => emailsQ.fetchNextPage()}
                                        >
                                            {emailsQ.isFetchingNextPage ? "Loading…" : "Load more mailboxes"}
                                        </Button>
                                    </div>
                                )}
                            </Panel>
                        </Section>
                    )}

                    <NodeDiagnostics node={node} />

                    <Section
                        title="Version"
                        description="The node pulls whatever the fleet is set to. Pin it to hold this one machine back, or to canary a release on it before the rest follow."
                    >
                        <div className="flex flex-wrap items-center gap-2">
                            <Input
                                value={pinDraft}
                                onChange={(e) => setPinDraft(e.target.value)}
                                placeholder={node.pinned_version || "v1.4.2"}
                                className="w-48 font-mono text-[12.5px]"
                                aria-label="Version to pin"
                            />
                            <Button
                                size="sm"
                                disabled={!pinDraft.trim() || patch.isPending}
                                onClick={() => {
                                    patch.mutate({ pinned_version: pinDraft.trim() });
                                    setPinDraft("");
                                }}
                            >
                                <Pin className="size-3.5" />
                                Pin
                            </Button>
                            {node.pinned_version && (
                                <Button
                                    size="sm"
                                    variant="outline"
                                    disabled={patch.isPending}
                                    onClick={() => patch.mutate({ pinned_version: "" })}
                                >
                                    <PinOff className="size-3.5" />
                                    Clear pin ({node.pinned_version})
                                </Button>
                            )}
                        </div>
                    </Section>

                    <Section title="Danger zone">
                        <div
                            className={cn(
                                "flex flex-col gap-3 rounded-lg border px-4 py-3.5 sm:flex-row sm:items-center sm:justify-between",
                                TONE_PANEL.danger,
                            )}
                        >
                            <div className="min-w-0">
                                <div className="text-[13px] font-medium text-foreground">Remove from fleet</div>
                                <p className="mt-0.5 max-w-xl text-[12.5px] leading-relaxed text-muted-foreground">
                                    Forgets the node. Any mailboxes it carries are re-placed within a few
                                    minutes. It does not stop the process: a machine that is still running
                                    re-joins on its next heartbeat, so stop the service there too.
                                </p>
                            </div>
                            <Button
                                size="sm"
                                variant="destructive"
                                className="shrink-0 self-start sm:self-center"
                                disabled={remove.isPending}
                                onClick={() => remove.mutate()}
                            >
                                <Trash2 className="size-3.5" />
                                {remove.isPending ? "Removing…" : "Remove from fleet"}
                            </Button>
                        </div>
                    </Section>
                </div>

                <aside className="order-first min-w-0 lg:order-none lg:border-l lg:border-border lg:pl-6">
                    <div className="lg:sticky lg:top-16">
                        <h2 className="text-[13px] font-semibold text-foreground">Properties</h2>
                        <p className="mt-0.5 text-xs text-muted-foreground">Reported by the node on its last heartbeat.</p>
                        <PropertyList className="mt-2">
                            <Property label="State">
                                <NodeStatePill state={state} />
                            </Property>
                            <Property label="Version">
                                {nodeNeedsUpdate(node) ? (
                                    <Mono>
                                        {node.version || "—"}
                                        <span className="text-subtle-foreground"> → </span>
                                        <span className={TONE_TEXT.warning}>{node.desired_version}</span>
                                    </Mono>
                                ) : (
                                    <Mono>{node.version || "—"}</Mono>
                                )}
                            </Property>
                            {node.pinned_version && (
                                <Property label="Pinned">
                                    <Mono>{node.pinned_version}</Mono>
                                </Property>
                            )}
                            <Property label="Public IPv4">
                                <Mono>{node.address || "—"}</Mono>
                            </Property>
                            <Property label="Region">
                                <Mono>{node.region || "—"}</Mono>
                            </Property>
                            {node.role === "worker" && (
                                <Property label="Mailbox target">
                                    <span className="tabular-nums">{(node.capacity_target || 100).toLocaleString()}</span>
                                </Property>
                            )}
                            <Property label="CPU">
                                <ResourceUsage usage={node.usage} kind="cpu" live={state === "live"} />
                            </Property>
                            <Property label="RAM">
                                <ResourceUsage usage={node.usage} kind="memory" live={state === "live"} />
                            </Property>
                            <Property label="Process RAM">
                                <ResourceUsage usage={node.usage} kind="resident" live={state === "live"} />
                            </Property>
                            <Property label="Goroutines">
                                <span className="tabular-nums">{node.usage?.goroutines ?? "—"}</span>
                            </Property>
                            <Property label="Uptime">
                                <span className="tabular-nums">{uptime(node.usage?.uptime_seconds)}</span>
                            </Property>
                            <Property label="Last seen">
                                {node.last_seen_at ? new Date(node.last_seen_at).toLocaleString() : "never"}
                            </Property>
                            <Property label="Enrolled">{new Date(node.enrolled_at).toLocaleString()}</Property>
                            <Property label="Node id">
                                <span className="break-all font-mono text-[11.5px] text-muted-foreground">{node.id}</span>
                            </Property>
                        </PropertyList>
                    </div>
                </aside>
            </div>

            <ReassignDialog
                open={reassignOpen}
                onOpenChange={setReassignOpen}
                source={node}
                mailboxIds={[...selected]}
                onDone={() => {
                    setSelected(new Set());
                    emailsQ.refetch();
                    nodeQ.refetch();
                }}
            />
        </div>
    );
}

function ReassignDialog({
    open,
    onOpenChange,
    source,
    mailboxIds,
    onDone,
}: {
    open: boolean;
    onOpenChange: (v: boolean) => void;
    source: FleetNode;
    mailboxIds: string[];
    onDone: () => void;
}) {
    const [target, setTarget] = useState("");

    const workersQ = useQuery({
        queryKey: ["admin", "fleet", "nodes", "worker"],
        queryFn: () => listFleetNodes("worker"),
        enabled: open,
        staleTime: 30_000,
    });
    const candidates = (workersQ.data?.data ?? []).filter((x) => x.id !== source.id);
    const chosen = candidates.find((x) => x.id === target) ?? null;

    const mutation = useMutation({
        mutationFn: () => reassignWorkerEmails(target, mailboxIds),
        onSuccess: () => {
            toast.success(
                `${mailboxIds.length} mailbox${mailboxIds.length === 1 ? "" : "es"} moved`,
            );
            setTarget("");
            onDone();
            onOpenChange(false);
        },
        onError: (e: Error) => toast.error(e.message || "Reassign failed"),
    });

    return (
        <Dialog open={open} onOpenChange={onOpenChange}>
            <DialogContent>
                <DialogHeader>
                    <DialogTitle>
                        Move {mailboxIds.length} mailbox{mailboxIds.length === 1 ? "" : "es"}
                    </DialogTitle>
                    <DialogDescription>
                        Sending and sync continue from the target on its next heartbeat. Any
                        worker can host any mailbox, so this is only worth doing when you know
                        something placement does not.
                    </DialogDescription>
                </DialogHeader>

                <Select value={target || undefined} onValueChange={setTarget}>
                    <SelectTrigger className="h-8 w-full text-[13px]">
                        <SelectValue
                            placeholder={workersQ.isLoading ? "Loading…" : "Pick a worker"}
                        />
                    </SelectTrigger>
                    <SelectContent>
                        {candidates.length === 0 && (
                            <div className="px-2 py-1.5 text-xs text-muted-foreground">
                                No other workers.
                            </div>
                        )}
                        {candidates.map((x) => (
                            <SelectItem key={x.id} value={x.id} className="text-[12.5px]">
                                {x.name || x.id.slice(0, 8)}
                                {x.region ? ` · ${x.region}` : ""} · {nodeState(x)} ·{" "}
                                {x.mailbox_count ?? 0} mailbox
                                {(x.mailbox_count ?? 0) === 1 ? "" : "es"}
                            </SelectItem>
                        ))}
                    </SelectContent>
                </Select>

                {chosen && nodeState(chosen) !== "live" && (
                    <Callout tone="warning" icon={AlertTriangle}>
                        That node is {nodeState(chosen)}; placement would not choose it, and the
                        rotation loop will move these mailboxes off it again.
                    </Callout>
                )}

                <DialogFooter>
                    <Button variant="outline" onClick={() => onOpenChange(false)}>
                        Cancel
                    </Button>
                    <Button
                        onClick={() => mutation.mutate()}
                        disabled={!chosen || mailboxIds.length === 0 || mutation.isPending}
                    >
                        {mutation.isPending ? "Moving…" : "Move"}
                    </Button>
                </DialogFooter>
            </DialogContent>
        </Dialog>
    );
}
