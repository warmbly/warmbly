import { useMemo, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { searchAdminAuditLogs } from "@/lib/api/client/app/admin/audit";
import type { AdminAuditLog, AdminAuditLogSearch } from "@/lib/api/models/app/admin/Audit";

// Free-form action / target_type strings used by handlers across the app.
// These are populated dynamically from the result set too — anything new the
// backend logs shows up in the dropdown automatically.
const KNOWN_ACTIONS = [
    "create", "update", "delete",
    "test", "install", "restart", "upgrade", "uninstall",
    "rotate_keys", "apply", "assign",
    "system_update", "reboot", "check_releases",
    "auto_reassign",
    "ban_user", "unban_user",
    "block_account", "unblock_account",
    "review_appeal", "stop_campaign",
    "grant_admin_permissions", "revoke_admin_permissions",
];

const KNOWN_ENTITY_TYPES = [
    "worker", "aws_credentials", "worker_profile", "release",
    "user", "email_account", "campaign", "plan",
];

const ACTION_TONE: Record<string, string> = {
    delete: "text-red-600",
    uninstall: "text-red-600",
    ban_user: "text-red-600",
    block_account: "text-red-600",
    auto_reassign: "text-amber-600",
    install: "text-green-700",
    create: "text-green-700",
    rotate_keys: "text-purple-700",
    system_update: "text-blue-700",
    reboot: "text-blue-700",
};

export default function AdminAuditPage() {
    const [filters, setFilters] = useState<AdminAuditLogSearch>({ limit: 50 });
    const [autoRefresh, setAutoRefresh] = useState(false);
    const [cursors, setCursors] = useState<string[]>([]); // page history

    const search = useMemo<AdminAuditLogSearch>(
        () => ({
            ...filters,
            cursor: cursors.length > 0 ? cursors[cursors.length - 1] : undefined,
        }),
        [filters, cursors],
    );

    const { data, isLoading, refetch, isFetching } = useQuery({
        queryKey: ["admin", "audit", search],
        queryFn: () => searchAdminAuditLogs(search),
        refetchInterval: autoRefresh ? 5_000 : false,
    });

    function applyFilter(patch: Partial<AdminAuditLogSearch>) {
        setCursors([]); // reset paging when filters change
        setFilters((f) => ({ ...f, ...patch }));
    }

    function resetFilters() {
        setCursors([]);
        setFilters({ limit: filters.limit ?? 50 });
    }

    function nextPage() {
        const nextCursor = data?.pagination?.cursor;
        if (nextCursor) setCursors((c) => [...c, nextCursor]);
    }

    function prevPage() {
        setCursors((c) => c.slice(0, -1));
    }

    // Merge known actions with anything new from the result set.
    const allActions = useMemo(() => {
        const set = new Set<string>(KNOWN_ACTIONS);
        for (const r of data?.data ?? []) set.add(r.action);
        return Array.from(set).sort();
    }, [data]);

    const allEntityTypes = useMemo(() => {
        const set = new Set<string>(KNOWN_ENTITY_TYPES);
        for (const r of data?.data ?? []) set.add(r.target_type);
        return Array.from(set).sort();
    }, [data]);

    return (
        <div>
            <div className="flex items-center justify-between mb-4">
                <div>
                    <h2 className="text-slate-700 font-semibold text-lg">Audit Log</h2>
                    <p className="text-slate-400 text-sm">
                        Every mutating admin action. Searches the
                        <code className="bg-slate-100 px-1 mx-1 rounded">admin_audit_log</code>
                        table.
                    </p>
                </div>
                <div className="flex items-center gap-2 text-sm">
                    <label className="flex items-center gap-2 text-slate-600">
                        <input
                            type="checkbox"
                            checked={autoRefresh}
                            onChange={(e) => setAutoRefresh(e.target.checked)}
                        />
                        Auto-refresh
                    </label>
                    <button
                        onClick={() => refetch()}
                        className="border px-3 py-1.5 rounded text-sm hover:bg-slate-50"
                    >
                        {isFetching ? "Refreshing…" : "Refresh"}
                    </button>
                </div>
            </div>

            {/* Filters */}
            <div className="border rounded-lg p-3 mb-3 bg-slate-50">
                <div className="grid grid-cols-1 md:grid-cols-3 gap-3">
                    <Field label="Action">
                        <select
                            value={filters.action ?? ""}
                            onChange={(e) => applyFilter({ action: e.target.value || undefined })}
                            className={inp}
                        >
                            <option value="">(any)</option>
                            {allActions.map((a) => (
                                <option key={a} value={a}>{a}</option>
                            ))}
                        </select>
                    </Field>
                    <Field label="Target type">
                        <select
                            value={filters.target_type ?? ""}
                            onChange={(e) => applyFilter({ target_type: e.target.value || undefined })}
                            className={inp}
                        >
                            <option value="">(any)</option>
                            {allEntityTypes.map((t) => (
                                <option key={t} value={t}>{t}</option>
                            ))}
                        </select>
                    </Field>
                    <Field label="Target ID">
                        <input
                            value={filters.target_id ?? ""}
                            onChange={(e) => applyFilter({ target_id: e.target.value || undefined })}
                            placeholder="uuid"
                            className={inp + " font-mono text-xs"}
                        />
                    </Field>
                    <Field label="Admin user ID">
                        <input
                            value={filters.admin_user_id ?? ""}
                            onChange={(e) => applyFilter({ admin_user_id: e.target.value || undefined })}
                            placeholder="uuid (00000…000 = system)"
                            className={inp + " font-mono text-xs"}
                        />
                    </Field>
                    <Field label="From">
                        <input
                            type="datetime-local"
                            value={filters.start_date ?? ""}
                            onChange={(e) => applyFilter({ start_date: e.target.value || undefined })}
                            className={inp}
                        />
                    </Field>
                    <Field label="Until">
                        <input
                            type="datetime-local"
                            value={filters.end_date ?? ""}
                            onChange={(e) => applyFilter({ end_date: e.target.value || undefined })}
                            className={inp}
                        />
                    </Field>
                </div>
                <div className="flex items-center gap-3 mt-3">
                    <div className="text-xs text-slate-500">
                        Limit:{" "}
                        <select
                            value={filters.limit ?? 50}
                            onChange={(e) => applyFilter({ limit: parseInt(e.target.value, 10) })}
                            className="border rounded px-2 py-1 text-xs"
                        >
                            {[25, 50, 100].map((n) => <option key={n} value={n}>{n}</option>)}
                        </select>
                    </div>
                    <button
                        onClick={resetFilters}
                        className="text-xs text-slate-500 hover:underline"
                    >
                        clear all
                    </button>
                </div>
            </div>

            {/* Results */}
            {isLoading && <p className="text-slate-400 text-sm">Loading…</p>}

            <div className="border rounded-lg overflow-hidden">
                <table className="w-full text-sm">
                    <thead className="bg-slate-50 text-slate-500 text-xs uppercase">
                        <tr>
                            <th className="w-10"></th>
                            <th className="text-left px-3 py-2">When</th>
                            <th className="text-left px-3 py-2">Admin</th>
                            <th className="text-left px-3 py-2">Action</th>
                            <th className="text-left px-3 py-2">Target</th>
                            <th className="text-left px-3 py-2">IP</th>
                        </tr>
                    </thead>
                    <tbody>
                        {(data?.data ?? []).map((row) => <Row key={row.id} row={row} />)}
                        {data && data.data.length === 0 && (
                            <tr>
                                <td colSpan={6} className="text-center text-slate-400 py-8 text-sm">
                                    No audit entries match these filters.
                                </td>
                            </tr>
                        )}
                    </tbody>
                </table>
            </div>

            {/* Pagination */}
            <div className="flex items-center justify-between mt-3 text-sm">
                <div className="text-slate-400">
                    {data?.data?.length ?? 0} entries
                    {cursors.length > 0 && ` · page ${cursors.length + 1}`}
                </div>
                <div className="flex gap-2">
                    <button
                        onClick={prevPage}
                        disabled={cursors.length === 0}
                        className="border px-3 py-1.5 rounded text-sm disabled:opacity-50"
                    >
                        ← Prev
                    </button>
                    <button
                        onClick={nextPage}
                        disabled={!data?.pagination?.cursor}
                        className="border px-3 py-1.5 rounded text-sm disabled:opacity-50"
                    >
                        Next →
                    </button>
                </div>
            </div>
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
                className="border-t hover:bg-slate-50 cursor-pointer"
                onClick={() => hasDetails && setOpen(!open)}
            >
                <td className="px-2 py-2 text-center text-slate-400">
                    {hasDetails ? (open ? "▾" : "▸") : ""}
                </td>
                <td className="px-3 py-2 text-xs text-slate-600 whitespace-nowrap">
                    {new Date(row.created_at).toLocaleString()}
                </td>
                <td className="px-3 py-2 text-xs">
                    {systemActor ? (
                        <span className="text-slate-500">system</span>
                    ) : row.admin_user ? (
                        <div>
                            <div className="text-slate-700">
                                {row.admin_user.first_name} {row.admin_user.last_name}
                            </div>
                            <div className="text-slate-400 font-mono text-[10px]">
                                {row.admin_user.email}
                            </div>
                        </div>
                    ) : (
                        <span className="font-mono text-[10px] text-slate-500">
                            {row.admin_user_id.slice(0, 8)}…
                        </span>
                    )}
                </td>
                <td className={`px-3 py-2 text-xs font-medium ${ACTION_TONE[row.action] ?? "text-slate-700"}`}>
                    {row.action}
                </td>
                <td className="px-3 py-2 text-xs">
                    <div className="text-slate-700">{row.target_type}</div>
                    {row.target_id !== "00000000-0000-0000-0000-000000000000" && (
                        <div className="font-mono text-[10px] text-slate-400">
                            {row.target_id}
                        </div>
                    )}
                </td>
                <td className="px-3 py-2 text-xs text-slate-500 font-mono">
                    {row.ip_address || "—"}
                </td>
            </tr>
            {open && hasDetails && (
                <tr className="bg-slate-50">
                    <td></td>
                    <td colSpan={5} className="px-3 py-2">
                        <div className="text-xs text-slate-500 mb-1">Details</div>
                        <pre className="bg-white border rounded p-2 text-xs overflow-auto font-mono">
                            {JSON.stringify(row.details, null, 2)}
                        </pre>
                        {row.user_agent && (
                            <div className="text-xs text-slate-400 mt-1">
                                <span className="text-slate-500">User-agent:</span> {row.user_agent}
                            </div>
                        )}
                    </td>
                </tr>
            )}
        </>
    );
}

const inp = "w-full border rounded px-2 py-1.5 text-sm bg-white";

function Field({ label, children }: { label: string; children: React.ReactNode }) {
    return (
        <div>
            <label className="text-xs font-medium text-slate-500 uppercase block mb-1">{label}</label>
            {children}
        </div>
    );
}
