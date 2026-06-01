// Users browser — left filter rail + a server-driven, sortable, cursor-paged
// table. Search name/email, filter by status + admin, sort by name/email,
// page through results, toggle columns, export CSV. Row → detail.

import { useEffect, useState } from "react";
import { useQuery, keepPreviousData } from "@tanstack/react-query";
import { Link, useNavigate } from "react-router-dom";
import { ShieldAlert } from "lucide-react";
import { PageHeader } from "@/components/layout/PageHeader";
import { Badge } from "@/components/ui/badge";
import { Explorer, FilterGroup, SearchFilter, SegmentedFilter, ToggleFilter } from "@/components/data/Explorer";
import { DataTable, type Column } from "@/components/data/DataTable";
import { useCursorPager } from "@/lib/useCursorPager";
import { searchUsers } from "@/lib/api/client/admin/users";
import type { AdminUserDetail } from "@/lib/api/models/admin";

type StatusFilter = "active" | "banned" | "all";

const fullName = (u: AdminUserDetail) => `${u.first_name} ${u.last_name}`.trim() || u.email;

const columns: Column<AdminUserDetail>[] = [
    {
        id: "name",
        header: "Name",
        sortable: true,
        sortKey: "name",
        cell: (u) => (
            <div className="flex items-center gap-1.5">
                <Link
                    to={`/users/${u.id}`}
                    onClick={(e) => e.stopPropagation()}
                    className="font-medium text-[var(--admin-accent-strong)] hover:underline"
                >
                    {fullName(u)}
                </Link>
                {u.admin_permissions > 0 && (
                    <Badge
                        variant="outline"
                        className="gap-0.5 text-[10px] border-[var(--admin-accent)] text-[var(--admin-accent-strong)]"
                    >
                        <ShieldAlert className="size-2.5" />
                        admin
                    </Badge>
                )}
            </div>
        ),
        csv: (u) => fullName(u),
    },
    {
        id: "email",
        header: "Email",
        sortable: true,
        sortKey: "email",
        cell: (u) => <span className="text-muted-foreground">{u.email}</span>,
        csv: (u) => u.email,
    },
    {
        id: "status",
        header: "Status",
        cell: (u) =>
            u.banned_at ? (
                <Badge variant="outline" className="text-[10px] border-red-300 bg-red-50 text-red-700">
                    banned
                </Badge>
            ) : (
                <span className="text-xs text-emerald-600">active</span>
            ),
        csv: (u) => (u.banned_at ? "banned" : "active"),
    },
    { id: "orgs", header: "Orgs", align: "right", cell: (u) => <span className="tabular-nums">{u.organization_count}</span>, csv: (u) => u.organization_count },
    { id: "mailboxes", header: "Mailboxes", align: "right", cell: (u) => <span className="tabular-nums">{u.email_account_count}</span>, csv: (u) => u.email_account_count },
    { id: "campaigns", header: "Campaigns", align: "right", cell: (u) => <span className="tabular-nums">{u.campaign_count}</span>, csv: (u) => u.campaign_count },
    {
        id: "joined",
        header: "Joined",
        cell: (u) => <span className="text-xs text-muted-foreground">{new Date(u.created_at).toLocaleDateString()}</span>,
        csv: (u) => u.created_at,
        defaultHidden: true,
    },
];

export default function UsersPage() {
    const nav = useNavigate();
    const [query, setQuery] = useState("");
    const [status, setStatus] = useState<StatusFilter>("active");
    const [adminOnly, setAdminOnly] = useState(false);
    const [sort, setSort] = useState<{ by: string; desc: boolean }>({ by: "", desc: true });
    const pager = useCursorPager();
    const { reset } = pager;

    // Any filter/sort change returns to page 1.
    useEffect(() => {
        reset();
    }, [query, status, adminOnly, sort.by, sort.desc, reset]);

    const { data, isLoading, error, refetch } = useQuery({
        queryKey: ["admin", "users", { query, status, adminOnly, sort, cursor: pager.cursor }],
        queryFn: () =>
            searchUsers({
                q: query.trim() || undefined,
                status: status === "all" ? "" : status,
                is_admin: adminOnly || undefined,
                limit: 50,
                cursor: pager.cursor,
                sort_by: sort.by ? (sort.by as "name" | "email") : undefined,
                sort_desc: sort.by ? sort.desc : undefined,
            }),
        staleTime: 30_000,
        placeholderData: keepPreviousData,
    });

    const rows = data?.data ?? [];

    return (
        <div>
            <PageHeader title="Users" description="Every account on the platform. Filter, sort, and drill in to ban, unban, override rate limits, or inspect orgs and mailboxes." />
            <Explorer
                activeCount={(query ? 1 : 0) + (status !== "active" ? 1 : 0) + (adminOnly ? 1 : 0) + (sort.by ? 1 : 0)}
                onReset={() => {
                    setQuery("");
                    setStatus("active");
                    setAdminOnly(false);
                    setSort({ by: "", desc: true });
                }}
                filters={
                    <>
                        <FilterGroup label="Search">
                            <SearchFilter value={query} onChange={setQuery} placeholder="Name or email…" />
                        </FilterGroup>
                        <FilterGroup label="Status">
                            <SegmentedFilter
                                value={status}
                                onChange={setStatus}
                                options={[
                                    { value: "active", label: "Active" },
                                    { value: "banned", label: "Banned" },
                                    { value: "all", label: "All" },
                                ]}
                            />
                        </FilterGroup>
                        <FilterGroup label="Access">
                            <ToggleFilter checked={adminOnly} onChange={setAdminOnly} label="Admins only" />
                        </FilterGroup>
                    </>
                }
            >
                <DataTable
                    columns={columns}
                    rows={rows}
                    getRowId={(u) => u.id}
                    loading={isLoading}
                    error={error}
                    onRetry={() => refetch()}
                    errorTitle="Failed to load users"
                    onRowClick={(u) => nav(`/users/${u.id}`)}
                    sort={sort.by ? sort : undefined}
                    onSortChange={setSort}
                    storageKey="admin.users"
                    csvName="warmbly-users"
                    noun="users"
                    emptyTitle="No users"
                    emptyHint="No users match these filters."
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
            </Explorer>
        </div>
    );
}
