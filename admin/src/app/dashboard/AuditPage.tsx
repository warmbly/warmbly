// /admin/audit-logs — cursor-paginated table. Same filter shape as the
// dashboard's audit page, just visually fitted into the admin shell.

import { useMemo, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { RefreshCw } from "lucide-react";
import { PageHeader } from "@/components/layout/PageHeader";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Skeleton } from "@/components/ui/skeleton";
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
    "worker", "aws_credentials", "worker_profile", "release",
    "user", "email_account", "campaign", "plan",
];

// Action -> tone. Destructive actions get red, lifecycle gets green/blue,
// security operations get purple. Read in the audit feed at a glance.
const ACTION_TONE: Record<string, string> = {
    delete: "text-red-600",
    uninstall: "text-red-600",
    ban_user: "text-red-600",
    block_account: "text-red-600",
    install: "text-emerald-700",
    create: "text-emerald-700",
    rotate_keys: "text-purple-700",
    system_update: "text-blue-700",
    reboot: "text-blue-700",
};

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

    return (
        <div>
            <PageHeader
                title="Audit log"
                description="Every mutating admin action. Backed by /admin/audit-logs and the admin_audit_logs table."
            >
                <label className="flex items-center gap-2 text-xs text-muted-foreground">
                    <input
                        type="checkbox"
                        checked={autoRefresh}
                        onChange={(e) => setAutoRefresh(e.target.checked)}
                        className="size-3.5"
                    />
                    Auto-refresh
                </label>
                <Button
                    size="sm"
                    variant="outline"
                    onClick={() => refetch()}
                    disabled={isFetching}
                >
                    <RefreshCw className="size-4" />
                    {isFetching ? "Refreshing…" : "Refresh"}
                </Button>
            </PageHeader>

            <div className="rounded-lg border border-border bg-muted/40 p-3 mb-3 grid grid-cols-1 md:grid-cols-3 gap-3">
                <FilterField label="Action">
                    <select
                        value={filters.action ?? ""}
                        onChange={(e) => applyFilter({ action: e.target.value || undefined })}
                        className="w-full border border-border rounded px-2 py-1.5 text-sm bg-background"
                    >
                        <option value="">(any)</option>
                        {allActions.map((a) => (
                            <option key={a} value={a}>{a}</option>
                        ))}
                    </select>
                </FilterField>
                <FilterField label="Target type">
                    <select
                        value={filters.target_type ?? ""}
                        onChange={(e) => applyFilter({ target_type: e.target.value || undefined })}
                        className="w-full border border-border rounded px-2 py-1.5 text-sm bg-background"
                    >
                        <option value="">(any)</option>
                        {allTargets.map((t) => (
                            <option key={t} value={t}>{t}</option>
                        ))}
                    </select>
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

            {isLoading && <Skeleton className="h-40 w-full" />}

            {!isLoading && (
                <div className="border border-border rounded-lg overflow-hidden bg-card">
                    <table className="w-full text-sm">
                        <thead className="bg-muted/50 text-muted-foreground text-xs uppercase">
                            <tr>
                                <th className="w-8" />
                                <th className="text-left px-3 py-2 font-medium">When</th>
                                <th className="text-left px-3 py-2 font-medium">Admin</th>
                                <th className="text-left px-3 py-2 font-medium">Action</th>
                                <th className="text-left px-3 py-2 font-medium">Target</th>
                                <th className="text-left px-3 py-2 font-medium">IP</th>
                            </tr>
                        </thead>
                        <tbody>
                            {(data?.data ?? []).map((row) => (
                                <Row key={row.id} row={row} />
                            ))}
                            {data && data.data.length === 0 && (
                                <tr>
                                    <td colSpan={6} className="text-center text-muted-foreground py-8 text-sm">
                                        No audit entries match these filters.
                                    </td>
                                </tr>
                            )}
                        </tbody>
                    </table>
                </div>
            )}

            <div className="flex items-center justify-between mt-3 text-sm">
                <div className="text-xs text-muted-foreground">
                    {data?.data?.length ?? 0} entries
                    {cursors.length > 0 && ` · page ${cursors.length + 1}`}
                </div>
                <div className="flex gap-2">
                    <Button
                        size="sm"
                        variant="outline"
                        onClick={() => setCursors((c) => c.slice(0, -1))}
                        disabled={cursors.length === 0}
                    >
                        Prev
                    </Button>
                    <Button
                        size="sm"
                        variant="outline"
                        onClick={() => {
                            const c = data?.pagination?.cursor;
                            if (c) setCursors((prev) => [...prev, c]);
                        }}
                        disabled={!data?.pagination?.cursor}
                    >
                        Next
                    </Button>
                </div>
            </div>
        </div>
    );
}

function FilterField({ label, children }: { label: string; children: React.ReactNode }) {
    return (
        <div>
            <Label className="text-[10px] font-semibold text-muted-foreground uppercase block mb-1">
                {label}
            </Label>
            {children}
        </div>
    );
}

function Row({ row }: { row: AdminAuditLog }) {
    const [open, setOpen] = useState(false);
    const hasDetails = row.details && Object.keys(row.details).length > 0;
    const systemActor = row.admin_user_id === "00000000-0000-0000-0000-000000000000";
    return (
        <>
            <tr
                className="border-t border-border hover:bg-muted/30 cursor-pointer"
                onClick={() => hasDetails && setOpen(!open)}
            >
                <td className="px-2 py-2 text-center text-muted-foreground">
                    {hasDetails ? (open ? "▾" : "▸") : ""}
                </td>
                <td className="px-3 py-2 text-xs text-muted-foreground whitespace-nowrap">
                    {new Date(row.created_at).toLocaleString()}
                </td>
                <td className="px-3 py-2 text-xs">
                    {systemActor ? (
                        <span className="text-muted-foreground">system</span>
                    ) : row.admin_user ? (
                        <div>
                            <div>{row.admin_user.first_name} {row.admin_user.last_name}</div>
                            <div className="text-[10px] text-muted-foreground font-mono">
                                {row.admin_user.email}
                            </div>
                        </div>
                    ) : (
                        <span className="font-mono text-[10px] text-muted-foreground">
                            {row.admin_user_id.slice(0, 8)}…
                        </span>
                    )}
                </td>
                <td className={`px-3 py-2 text-xs font-medium ${ACTION_TONE[row.action] ?? "text-foreground"}`}>
                    {row.action}
                </td>
                <td className="px-3 py-2 text-xs">
                    <div>{row.target_type}</div>
                    {row.target_id !== "00000000-0000-0000-0000-000000000000" && (
                        <div className="font-mono text-[10px] text-muted-foreground">
                            {row.target_id}
                        </div>
                    )}
                </td>
                <td className="px-3 py-2 text-xs text-muted-foreground font-mono">
                    {row.ip_address || "—"}
                </td>
            </tr>
            {open && hasDetails && (
                <tr className="bg-muted/40">
                    <td />
                    <td colSpan={5} className="px-3 py-3">
                        <div className="text-[10px] text-muted-foreground mb-1 uppercase">Details</div>
                        <pre className="bg-background border border-border rounded p-2 text-xs overflow-auto font-mono">
                            {JSON.stringify(row.details, null, 2)}
                        </pre>
                    </td>
                </tr>
            )}
        </>
    );
}
