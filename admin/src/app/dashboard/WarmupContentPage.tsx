// Warmup Content admin — control + visibility surface for the OFFLINE AI
// warmup-content generator. Five tabs:
//
//   Overview  — headline counts, AI/schedule status, per-pool breakdown
//   Library   — filterable, paged table of generated conversation threads,
//               with a detail dialog + archive/unarchive/delete actions
//   Generate  — enqueue an offline generation job (runs in the background)
//   Jobs      — paged table of generation jobs, polled live so running jobs
//               update without a manual refresh
//   Settings  — full editor for WarmupGenerationSettings
//
// The generator never runs inline: "Generate" returns a job id and the Jobs
// tab is where progress is observed. Everything mirrors the existing admin
// design language (cards, DataTable, dialogs, var(--admin-accent) styling).

import { useEffect, useMemo, useState } from "react";
import {
    keepPreviousData,
    useMutation,
    useQuery,
    useQueryClient,
} from "@tanstack/react-query";
import { toast } from "sonner";
import {
    Archive,
    ArchiveRestore,
    CalendarClock,
    FlaskConical,
    Inbox,
    Layers,
    Play,
    Sparkles,
    Trash2,
} from "lucide-react";
import { PageHeader } from "@/components/layout/PageHeader";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Switch } from "@/components/ui/switch";
import { Skeleton } from "@/components/ui/skeleton";
import { Textarea } from "@/components/ui/textarea";
import { ErrorState } from "@/components/ErrorState";
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
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { DataTable, type Column } from "@/components/data/DataTable";
import { useCursorPager } from "@/lib/useCursorPager";
import {
    archiveWarmupConversation,
    deleteWarmupConversation,
    generateWarmupContent,
    getWarmupContentAb,
    getWarmupContentOverview,
    getWarmupConversation,
    getWarmupGenerationSettings,
    listWarmupConversations,
    listWarmupGenerationJobs,
    unarchiveWarmupConversation,
    updateWarmupGenerationSettings,
    type WarmupConversationRow,
    type WarmupGenerationJob,
    type WarmupGenerationPoolConfig,
    type WarmupGenerationSettings,
} from "@/lib/api/client/admin/warmupContent";

const POOL_OPTIONS = [
    { value: "premium", label: "Premium" },
    { value: "free", label: "Free" },
];

// --------------------------------------------------------------------
// Status tone helpers
// --------------------------------------------------------------------

const JOB_STATUS_TONE: Record<string, string> = {
    pending: "border-amber-300 bg-amber-50 text-amber-700",
    queued: "border-amber-300 bg-amber-50 text-amber-700",
    running: "border-amber-300 bg-amber-50 text-amber-700",
    completed: "border-emerald-300 bg-emerald-50 text-emerald-700",
    succeeded: "border-emerald-300 bg-emerald-50 text-emerald-700",
    failed: "border-red-300 bg-red-50 text-red-700",
    error: "border-red-300 bg-red-50 text-red-700",
};

function jobTone(status: string): string {
    return JOB_STATUS_TONE[status] ?? "border-zinc-300 bg-zinc-50 text-zinc-600";
}

const CONTENT_STATUS_TONE: Record<string, string> = {
    active: "border-emerald-300 bg-emerald-50 text-emerald-700",
    archived: "border-zinc-300 bg-zinc-50 text-zinc-600",
    draft: "border-amber-300 bg-amber-50 text-amber-700",
};

function fmtDate(s: string | null | undefined): string {
    if (!s) return "—";
    return new Date(s).toLocaleString();
}

function poolBadge(pool: string) {
    return (
        <Badge
            variant="outline"
            className={`text-[10px] ${
                pool === "premium"
                    ? "border-purple-300 bg-purple-50 text-purple-700"
                    : "border-zinc-300 text-zinc-700"
            }`}
        >
            {pool}
        </Badge>
    );
}

// ====================================================================
// Page
// ====================================================================

export default function WarmupContentPage() {
    const [tab, setTab] = useState("overview");

    return (
        <div>
            <PageHeader
                title="Warmup Content"
                description="Control and observe the offline AI warmup-content generator: review the generated thread library, enqueue background generation, watch jobs, and tune generation + engagement settings."
            />

            <Tabs value={tab} onValueChange={setTab}>
                <TabsList variant="line">
                    <TabsTrigger value="overview">
                        <Layers className="size-4" /> Overview
                    </TabsTrigger>
                    <TabsTrigger value="library">
                        <Inbox className="size-4" /> Library
                    </TabsTrigger>
                    <TabsTrigger value="generate">
                        <Sparkles className="size-4" /> Generate
                    </TabsTrigger>
                    <TabsTrigger value="jobs">
                        <Play className="size-4" /> Jobs
                    </TabsTrigger>
                    <TabsTrigger value="settings">
                        <CalendarClock className="size-4" /> Settings
                    </TabsTrigger>
                </TabsList>

                <TabsContent value="overview" className="mt-5">
                    <OverviewTab />
                </TabsContent>
                <TabsContent value="library" className="mt-5">
                    <LibraryTab />
                </TabsContent>
                <TabsContent value="generate" className="mt-5">
                    <GenerateTab onQueued={() => setTab("jobs")} />
                </TabsContent>
                <TabsContent value="jobs" className="mt-5">
                    <JobsTab />
                </TabsContent>
                <TabsContent value="settings" className="mt-5">
                    <SettingsTab />
                </TabsContent>
            </Tabs>
        </div>
    );
}

// ====================================================================
// Overview tab
// ====================================================================

function StatCard({
    icon,
    title,
    value,
    hint,
    tone,
}: {
    icon: React.ReactNode;
    title: string;
    value: string;
    hint?: string;
    tone?: string;
}) {
    return (
        <div className="rounded-lg border border-border bg-card p-3">
            <div className="flex items-center gap-1.5 text-xs text-muted-foreground">
                {icon}
                <span>{title}</span>
            </div>
            <div className={`mt-1 text-2xl font-semibold tabular-nums ${tone ?? ""}`}>
                {value}
            </div>
            {hint && (
                <div className="mt-0.5 truncate text-[10px] text-muted-foreground">
                    {hint}
                </div>
            )}
        </div>
    );
}

function OverviewTab() {
    const { data, isLoading, error, refetch } = useQuery({
        queryKey: ["admin", "warmup-content", "overview"],
        queryFn: getWarmupContentOverview,
        refetchInterval: 30_000,
    });

    const ab = useQuery({
        queryKey: ["admin", "warmup-content", "ab", 14],
        queryFn: () => getWarmupContentAb(14),
        staleTime: 60_000,
    });

    if (isLoading) {
        return (
            <div className="grid gap-3 md:grid-cols-5">
                {Array.from({ length: 5 }).map((_, i) => (
                    <Skeleton key={i} className="h-24" />
                ))}
            </div>
        );
    }
    if (error) {
        return (
            <ErrorState
                error={error}
                title="Failed to load overview"
                onRetry={() => refetch()}
            />
        );
    }
    if (!data) return null;

    const byPool = data.by_pool ?? [];

    return (
        <div className="space-y-6">
            <div className="grid gap-3 md:grid-cols-5">
                <StatCard
                    icon={<Inbox className="size-4" />}
                    title="Active threads"
                    value={(data.total_active ?? 0).toLocaleString()}
                    hint="available to warmup sends"
                />
                <StatCard
                    icon={<Archive className="size-4" />}
                    title="Archived"
                    value={(data.total_archived ?? 0).toLocaleString()}
                    hint="retired from rotation"
                />
                <StatCard
                    icon={<Sparkles className="size-4" />}
                    title="AI generation"
                    value={data.ai_enabled ? "Enabled" : "Disabled"}
                    tone={data.ai_enabled ? "text-emerald-600" : "text-muted-foreground"}
                    hint="master generation toggle"
                />
                <StatCard
                    icon={<CalendarClock className="size-4" />}
                    title="Schedule"
                    value={data.schedule_enabled ? "On" : "Off"}
                    tone={
                        data.schedule_enabled ? "text-emerald-600" : "text-muted-foreground"
                    }
                    hint="automatic top-up jobs"
                />
                <StatCard
                    icon={<Play className="size-4" />}
                    title="Last generated"
                    value={
                        data.last_generated_at
                            ? new Date(data.last_generated_at).toLocaleDateString()
                            : "Never"
                    }
                    hint={data.last_generated_at ? fmtDate(data.last_generated_at) : "no jobs yet"}
                />
            </div>

            <section>
                <h2 className="mb-2 text-sm font-semibold">Library by pool</h2>
                <div className="overflow-hidden rounded-lg border border-border bg-card">
                    <table className="w-full text-sm">
                        <thead className="bg-muted/50 text-xs uppercase text-muted-foreground">
                            <tr>
                                <th className="px-3 py-2 text-left font-medium">Pool</th>
                                <th className="px-3 py-2 text-left font-medium">Segment</th>
                                <th className="px-3 py-2 text-left font-medium">Source</th>
                                <th className="px-3 py-2 text-right font-medium">Active</th>
                                <th className="px-3 py-2 text-right font-medium">Archived</th>
                            </tr>
                        </thead>
                        <tbody>
                            {byPool.map((p, i) => (
                                <tr
                                    key={`${p.pool_type}-${p.segment}-${p.source}-${i}`}
                                    className="border-t border-border"
                                >
                                    <td className="px-3 py-2">{poolBadge(p.pool_type)}</td>
                                    <td className="px-3 py-2 text-xs">{p.segment || "—"}</td>
                                    <td className="px-3 py-2 text-xs text-muted-foreground">
                                        {p.source || "—"}
                                    </td>
                                    <td className="px-3 py-2 text-right tabular-nums text-emerald-600">
                                        {p.active.toLocaleString()}
                                    </td>
                                    <td className="px-3 py-2 text-right tabular-nums text-muted-foreground">
                                        {p.archived.toLocaleString()}
                                    </td>
                                </tr>
                            ))}
                            {byPool.length === 0 && (
                                <tr>
                                    <td
                                        colSpan={5}
                                        className="py-6 text-center text-sm text-muted-foreground"
                                    >
                                        No content generated yet.
                                    </td>
                                </tr>
                            )}
                        </tbody>
                    </table>
                </div>
            </section>

            <section>
                <h2 className="mb-2 text-sm font-semibold">
                    Content source vs spam placement
                    {ab.data ? (
                        <span className="ml-2 text-xs font-normal text-muted-foreground">
                            last {ab.data.window_days} days
                        </span>
                    ) : null}
                </h2>
                {ab.error ? (
                    <ErrorState
                        error={ab.error}
                        title="Failed to load A/B comparison"
                        onRetry={() => ab.refetch()}
                    />
                ) : ab.isLoading ? (
                    <Skeleton className="h-24" />
                ) : (
                    <div className="overflow-hidden rounded-lg border border-border bg-card">
                        <table className="w-full text-sm">
                            <thead className="bg-muted/50 text-xs uppercase text-muted-foreground">
                                <tr>
                                    <th className="px-3 py-2 text-left font-medium">Source</th>
                                    <th className="px-3 py-2 text-right font-medium">Sent</th>
                                    <th className="px-3 py-2 text-right font-medium">
                                        Spam placements
                                    </th>
                                    <th className="px-3 py-2 text-right font-medium">
                                        Placement rate
                                    </th>
                                </tr>
                            </thead>
                            <tbody>
                                {(ab.data?.data ?? []).map((r) => {
                                    const pct = (r.spam_placement_rate ?? 0) * 100;
                                    const tone =
                                        pct >= 20
                                            ? "text-red-700"
                                            : pct >= 10
                                              ? "text-amber-700"
                                              : "text-emerald-600";
                                    return (
                                        <tr
                                            key={r.content_source}
                                            className="border-t border-border"
                                        >
                                            <td className="px-3 py-2 text-xs">
                                                {r.content_source}
                                            </td>
                                            <td className="px-3 py-2 text-right tabular-nums">
                                                {r.sent.toLocaleString()}
                                            </td>
                                            <td className="px-3 py-2 text-right tabular-nums">
                                                {r.spam_placements.toLocaleString()}
                                            </td>
                                            <td
                                                className={`px-3 py-2 text-right tabular-nums ${tone}`}
                                            >
                                                {pct.toFixed(2)}%
                                            </td>
                                        </tr>
                                    );
                                })}
                                {(ab.data?.data ?? []).length === 0 && (
                                    <tr>
                                        <td
                                            colSpan={4}
                                            className="py-6 text-center text-sm text-muted-foreground"
                                        >
                                            Not enough delivery data yet.
                                        </td>
                                    </tr>
                                )}
                            </tbody>
                        </table>
                    </div>
                )}
            </section>
        </div>
    );
}

// ====================================================================
// Library tab
// ====================================================================

function LibraryTab() {
    const qc = useQueryClient();
    const [pool, setPool] = useState("");
    const [source, setSource] = useState("");
    const [status, setStatus] = useState("");
    const [openId, setOpenId] = useState<string | null>(null);
    const [confirmDelete, setConfirmDelete] = useState<WarmupConversationRow | null>(
        null,
    );
    const pager = useCursorPager();
    const { reset } = pager;

    const filterKey = JSON.stringify({ pool, source, status });
    useEffect(() => {
        reset();
    }, [filterKey, reset]);

    const { data, isLoading, error, refetch } = useQuery({
        queryKey: ["admin", "warmup-content", "conversations", filterKey, pager.cursor],
        queryFn: () =>
            listWarmupConversations({
                pool: pool || undefined,
                source: source || undefined,
                status: status || undefined,
                cursor: pager.cursor,
                limit: 50,
            }),
        staleTime: 30_000,
        placeholderData: keepPreviousData,
    });

    const archive = useMutation({
        mutationFn: (id: string) => archiveWarmupConversation(id),
        onSuccess: () => {
            toast.success("Conversation archived");
            qc.invalidateQueries({ queryKey: ["admin", "warmup-content"] });
        },
        onError: (err: Error) => toast.error(err.message || "Failed to archive"),
    });
    const unarchive = useMutation({
        mutationFn: (id: string) => unarchiveWarmupConversation(id),
        onSuccess: () => {
            toast.success("Conversation restored");
            qc.invalidateQueries({ queryKey: ["admin", "warmup-content"] });
        },
        onError: (err: Error) => toast.error(err.message || "Failed to restore"),
    });
    const remove = useMutation({
        mutationFn: (id: string) => deleteWarmupConversation(id),
        onSuccess: () => {
            toast.success("Conversation deleted");
            setConfirmDelete(null);
            qc.invalidateQueries({ queryKey: ["admin", "warmup-content"] });
        },
        onError: (err: Error) => toast.error(err.message || "Failed to delete"),
    });

    const columns: Column<WarmupConversationRow>[] = useMemo(
        () => [
            {
                id: "subject",
                header: "Thread",
                cell: (c) => (
                    <div className="min-w-0">
                        <div className="truncate font-medium">{c.subject || "(no subject)"}</div>
                        <div className="truncate text-[11px] text-muted-foreground">
                            {c.theme || c.description || "—"}
                        </div>
                    </div>
                ),
                csv: (c) => c.subject,
            },
            {
                id: "pool",
                header: "Pool",
                cell: (c) => poolBadge(c.pool_type),
                csv: (c) => c.pool_type,
            },
            {
                id: "segment",
                header: "Segment",
                cell: (c) => <span className="text-xs">{c.segment || "—"}</span>,
                csv: (c) => c.segment,
            },
            {
                id: "source",
                header: "Source",
                cell: (c) => (
                    <span className="text-xs text-muted-foreground">{c.source || "—"}</span>
                ),
                csv: (c) => c.source,
            },
            {
                id: "messages",
                header: "Msgs",
                align: "right",
                cell: (c) => <span className="tabular-nums">{c.message_count}</span>,
                csv: (c) => c.message_count,
            },
            {
                id: "usage",
                header: "Used",
                align: "right",
                cell: (c) => <span className="tabular-nums">{c.usage_count}</span>,
                csv: (c) => c.usage_count,
            },
            {
                id: "lint",
                header: "Lint",
                cell: (c) =>
                    c.lint_passed ? (
                        <Badge
                            variant="outline"
                            className="text-[10px] border-emerald-300 bg-emerald-50 text-emerald-700"
                        >
                            pass
                        </Badge>
                    ) : (
                        <Badge
                            variant="outline"
                            className="text-[10px] border-red-300 bg-red-50 text-red-700"
                        >
                            fail
                        </Badge>
                    ),
                csv: (c) => (c.lint_passed ? "pass" : "fail"),
            },
            {
                id: "status",
                header: "Status",
                cell: (c) => (
                    <Badge
                        variant="outline"
                        className={`text-[10px] ${
                            CONTENT_STATUS_TONE[c.status] ?? "border-zinc-300 text-zinc-600"
                        }`}
                    >
                        {c.status}
                    </Badge>
                ),
                csv: (c) => c.status,
            },
            {
                id: "created",
                header: "Created",
                cell: (c) => (
                    <span className="text-xs text-muted-foreground">
                        {new Date(c.created_at).toLocaleDateString()}
                    </span>
                ),
                csv: (c) => c.created_at,
                defaultHidden: true,
            },
            {
                id: "actions",
                header: "Actions",
                align: "right",
                cell: (c) => (
                    <div
                        className="flex items-center justify-end gap-1.5"
                        onClick={(e) => e.stopPropagation()}
                    >
                        {c.status === "archived" ? (
                            <Button
                                size="xs"
                                variant="outline"
                                onClick={() => unarchive.mutate(c.id)}
                                disabled={unarchive.isPending}
                            >
                                <ArchiveRestore className="size-3" /> Restore
                            </Button>
                        ) : (
                            <Button
                                size="xs"
                                variant="outline"
                                onClick={() => archive.mutate(c.id)}
                                disabled={archive.isPending}
                            >
                                <Archive className="size-3" /> Archive
                            </Button>
                        )}
                        <Button
                            size="xs"
                            variant="outline"
                            className="text-red-700 hover:bg-red-50"
                            onClick={() => setConfirmDelete(c)}
                        >
                            <Trash2 className="size-3" /> Delete
                        </Button>
                    </div>
                ),
            },
        ],
        [archive, unarchive],
    );

    const rows = data?.data ?? [];

    return (
        <div className="space-y-4">
            <div className="flex flex-wrap items-end gap-3">
                <div className="w-40">
                    <Label className="mb-1 text-xs text-muted-foreground">Pool</Label>
                    <Select
                        value={pool || "any"}
                        onValueChange={(v) => setPool(v === "any" ? "" : v)}
                    >
                        <SelectTrigger size="sm">
                            <SelectValue placeholder="Any pool" />
                        </SelectTrigger>
                        <SelectContent>
                            <SelectItem value="any">Any pool</SelectItem>
                            {POOL_OPTIONS.map((o) => (
                                <SelectItem key={o.value} value={o.value}>
                                    {o.label}
                                </SelectItem>
                            ))}
                        </SelectContent>
                    </Select>
                </div>
                <div className="w-44">
                    <Label className="mb-1 text-xs text-muted-foreground">Source</Label>
                    <Select
                        value={source || "any"}
                        onValueChange={(v) => setSource(v === "any" ? "" : v)}
                    >
                        <SelectTrigger size="sm">
                            <SelectValue placeholder="Any source" />
                        </SelectTrigger>
                        <SelectContent>
                            <SelectItem value="any">Any source</SelectItem>
                            <SelectItem value="ai">AI generated</SelectItem>
                            <SelectItem value="curated">Curated</SelectItem>
                            <SelectItem value="imported">Imported</SelectItem>
                        </SelectContent>
                    </Select>
                </div>
                <div className="w-40">
                    <Label className="mb-1 text-xs text-muted-foreground">Status</Label>
                    <Select
                        value={status || "any"}
                        onValueChange={(v) => setStatus(v === "any" ? "" : v)}
                    >
                        <SelectTrigger size="sm">
                            <SelectValue placeholder="Any status" />
                        </SelectTrigger>
                        <SelectContent>
                            <SelectItem value="any">Any status</SelectItem>
                            <SelectItem value="active">Active</SelectItem>
                            <SelectItem value="archived">Archived</SelectItem>
                            <SelectItem value="draft">Draft</SelectItem>
                        </SelectContent>
                    </Select>
                </div>
            </div>

            <DataTable
                columns={columns}
                rows={rows}
                getRowId={(c) => c.id}
                loading={isLoading}
                error={error}
                onRetry={() => refetch()}
                onRowClick={(c) => setOpenId(c.id)}
                errorTitle="Failed to load conversations"
                storageKey="admin.warmup-content.library"
                csvName="warmbly-warmup-content"
                noun="conversations"
                emptyTitle="No conversations"
                emptyHint="No warmup content matches these filters."
                pager={{
                    canPrev: pager.canPrev,
                    canNext: !!data?.pagination.has_more,
                    onPrev: pager.prev,
                    onNext: () => pager.next(data?.pagination.next_cursor),
                    page: pager.page,
                    shown: rows.length,
                    total: data?.pagination.total ?? null,
                }}
            />

            {openId && (
                <ConversationDialog
                    id={openId}
                    open
                    onOpenChange={(v) => !v && setOpenId(null)}
                />
            )}

            <Dialog
                open={!!confirmDelete}
                onOpenChange={(v) => !v && setConfirmDelete(null)}
            >
                <DialogContent>
                    <DialogHeader>
                        <DialogTitle>Delete conversation</DialogTitle>
                        <DialogDescription>
                            This permanently removes the thread{" "}
                            <span className="font-medium">
                                “{confirmDelete?.subject || "(no subject)"}”
                            </span>{" "}
                            from the library. This cannot be undone — archive instead if you
                            only want it out of rotation.
                        </DialogDescription>
                    </DialogHeader>
                    <DialogFooter>
                        <Button variant="outline" onClick={() => setConfirmDelete(null)}>
                            Cancel
                        </Button>
                        <Button
                            variant="destructive"
                            disabled={remove.isPending}
                            onClick={() => confirmDelete && remove.mutate(confirmDelete.id)}
                        >
                            {remove.isPending ? "Deleting…" : "Delete"}
                        </Button>
                    </DialogFooter>
                </DialogContent>
            </Dialog>
        </div>
    );
}

function ConversationDialog({
    id,
    open,
    onOpenChange,
}: {
    id: string;
    open: boolean;
    onOpenChange: (v: boolean) => void;
}) {
    const { data, isLoading, error, refetch } = useQuery({
        queryKey: ["admin", "warmup-content", "conversation", id],
        queryFn: () => getWarmupConversation(id),
    });
    const c = data?.data;

    return (
        <Dialog open={open} onOpenChange={onOpenChange}>
            <DialogContent className="sm:max-w-2xl">
                <DialogHeader>
                    <DialogTitle className="truncate pr-6">
                        {c?.subject || (isLoading ? "Loading…" : "Conversation")}
                    </DialogTitle>
                    <DialogDescription>
                        {c?.description || "Full generated warmup thread."}
                    </DialogDescription>
                </DialogHeader>

                {error ? (
                    <ErrorState
                        error={error}
                        title="Failed to load conversation"
                        onRetry={() => refetch()}
                    />
                ) : isLoading || !c ? (
                    <div className="space-y-2">
                        <Skeleton className="h-5 w-1/2" />
                        <Skeleton className="h-16" />
                        <Skeleton className="h-16" />
                    </div>
                ) : (
                    <div className="space-y-4">
                        <div className="flex flex-wrap items-center gap-2 text-xs">
                            {poolBadge(c.pool_type)}
                            {c.segment && (
                                <Badge variant="outline" className="text-[10px]">
                                    segment: {c.segment}
                                </Badge>
                            )}
                            <Badge variant="outline" className="text-[10px]">
                                source: {c.source || "—"}
                            </Badge>
                            <Badge
                                variant="outline"
                                className={`text-[10px] ${
                                    CONTENT_STATUS_TONE[c.status] ??
                                    "border-zinc-300 text-zinc-600"
                                }`}
                            >
                                {c.status}
                            </Badge>
                            <Badge
                                variant="outline"
                                className={`text-[10px] ${
                                    c.lint_passed
                                        ? "border-emerald-300 bg-emerald-50 text-emerald-700"
                                        : "border-red-300 bg-red-50 text-red-700"
                                }`}
                            >
                                lint {c.lint_passed ? "pass" : "fail"}
                            </Badge>
                            <span className="text-muted-foreground">
                                used {c.usage_count}×
                            </span>
                        </div>

                        <div className="max-h-[50vh] space-y-2 overflow-y-auto rounded-lg border border-border bg-muted/30 p-3">
                            {(c.messages ?? []).map((m, i) => (
                                <div
                                    key={i}
                                    className="rounded-md border border-border bg-card p-2.5 text-[13px] leading-relaxed whitespace-pre-wrap"
                                >
                                    <div className="mb-1 text-[10px] font-semibold uppercase tracking-wider text-muted-foreground">
                                        Message {i + 1}
                                    </div>
                                    {m}
                                </div>
                            ))}
                            {(c.messages ?? []).length === 0 && (
                                <div className="py-4 text-center text-sm text-muted-foreground">
                                    No messages in this thread.
                                </div>
                            )}
                        </div>

                        <div className="grid grid-cols-2 gap-2 text-[11px] text-muted-foreground">
                            <div>
                                Generated by job:{" "}
                                <span className="font-mono">
                                    {c.generated_by_job_id ?? "—"}
                                </span>
                            </div>
                            <div>Created: {fmtDate(c.created_at)}</div>
                            <div>Updated: {fmtDate(c.updated_at)}</div>
                        </div>
                    </div>
                )}

                <DialogFooter showCloseButton />
            </DialogContent>
        </Dialog>
    );
}

// ====================================================================
// Generate tab
// ====================================================================

function GenerateTab({ onQueued }: { onQueued: () => void }) {
    const qc = useQueryClient();
    const [count, setCount] = useState(10);
    const [poolType, setPoolType] = useState("premium");
    const [segment, setSegment] = useState("");
    const [theme, setTheme] = useState("");
    const [model, setModel] = useState("");

    const generate = useMutation({
        mutationFn: () =>
            generateWarmupContent({
                count,
                pool_type: poolType,
                segment: segment.trim() || undefined,
                theme: theme.trim() || undefined,
                model: model.trim() || undefined,
            }),
        onSuccess: (res) => {
            toast.success("Generation job queued", {
                description: `Job ${res.job_id} is running offline. Watch the Jobs tab for progress.`,
            });
            qc.invalidateQueries({ queryKey: ["admin", "warmup-content", "jobs"] });
            onQueued();
        },
        onError: (err: Error) => toast.error(err.message || "Failed to queue job"),
    });

    return (
        <div className="max-w-xl space-y-5">
            <div className="rounded-lg border border-[var(--admin-accent)]/30 bg-[var(--admin-accent-soft)]/40 p-3 text-[12.5px] text-muted-foreground">
                <div className="flex items-center gap-2 font-medium text-foreground">
                    <Sparkles className="size-4 text-[var(--admin-accent-strong)]" />
                    Runs offline
                </div>
                <p className="mt-1">
                    Generation does not run inline. Submitting enqueues a background job
                    that produces threads, lint-checks them, and adds the passing ones to
                    the library. Track progress on the Jobs tab.
                </p>
            </div>

            <div className="space-y-4 rounded-lg border border-border bg-card p-4">
                <div>
                    <Label htmlFor="gen-count" className="mb-1 text-xs">
                        Count
                    </Label>
                    <Input
                        id="gen-count"
                        type="number"
                        min={1}
                        max={500}
                        value={count}
                        onChange={(e) =>
                            setCount(Math.max(1, Math.min(500, Number(e.target.value) || 0)))
                        }
                    />
                    <p className="mt-1 text-[11px] text-muted-foreground">
                        Number of conversation threads to generate.
                    </p>
                </div>

                <div>
                    <Label className="mb-1 text-xs">Pool</Label>
                    <Select value={poolType} onValueChange={setPoolType}>
                        <SelectTrigger>
                            <SelectValue />
                        </SelectTrigger>
                        <SelectContent>
                            {POOL_OPTIONS.map((o) => (
                                <SelectItem key={o.value} value={o.value}>
                                    {o.label}
                                </SelectItem>
                            ))}
                        </SelectContent>
                    </Select>
                    <p className="mt-1 text-[11px] text-muted-foreground">
                        Pool the generated threads will belong to. Keep free and premium
                        content separate.
                    </p>
                </div>

                <div>
                    <Label htmlFor="gen-segment" className="mb-1 text-xs">
                        Segment <span className="text-muted-foreground">(optional)</span>
                    </Label>
                    <Input
                        id="gen-segment"
                        placeholder="e.g. saas, agency, ecommerce"
                        value={segment}
                        onChange={(e) => setSegment(e.target.value)}
                    />
                </div>

                <div>
                    <Label htmlFor="gen-theme" className="mb-1 text-xs">
                        Theme <span className="text-muted-foreground">(optional)</span>
                    </Label>
                    <Textarea
                        id="gen-theme"
                        placeholder="Steer the topic, e.g. 'casual product follow-ups between colleagues'"
                        value={theme}
                        onChange={(e) => setTheme(e.target.value)}
                    />
                </div>

                <div>
                    <Label htmlFor="gen-model" className="mb-1 text-xs">
                        Model override{" "}
                        <span className="text-muted-foreground">(optional)</span>
                    </Label>
                    <Input
                        id="gen-model"
                        placeholder="Defaults to the configured model"
                        value={model}
                        onChange={(e) => setModel(e.target.value)}
                    />
                </div>

                <div className="flex justify-end">
                    <Button
                        onClick={() => generate.mutate()}
                        disabled={generate.isPending || count < 1}
                    >
                        <Sparkles className="size-4" />
                        {generate.isPending ? "Queuing…" : "Queue generation job"}
                    </Button>
                </div>
            </div>
        </div>
    );
}

// ====================================================================
// Jobs tab
// ====================================================================

function JobsTab() {
    const pager = useCursorPager();

    const { data, isLoading, error, refetch } = useQuery({
        queryKey: ["admin", "warmup-content", "jobs", pager.cursor],
        queryFn: () => listWarmupGenerationJobs({ cursor: pager.cursor, limit: 50 }),
        placeholderData: keepPreviousData,
        refetchInterval: 5_000,
    });

    const columns: Column<WarmupGenerationJob>[] = useMemo(
        () => [
            {
                id: "status",
                header: "Status",
                cell: (j) => (
                    <Badge variant="outline" className={`text-[10px] ${jobTone(j.status)}`}>
                        {j.status}
                    </Badge>
                ),
                csv: (j) => j.status,
            },
            {
                id: "pool",
                header: "Pool",
                cell: (j) => poolBadge(j.pool_type),
                csv: (j) => j.pool_type,
            },
            {
                id: "segment",
                header: "Segment",
                cell: (j) => <span className="text-xs">{j.segment || "—"}</span>,
                csv: (j) => j.segment,
            },
            {
                id: "trigger",
                header: "Trigger",
                cell: (j) => (
                    <span className="text-xs text-muted-foreground">{j.trigger || "—"}</span>
                ),
                csv: (j) => j.trigger,
            },
            {
                id: "model",
                header: "Model",
                cell: (j) => <span className="text-xs">{j.model || "—"}</span>,
                csv: (j) => j.model,
            },
            {
                id: "counts",
                header: "Generated / Requested",
                align: "right",
                cell: (j) => (
                    <span className="tabular-nums">
                        {j.generated_count} / {j.requested_count}
                    </span>
                ),
                csv: (j) => `${j.generated_count}/${j.requested_count}`,
            },
            {
                id: "rejected",
                header: "Lint rej.",
                align: "right",
                cell: (j) => (
                    <span
                        className={`tabular-nums ${
                            j.lint_rejected_count > 0 ? "text-amber-700" : "text-muted-foreground"
                        }`}
                    >
                        {j.lint_rejected_count}
                    </span>
                ),
                csv: (j) => j.lint_rejected_count,
            },
            {
                id: "failed",
                header: "Failed",
                align: "right",
                cell: (j) => (
                    <span
                        className={`tabular-nums ${
                            j.failed_count > 0 ? "text-red-700" : "text-muted-foreground"
                        }`}
                    >
                        {j.failed_count}
                    </span>
                ),
                csv: (j) => j.failed_count,
            },
            {
                id: "started",
                header: "Started",
                cell: (j) => (
                    <span className="text-xs text-muted-foreground">
                        {fmtDate(j.started_at)}
                    </span>
                ),
                csv: (j) => j.started_at ?? "",
            },
            {
                id: "finished",
                header: "Finished",
                cell: (j) => (
                    <span className="text-xs text-muted-foreground">
                        {fmtDate(j.finished_at)}
                    </span>
                ),
                csv: (j) => j.finished_at ?? "",
            },
            {
                id: "error",
                header: "Error",
                cell: (j) =>
                    j.error ? (
                        <span className="text-xs text-red-700" title={j.error}>
                            {j.error.length > 60 ? `${j.error.slice(0, 60)}…` : j.error}
                        </span>
                    ) : (
                        <span className="text-xs text-muted-foreground">—</span>
                    ),
                csv: (j) => j.error,
                defaultHidden: true,
            },
            {
                id: "created",
                header: "Created",
                cell: (j) => (
                    <span className="text-xs text-muted-foreground">
                        {new Date(j.created_at).toLocaleString()}
                    </span>
                ),
                csv: (j) => j.created_at,
                defaultHidden: true,
            },
        ],
        [],
    );

    const rows = data?.data ?? [];

    return (
        <DataTable
            columns={columns}
            rows={rows}
            getRowId={(j) => j.id}
            loading={isLoading}
            error={error}
            onRetry={() => refetch()}
            errorTitle="Failed to load jobs"
            storageKey="admin.warmup-content.jobs"
            csvName="warmbly-warmup-content-jobs"
            noun="jobs"
            emptyTitle="No generation jobs"
            emptyHint="Queue a job from the Generate tab to see it here."
            pager={{
                canPrev: pager.canPrev,
                canNext: !!data?.pagination.has_more,
                onPrev: pager.prev,
                onNext: () => pager.next(data?.pagination.next_cursor),
                page: pager.page,
                shown: rows.length,
                total: data?.pagination.total ?? null,
            }}
        />
    );
}

// ====================================================================
// Settings tab
// ====================================================================

function NumberField({
    id,
    label,
    hint,
    value,
    onChange,
    min,
    max,
    step,
}: {
    id: string;
    label: string;
    hint?: string;
    value: number;
    onChange: (v: number) => void;
    min?: number;
    max?: number;
    step?: number;
}) {
    return (
        <div>
            <Label htmlFor={id} className="mb-1 text-xs">
                {label}
            </Label>
            <Input
                id={id}
                type="number"
                min={min}
                max={max}
                step={step}
                value={value}
                onChange={(e) => onChange(Number(e.target.value))}
            />
            {hint && <p className="mt-1 text-[11px] text-muted-foreground">{hint}</p>}
        </div>
    );
}

function ToggleRow({
    label,
    hint,
    checked,
    onChange,
}: {
    label: string;
    hint?: string;
    checked: boolean;
    onChange: (v: boolean) => void;
}) {
    return (
        <label className="flex items-start justify-between gap-3 rounded-md border border-border bg-card px-3 py-2.5">
            <div className="min-w-0">
                <div className="text-sm font-medium">{label}</div>
                {hint && (
                    <div className="mt-0.5 text-[11px] text-muted-foreground">{hint}</div>
                )}
            </div>
            <Switch checked={checked} onCheckedChange={onChange} />
        </label>
    );
}

function SettingsTab() {
    const qc = useQueryClient();
    const { data, isLoading, error, refetch } = useQuery({
        queryKey: ["admin", "warmup-content", "settings"],
        queryFn: getWarmupGenerationSettings,
    });

    const [form, setForm] = useState<WarmupGenerationSettings | null>(null);

    useEffect(() => {
        if (data?.data) setForm(data.data);
    }, [data]);

    const save = useMutation({
        mutationFn: (body: WarmupGenerationSettings) =>
            updateWarmupGenerationSettings(body),
        onSuccess: () => {
            toast.success("Settings saved");
            qc.invalidateQueries({ queryKey: ["admin", "warmup-content"] });
        },
        onError: (err: Error) => toast.error(err.message || "Failed to save settings"),
    });

    if (isLoading || !form) {
        if (error) {
            return (
                <ErrorState
                    error={error}
                    title="Failed to load settings"
                    onRetry={() => refetch()}
                />
            );
        }
        return (
            <div className="max-w-3xl space-y-3">
                <Skeleton className="h-16" />
                <Skeleton className="h-40" />
                <Skeleton className="h-40" />
            </div>
        );
    }

    function patch(p: Partial<WarmupGenerationSettings>) {
        setForm((f) => (f ? { ...f, ...p } : f));
    }
    function patchEngagement(
        p: Partial<WarmupGenerationSettings["engagement"]>,
    ) {
        setForm((f) =>
            f ? { ...f, engagement: { ...f.engagement, ...p } } : f,
        );
    }
    function patchPool(index: number, p: Partial<WarmupGenerationPoolConfig>) {
        setForm((f) => {
            if (!f) return f;
            const pools = f.pools.map((pool, i) =>
                i === index ? { ...pool, ...p } : pool,
            );
            return { ...f, pools };
        });
    }

    return (
        <div className="max-w-3xl space-y-6">
            <section className="space-y-2">
                <h2 className="text-sm font-semibold">Master controls</h2>
                <ToggleRow
                    label="AI generation enabled"
                    hint="Master switch for the offline generator. When off, no new content is produced."
                    checked={form.enabled}
                    onChange={(v) => patch({ enabled: v })}
                />
                <ToggleRow
                    label="Scheduled generation"
                    hint="Automatically enqueue top-up jobs on a cadence to keep the library stocked."
                    checked={form.schedule_enabled}
                    onChange={(v) => patch({ schedule_enabled: v })}
                />
            </section>

            <section className="space-y-3">
                <h2 className="text-sm font-semibold">Generation</h2>
                <div className="grid gap-4 rounded-lg border border-border bg-card p-4 sm:grid-cols-2">
                    <NumberField
                        id="set-cadence"
                        label="Cadence (hours)"
                        hint="Interval between scheduled top-up jobs."
                        value={form.cadence_hours}
                        min={1}
                        onChange={(v) => patch({ cadence_hours: v })}
                    />
                    <div>
                        <Label htmlFor="set-model" className="mb-1 text-xs">
                            Model
                        </Label>
                        <Input
                            id="set-model"
                            value={form.model}
                            onChange={(e) => patch({ model: e.target.value })}
                        />
                        <p className="mt-1 text-[11px] text-muted-foreground">
                            Default model used for generation.
                        </p>
                    </div>
                    <NumberField
                        id="set-max-msgs"
                        label="Max messages / thread"
                        hint="Upper bound on messages in a generated conversation."
                        value={form.max_messages_per_thread}
                        min={1}
                        onChange={(v) => patch({ max_messages_per_thread: v })}
                    />
                    <NumberField
                        id="set-daily-cap"
                        label="Daily generation cap"
                        hint="Max threads generated across all jobs per day."
                        value={form.daily_generation_cap}
                        min={0}
                        onChange={(v) => patch({ daily_generation_cap: v })}
                    />
                    <NumberField
                        id="set-ai-share"
                        label="AI selection share (%)"
                        hint="Share of warmup sends that draw from AI-generated content (0–100)."
                        value={form.ai_selection_share}
                        min={0}
                        max={100}
                        onChange={(v) =>
                            patch({ ai_selection_share: Math.max(0, Math.min(100, v)) })
                        }
                    />
                </div>
            </section>

            <section className="space-y-3">
                <h2 className="text-sm font-semibold">Pools</h2>
                <div className="space-y-3">
                    {form.pools.map((pool, i) => (
                        <div
                            key={pool.pool_type || i}
                            className="space-y-3 rounded-lg border border-border bg-card p-4"
                        >
                            <div className="flex items-center justify-between">
                                <div className="flex items-center gap-2">
                                    {poolBadge(pool.pool_type)}
                                    <span className="text-sm font-medium capitalize">
                                        {pool.pool_type} pool
                                    </span>
                                </div>
                                <div className="flex items-center gap-2 text-xs text-muted-foreground">
                                    <span>{pool.enabled ? "Enabled" : "Disabled"}</span>
                                    <Switch
                                        checked={pool.enabled}
                                        onCheckedChange={(v) => patchPool(i, { enabled: v })}
                                    />
                                </div>
                            </div>
                            <div className="grid gap-4 sm:grid-cols-2">
                                <NumberField
                                    id={`pool-target-${i}`}
                                    label="Target active threads"
                                    hint="Library top-up target for this pool."
                                    value={pool.target_active_threads}
                                    min={0}
                                    onChange={(v) =>
                                        patchPool(i, { target_active_threads: v })
                                    }
                                />
                                <div>
                                    <Label
                                        htmlFor={`pool-segments-${i}`}
                                        className="mb-1 text-xs"
                                    >
                                        Segments
                                    </Label>
                                    <Input
                                        id={`pool-segments-${i}`}
                                        placeholder="comma,separated,segments"
                                        value={pool.segments.join(", ")}
                                        onChange={(e) =>
                                            patchPool(i, {
                                                segments: e.target.value
                                                    .split(",")
                                                    .map((s) => s.trim())
                                                    .filter(Boolean),
                                            })
                                        }
                                    />
                                    <p className="mt-1 text-[11px] text-muted-foreground">
                                        Comma-separated segments to generate for this pool.
                                    </p>
                                </div>
                            </div>
                        </div>
                    ))}
                    {form.pools.length === 0 && (
                        <div className="rounded-md border border-dashed border-border p-4 text-sm text-muted-foreground">
                            No pools configured.
                        </div>
                    )}
                </div>
            </section>

            <section className="space-y-3">
                <h2 className="text-sm font-semibold">Engagement simulation</h2>
                <p className="text-[12.5px] text-muted-foreground">
                    How recipient mailboxes behave toward warmup mail — rescuing from spam,
                    marking important/read, and dwell time before actions.
                </p>
                <div className="grid gap-4 rounded-lg border border-border bg-card p-4 sm:grid-cols-2">
                    <NumberField
                        id="eng-rescue"
                        label="Spam rescue rate"
                        hint="Fraction of spam-foldered warmup mail that gets rescued (0–1)."
                        value={form.engagement.spam_rescue_rate}
                        min={0}
                        max={1}
                        step={0.01}
                        onChange={(v) => patchEngagement({ spam_rescue_rate: v })}
                    />
                    <NumberField
                        id="eng-important"
                        label="Mark important rate"
                        hint="Fraction marked as important (0–1)."
                        value={form.engagement.mark_important_rate}
                        min={0}
                        max={1}
                        step={0.01}
                        onChange={(v) => patchEngagement({ mark_important_rate: v })}
                    />
                    <NumberField
                        id="eng-read"
                        label="Mark read rate"
                        hint="Fraction opened / marked read (0–1)."
                        value={form.engagement.mark_read_rate}
                        min={0}
                        max={1}
                        step={0.01}
                        onChange={(v) => patchEngagement({ mark_read_rate: v })}
                    />
                    <NumberField
                        id="eng-star"
                        label="Star rate (%)"
                        hint="Share of warmup mail starred / flagged (0–100)."
                        value={form.engagement.star_rate}
                        min={0}
                        max={100}
                        onChange={(v) =>
                            patchEngagement({ star_rate: Math.max(0, Math.min(100, v)) })
                        }
                    />
                    <NumberField
                        id="eng-min-dwell"
                        label="Min dwell (seconds)"
                        hint="Shortest simulated read time before an action."
                        value={form.engagement.min_dwell_seconds}
                        min={0}
                        onChange={(v) => patchEngagement({ min_dwell_seconds: v })}
                    />
                    <NumberField
                        id="eng-max-dwell"
                        label="Max dwell (seconds)"
                        hint="Longest simulated read time before an action."
                        value={form.engagement.max_dwell_seconds}
                        min={0}
                        onChange={(v) => patchEngagement({ max_dwell_seconds: v })}
                    />
                </div>
            </section>

            <div className="sticky bottom-0 flex justify-end gap-2 border-t border-border bg-background/95 py-3 backdrop-blur">
                <Button
                    variant="outline"
                    onClick={() => data?.data && setForm(data.data)}
                    disabled={save.isPending}
                >
                    Reset
                </Button>
                <Button
                    onClick={() => form && save.mutate(form)}
                    disabled={save.isPending}
                >
                    <FlaskConical className="size-4" />
                    {save.isPending ? "Saving…" : "Save settings"}
                </Button>
            </div>
        </div>
    );
}
