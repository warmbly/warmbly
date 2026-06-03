// /warmup-content/library — filterable, paged table of generated conversation
// threads, with a detail dialog + archive/unarchive/delete actions.

import { useEffect, useMemo, useState } from "react";
import {
    keepPreviousData,
    useMutation,
    useQuery,
    useQueryClient,
} from "@tanstack/react-query";
import { toast } from "sonner";
import { Archive, ArchiveRestore, Trash2 } from "lucide-react";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Label } from "@/components/ui/label";
import { Skeleton } from "@/components/ui/skeleton";
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
import { DataTable, type Column } from "@/components/data/DataTable";
import { useCursorPager } from "@/lib/useCursorPager";
import {
    archiveWarmupConversation,
    deleteWarmupConversation,
    getWarmupConversation,
    listWarmupConversations,
    unarchiveWarmupConversation,
    type WarmupConversationRow,
} from "@/lib/api/client/admin/warmupContent";
import { CONTENT_STATUS_TONE, fmtDate } from "./shared";

export default function LibraryPage() {
    const qc = useQueryClient();
    const [source, setSource] = useState("");
    const [status, setStatus] = useState("");
    const [openId, setOpenId] = useState<string | null>(null);
    const [confirmDelete, setConfirmDelete] = useState<WarmupConversationRow | null>(
        null,
    );
    const pager = useCursorPager();
    const { reset } = pager;

    const filterKey = JSON.stringify({ source, status });
    useEffect(() => {
        reset();
    }, [filterKey, reset]);

    const { data, isLoading, error, refetch } = useQuery({
        queryKey: ["admin", "warmup-content", "conversations", filterKey, pager.cursor],
        queryFn: () =>
            listWarmupConversations({
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
                        <div className="truncate font-medium">
                            {c.subject || "(no subject)"}
                        </div>
                        <div className="truncate text-[11px] text-muted-foreground">
                            {c.theme || c.description || "—"}
                        </div>
                    </div>
                ),
                csv: (c) => c.subject,
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
                    <span className="text-xs text-muted-foreground">
                        {c.source || "—"}
                    </span>
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
