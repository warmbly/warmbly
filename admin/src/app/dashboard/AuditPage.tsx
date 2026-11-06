// /admin/audit-logs — cursor-paginated table. Same filter shape as the
// dashboard's audit page, just visually fitted into the admin shell.

import { useMemo, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { ChevronLeft, ChevronRight, RefreshCw, ScrollText } from "lucide-react";
import { PageHeader } from "@/components/layout/PageHeader";
import { Button } from "@/components/ui/button";
import { Checkbox } from "@/components/ui/checkbox";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Skeleton } from "@/components/ui/skeleton";
import { EmptyState, StatusDot } from "@/components/ui/kit";
import { TONE_DOT, TONE_TEXT, type Tone } from "@/lib/tones";
import { cn } from "@/lib/utils";
import { searchAdminAuditLogs } from "@/lib/api/client/admin/audit";
import type {
    AdminAuditLog,
    AdminAuditLogSearch,
} from "@/lib/api/models/admin";

const KNOWN_ACTIONS = [
    "create", "update", "delete",
    "test", "install", "restart", "upgrade", "uninstall",
    "rotate_keys", "apply", "assign",
    "system_update", "reboot",
    "ban_user", "unban_user",
    "block_account", "unblock_account",
    "review_appeal", "stop_campaign",
    "grant_admin_permissions", "revoke_admin_permissions",
];

const KNOWN_TARGETS = [
    "worker", "aws_credentials", "worker_profile", "release", "instance",
    "user", "email_account", "campaign", "plan",
];

// Action -> tone. Destructive actions read red, lifecycle green/blue,
// security operations in strong neutral. Read in the audit feed at a glance.
const ACTION_TONE: Record<string, Tone> = {
    delete: "danger",
    uninstall: "danger",
    ban_user: "danger",
    block_account: "danger",
    install: "success",
    create: "success",
    rotate_keys: "strong",
    system_update: "info",
    reboot: "info",
};

// Radix Select refuses an empty value, so "any" travels as a sentinel.
const ANY = "__any";

const NIL_UUID = "00000000-0000-0000-0000-000000000000";

export default function AuditPage() {
    const [filters, setFilters] = useState<AdminAuditLogSearch>({ limit: 50 });
    const [cursors, setCursors] = useState<string[]>([]);
    const [autoRefresh, setAutoRefresh] = useState(false);

    const search = useMemo<AdminAuditLogSearch>(
        () => ({
            ...filters,
            cursor: cursors.length > 0 ? cursors[cursors.length - 1] : undefined,
        }),
        [filters, cursors],
    );

    const { data, isLoading, isFetching, refetch } = useQuery({
        queryKey: ["admin", "audit", search],
        queryFn: () => searchAdminAuditLogs(search),
        refetchInterval: autoRefresh ? 5_000 : false,
    });

    function applyFilter(patch: Partial<AdminAuditLogSearch>) {
        setCursors([]);
        setFilters((f) => ({ ...f, ...patch }));
    }

    const allActions = useMemo(() => {
        const set = new Set<string>(KNOWN_ACTIONS);
        for (const r of data?.data ?? []) set.add(r.action);
        return Array.from(set).sort();
    }, [data]);

    const allTargets = useMemo(() => {
        const set = new Set<string>(KNOWN_TARGETS);
        for (const r of data?.data ?? []) set.add(r.target_type);
        return Array.from(set).sort();
    }, [data]);

    const rows = data?.data ?? [];

    return (
        <div>
            <PageHeader
                title="Audit log"
                description="Every mutating admin action. Backed by /admin/audit-logs and the admin_audit_logs table."
            >
                <label className="flex h-7 cursor-pointer items-center gap-2 rounded-md px-1.5 text-[12.5px] text-muted-foreground hover:text-foreground">
                    <Checkbox checked={autoRefresh} onCheckedChange={(v) => setAutoRefresh(v === true)} />
                    Auto-refresh
                    {autoRefresh && <StatusDot tone="success" pulse />}
                </label>
                <Button size="sm" variant="outline" onClick={() => refetch()} disabled={isFetching}>
                    <RefreshCw className={cn(isFetching && "animate-spin")} />
                    {isFetching ? "Refreshing…" : "Refresh"}
                </Button>
            </PageHeader>

            <div className="mb-4 grid grid-cols-1 gap-x-3 gap-y-3 surface-lit rounded-xl border border-border bg-card p-3 sm:grid-cols-2 lg:grid-cols-3">
                <FilterField label="Action">
                    <Select
                        value={filters.action ?? ANY}
                        onValueChange={(v) => applyFilter({ action: v === ANY ? undefined : v })}
                    >
                        <SelectTrigger className="w-full">
                            <SelectValue />
                        </SelectTrigger>
                        <SelectContent>
                            <SelectItem value={ANY}>(any)</SelectItem>
                            {allActions.map((a) => (
                                <SelectItem key={a} value={a}>
                                    {a}
                                </SelectItem>
                            ))}
                        </SelectContent>
                    </Select>
                </FilterField>
                <FilterField label="Target type">
                    <Select
                        value={filters.target_type ?? ANY}
                        onValueChange={(v) => applyFilter({ target_type: v === ANY ? undefined : v })}
                    >
                        <SelectTrigger className="w-full">
                            <SelectValue />
                        </SelectTrigger>
                        <SelectContent>
                            <SelectItem value={ANY}>(any)</SelectItem>
                            {allTargets.map((t) => (
                                <SelectItem key={t} value={t}>
                                    {t}
                                </SelectItem>
                            ))}
                        </SelectContent>
                    </Select>
                </FilterField>
                <FilterField label="Target ID">
                    <Input
                        value={filters.target_id ?? ""}
                        onChange={(e) => applyFilter({ target_id: e.target.value || undefined })}
                        placeholder="uuid"
                        className="font-mono text-xs"
                    />
                </FilterField>
                <FilterField label="Admin user ID">
                    <Input
                        value={filters.admin_user_id ?? ""}
                        onChange={(e) => applyFilter({ admin_user_id: e.target.value || undefined })}
                        placeholder="uuid"
                        className="font-mono text-xs"
                    />
                </FilterField>
                <FilterField label="From">
                    <Input
                        type="datetime-local"
                        value={filters.start_date ?? ""}
                        onChange={(e) => applyFilter({ start_date: e.target.value || undefined })}
                    />
                </FilterField>
                <FilterField label="Until">
                    <Input
                        type="datetime-local"
                        value={filters.end_date ?? ""}
                        onChange={(e) => applyFilter({ end_date: e.target.value || undefined })}
                    />
                </FilterField>
            </div>

            {isLoading && (
                <div className="overflow-hidden surface-lit rounded-xl border border-border bg-card">
                    {Array.from({ length: 6 }).map((_, i) => (
                        <div key={i} className="flex h-10 items-center gap-4 border-b border-border/70 px-4 last:border-0">
                            <Skeleton className="h-3 w-28" />
                            <Skeleton className="h-3 w-32" />
                            <Skeleton className="h-3 w-20" />
                            <Skeleton className="h-3 w-40" />
                        </div>
                    ))}
                </div>
            )}

            {!isLoading && (
                <div className="overflow-hidden surface-lit rounded-xl border border-border bg-card">
                    <div className="overflow-x-auto">
                        <table className="w-full text-[13px]">
                            <thead>
                                <tr className="h-9 border-b border-border text-left text-xs text-muted-foreground">
                                    <th className="w-9 pl-4">
                                        <span className="sr-only">Expand</span>
                                    </th>
                                    <th className="px-3 font-medium">When</th>
                                    <th className="px-3 font-medium">Admin</th>
                                    <th className="px-3 font-medium">Action</th>
                                    <th className="px-3 font-medium">Target</th>
                                    <th className="px-3 pr-4 font-medium">IP</th>
                                </tr>
                            </thead>
                            <tbody>
                                {rows.map((row) => (
                                    <Row key={row.id} row={row} />
                                ))}
                                {data && !data.data?.length && (
                                    <tr>
                                        <td colSpan={6}>
                                            <EmptyState icon={ScrollText} title="No audit entries match these filters." />
                                        </td>
                                    </tr>
                                )}
                            </tbody>
                        </table>
                    </div>
                </div>
            )}

            <div className="mt-3 flex items-center justify-between gap-3">
                <div className="text-xs text-muted-foreground tabular-nums">
                    {data?.data?.length ?? 0} entries
                    {cursors.length > 0 && ` · page ${cursors.length + 1}`}
                </div>
                <div className="flex gap-1.5">
                    <Button
                        size="sm"
                        variant="outline"
                        onClick={() => setCursors((c) => c.slice(0, -1))}
                        disabled={cursors.length === 0}
                    >
                        <ChevronLeft />
                        Prev
                    </Button>
                    <Button
                        size="sm"
                        variant="outline"
                        onClick={() => {
                            const c = data?.pagination?.next_cursor;
                            if (c) setCursors((prev) => [...prev, c]);
                        }}
                        disabled={!data?.pagination?.next_cursor}
                    >
                        Next
                        <ChevronRight />
                    </Button>
                </div>
            </div>
        </div>
    );
}

function FilterField({ label, children }: { label: string; children: React.ReactNode }) {
    return (
        <div className="min-w-0">
            <Label className="mb-1.5 block text-xs font-medium text-muted-foreground">{label}</Label>
            {children}
        </div>
    );
}

function Row({ row }: { row: AdminAuditLog }) {
    const [open, setOpen] = useState(false);
    const hasDetails = row.details && Object.keys(row.details).length > 0;
    // The actor's id is cleared when their account is deleted.
    const deletedActor = row.admin_user_id === NIL_UUID;
    const tone = ACTION_TONE[row.action] ?? "neutral";
    return (
        <>
            <tr
                className={cn(
                    "h-10 cursor-pointer border-b border-border/70 transition-colors last:border-b-0 hover:bg-accent/50",
                    open && "bg-accent/40",
                )}
                onClick={() => hasDetails && setOpen(!open)}
            >
                <td className="w-9 pl-4 text-subtle-foreground">
                    {hasDetails && (
                        <ChevronRight className={cn("size-3.5 transition-transform", open && "rotate-90")} />
                    )}
                </td>
                <td className="whitespace-nowrap px-3 py-2 text-muted-foreground tabular-nums">
                    {new Date(row.created_at).toLocaleString()}
                </td>
                <td className="px-3 py-2">
                    {deletedActor ? (
                        <span className="text-subtle-foreground">Deleted account</span>
                    ) : row.admin_user ? (
                        <div className="min-w-0">
                            <div className="whitespace-nowrap text-foreground">
                                {row.admin_user.first_name} {row.admin_user.last_name}
                            </div>
                            <div className="font-mono text-[11px] text-muted-foreground">{row.admin_user.email}</div>
                        </div>
                    ) : (
                        <span className="font-mono text-[11px] text-muted-foreground">
                            {row.admin_user_id.slice(0, 8)}…
                        </span>
                    )}
                </td>
                <td className="px-3 py-2">
                    <span className="inline-flex items-center gap-1.5 whitespace-nowrap">
                        <span className={cn("size-1.5 shrink-0 rounded-full", TONE_DOT[tone])} />
                        <span className={cn("font-medium", tone === "neutral" ? "text-foreground" : TONE_TEXT[tone])}>
                            {row.action}
                        </span>
                    </span>
                </td>
                <td className="px-3 py-2">
                    <div className="text-foreground">{row.target_type}</div>
                    {row.target_id !== NIL_UUID && (
                        <div className="font-mono text-[11px] text-muted-foreground">{row.target_id}</div>
                    )}
                </td>
                <td className="whitespace-nowrap px-3 py-2 pr-4 font-mono text-xs text-muted-foreground">
                    {row.ip_address || "—"}
                </td>
            </tr>
            {open && hasDetails && (
                <tr className="border-b border-border/70 bg-muted/30 last:border-b-0">
                    <td />
                    <td colSpan={5} className="px-3 py-3 pr-4">
                        <div className="mb-1.5 text-xs font-medium text-muted-foreground">Details</div>
                        <pre className="max-h-80 overflow-auto rounded-md border border-border bg-card p-3 font-mono text-[11.5px] leading-relaxed text-foreground">
                            {JSON.stringify(row.details, null, 2)}
                        </pre>
                    </td>
                </tr>
            )}
        </>
    );
}
