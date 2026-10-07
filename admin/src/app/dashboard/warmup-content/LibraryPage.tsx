// /warmup-content/library: filterable, paged table of generated conversation
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
import { Button } from "@/components/ui/button";
import { Skeleton } from "@/components/ui/skeleton";
import { Property, PropertyList, StatusBadge } from "@/components/ui/kit";
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
import { TONE_TEXT } from "@/lib/tones";
import { cn } from "@/lib/utils";
import { contentTone, fmtDate } from "./shared";

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
                    <div className="min-w-0 max-w-[28rem] py-1.5">
                        <div className="truncate font-medium text-foreground">
                            {c.subject || "(no subject)"}
                        </div>
                        <div className="truncate text-xs text-muted-foreground">
                            {c.theme || c.description || "—"}
                        </div>
                    </div>
                ),
                csv: (c) => c.subject,
            },
            {
                id: "segment",
                header: "Segment",
                cell: (c) => <span>{c.segment || "—"}</span>,
                csv: (c) => c.segment,
            },
            {
                id: "source",
                header: "Source",
                cell: (c) => (
                    <span className="text-muted-foreground">{c.source || "—"}</span>
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
                cell: (c) => (
                    <StatusBadge tone={c.lint_passed ? "success" : "danger"}>
                        {c.lint_passed ? "pass" : "fail"}
                    </StatusBadge>
                ),
                csv: (c) => (c.lint_passed ? "pass" : "fail"),
            },
            {
                id: "review",
                header: "Semantic review",
                cell: (c) => <StatusBadge tone={c.semantic_review === "passed" ? "success" : "neutral"}>{c.semantic_review ?? "legacy_unknown"}</StatusBadge>,
                csv: (c) => c.semantic_review ?? "legacy_unknown",
            },
            {
                id: "status",
                header: "Status",
                cell: (c) => (
                    <StatusBadge tone={contentTone(c.status)} dot>
                        {c.status}
                    </StatusBadge>
                ),
                csv: (c) => c.status,
            },
            {
                id: "created",
                header: "Created",
                cell: (c) => (
                    <span className="whitespace-nowrap text-muted-foreground">
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
                        className="flex items-center justify-end gap-1"
                        onClick={(e) => e.stopPropagation()}
                    >
                        {c.status === "archived" ? (
                            <Button
                                size="xs"
                                variant="ghost"
                                onClick={() => unarchive.mutate(c.id)}
                                disabled={unarchive.isPending || (c.semantic_review !== undefined && !["passed", "legacy_unknown"].includes(c.semantic_review))}
                            >
                                <ArchiveRestore /> Restore
                            </Button>
                        ) : (
                            <Button
                                size="xs"
                                variant="ghost"
                                onClick={() => archive.mutate(c.id)}
                                disabled={archive.isPending}
                            >
                                <Archive /> Archive
                            </Button>
                        )}
                        <Button
                            size="xs"
                            variant="ghost"
                            className={cn(TONE_TEXT.danger, "hover:bg-red-500/10 hover:text-red-700 dark:hover:text-red-400")}
                            onClick={() => setConfirmDelete(c)}
                        >
                            <Trash2 /> Delete
                        </Button>
                    </div>
                ),
            },
        ],
        [archive, unarchive],
    );

    const rows = data?.data ?? [];

    return (
        <div className="space-y-3">
            <div className="flex flex-wrap items-center gap-2">
                <FilterSelect
                    label="Source"
                    value={source || "any"}
                    onChange={(v) => setSource(v === "any" ? "" : v)}
                    placeholder="Any source"
                    options={[
                        { value: "any", label: "Any source" },
                        { value: "ai", label: "AI generated" },
                        { value: "curated", label: "Curated" },
                        { value: "imported", label: "Imported" },
                    ]}
                />
                <FilterSelect
                    label="Status"
                    value={status || "any"}
                    onChange={(v) => setStatus(v === "any" ? "" : v)}
                    placeholder="Any status"
                    options={[
                        { value: "any", label: "Any status" },
                        { value: "active", label: "Active" },
                        { value: "archived", label: "Archived" },
                        { value: "draft", label: "Draft" },
                    ]}
                />
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
                            from the library. This cannot be undone. Archive instead if you
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
                        <div className="flex flex-wrap items-center gap-1.5">
                            <StatusBadge tone={contentTone(c.status)} dot>
                                {c.status}
                            </StatusBadge>
                            <StatusBadge tone={c.lint_passed ? "success" : "danger"}>
                                lint {c.lint_passed ? "pass" : "fail"}
                            </StatusBadge>
                            <StatusBadge>review: {c.semantic_review ?? "legacy_unknown"}</StatusBadge>
                            {c.scenario_version && <StatusBadge>scenario: {c.scenario_version}</StatusBadge>}
                            {c.rendering_version && <StatusBadge>rendering: {c.rendering_version}</StatusBadge>}
                            {c.segment && <StatusBadge>segment: {c.segment}</StatusBadge>}
                            <StatusBadge>source: {c.source || "—"}</StatusBadge>
                            <span className="ml-1 text-xs tabular-nums text-muted-foreground">
                                used {c.usage_count}×
                            </span>
                        </div>

                        <div className="max-h-[50vh] overflow-y-auto surface-lit rounded-xl border border-border bg-card">
                            {(c.messages ?? []).map((m, i) => (
                                <div
                                    key={i}
                                    className="border-b border-border/70 px-3.5 py-3 text-[13px] leading-relaxed whitespace-pre-wrap last:border-0"
                                >
                                    <div className="mb-1 text-xs font-medium text-muted-foreground">
                                        Message {i + 1}
                                    </div>
                                    {m}
                                </div>
                            ))}
                            {(c.messages ?? []).length === 0 && (
                                <div className="py-6 text-center text-[13px] text-muted-foreground">
                                    No messages in this thread.
                                </div>
                            )}
                        </div>

                        <PropertyList className="border-t border-border">
                            <Property label="Generated by job">
                                <span className="font-mono text-xs">{c.generated_by_job_id ?? "—"}</span>
                            </Property>
                            <Property label="Created">{fmtDate(c.created_at)}</Property>
                            <Property label="Updated">{fmtDate(c.updated_at)}</Property>
                        </PropertyList>
                    </div>
                )}

                <DialogFooter showCloseButton />
            </DialogContent>
        </Dialog>
    );
}

function FilterSelect({
    label,
    value,
    onChange,
    placeholder,
    options,
}: {
    label: string;
    value: string;
    onChange: (v: string) => void;
    placeholder: string;
    options: { value: string; label: string }[];
}) {
    return (
        <div className="flex items-center gap-1.5">
            <span className="text-xs font-medium text-muted-foreground">{label}</span>
            <Select value={value} onValueChange={onChange}>
                <SelectTrigger size="sm" className="w-40" aria-label={label}>
                    <SelectValue placeholder={placeholder} />
                </SelectTrigger>
                <SelectContent>
                    {options.map((o) => (
                        <SelectItem key={o.value} value={o.value}>
                            {o.label}
                        </SelectItem>
                    ))}
                </SelectContent>
            </Select>
        </div>
    );
}
