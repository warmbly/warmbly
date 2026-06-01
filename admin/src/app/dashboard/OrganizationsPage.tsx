// Organizations browser — left filter rail + server-driven sortable,
// cursor-paged table. Search name/slug/owner, filter by status, sort by name.
// The Mailboxes cell links into the Mailboxes browser pre-filtered to the org.

import { useEffect, useState } from "react";
import { useQuery, keepPreviousData } from "@tanstack/react-query";
import { Link, useNavigate } from "react-router-dom";
import { PageHeader } from "@/components/layout/PageHeader";
import { Badge } from "@/components/ui/badge";
import { Explorer, FilterGroup, SearchFilter, SegmentedFilter } from "@/components/data/Explorer";
import { DataTable, type Column } from "@/components/data/DataTable";
import { useCursorPager } from "@/lib/useCursorPager";
import { listOrganizations } from "@/lib/api/client/admin/organizations";
import type { AdminOrgListItem } from "@/lib/api/models/admin";

type StatusFilter = "active" | "pending_deletion" | "all";

const ownerName = (o: AdminOrgListItem) =>
    `${o.owner_first_name} ${o.owner_last_name}`.trim() || o.owner_email;

const columns: Column<AdminOrgListItem>[] = [
    {
        id: "name",
        header: "Name",
        sortable: true,
        sortKey: "name",
        cell: (o) => (
            <div>
                <Link
                    to={`/organizations/${o.id}`}
                    onClick={(e) => e.stopPropagation()}
                    className="font-medium text-[var(--admin-accent-strong)] hover:underline"
                >
                    {o.name}
                </Link>
                {o.slug && <div className="font-mono text-[10px] text-muted-foreground">{o.slug}</div>}
            </div>
        ),
        csv: (o) => o.name,
    },
    {
        id: "owner",
        header: "Owner",
        cell: (o) => (
            <div>
                <div className="flex items-center gap-1.5">
                    <span className="text-xs">{ownerName(o)}</span>
                    {o.owner_banned_at && (
                        <Badge variant="outline" className="text-[10px] border-red-300 bg-red-50 text-red-700">
                            banned
                        </Badge>
                    )}
                </div>
                <div className="text-[10px] text-muted-foreground">{o.owner_email}</div>
            </div>
        ),
        csv: (o) => o.owner_email,
    },
    { id: "members", header: "Members", align: "right", cell: (o) => <span className="tabular-nums">{o.member_count}</span>, csv: (o) => o.member_count },
    {
        id: "mailboxes",
        header: "Mailboxes",
        align: "right",
        cell: (o) => (
            <Link
                to={`/mailboxes?org=${o.id}`}
                onClick={(e) => e.stopPropagation()}
                className="tabular-nums text-[var(--admin-accent-strong)] hover:underline"
                title="Browse this org's mailboxes"
            >
                {o.email_account_count}
            </Link>
        ),
        csv: (o) => o.email_account_count,
    },
    {
        id: "campaigns",
        header: "Campaigns",
        align: "right",
        cell: (o) => (
            <span className="tabular-nums">
                {o.campaign_count}
                {o.active_campaigns > 0 && <span className="ml-1 text-emerald-600">({o.active_campaigns} active)</span>}
            </span>
        ),
        csv: (o) => o.campaign_count,
    },
    {
        id: "status",
        header: "Status",
        cell: (o) =>
            o.deletion_scheduled_for ? (
                <Badge variant="outline" className="text-[10px] border-amber-300 bg-amber-50 text-amber-700">
                    pending deletion
                </Badge>
            ) : (
                <span className="text-xs text-emerald-600">active</span>
            ),
        csv: (o) => (o.deletion_scheduled_for ? "pending_deletion" : "active"),
    },
    {
        id: "created",
        header: "Created",
        cell: (o) => <span className="text-xs text-muted-foreground">{new Date(o.created_at).toLocaleDateString()}</span>,
        csv: (o) => o.created_at,
        defaultHidden: true,
    },
];

export default function OrganizationsPage() {
    const nav = useNavigate();
    const [query, setQuery] = useState("");
    const [status, setStatus] = useState<StatusFilter>("active");
    const [sort, setSort] = useState<{ by: string; desc: boolean }>({ by: "", desc: true });
    const pager = useCursorPager();
    const { reset } = pager;

    useEffect(() => {
        reset();
    }, [query, status, sort.by, sort.desc, reset]);

    const { data, isLoading, error, refetch } = useQuery({
        queryKey: ["admin", "organizations", { query, status, sort, cursor: pager.cursor }],
        queryFn: () =>
            listOrganizations({
                q: query.trim() || undefined,
                status: status === "all" ? "" : status,
                limit: 50,
                cursor: pager.cursor,
                sort_by: sort.by ? (sort.by as "name") : undefined,
                sort_desc: sort.by ? sort.desc : undefined,
            }),
        staleTime: 30_000,
        placeholderData: keepPreviousData,
    });

    const rows = data?.data ?? [];

    return (
        <div>
            <PageHeader title="Organizations" description="Every workspace on the platform. Filter, sort, and drill into owner, usage, members, and mailboxes." />
            <Explorer
                activeCount={(query ? 1 : 0) + (status !== "active" ? 1 : 0) + (sort.by ? 1 : 0)}
                onReset={() => {
                    setQuery("");
                    setStatus("active");
                    setSort({ by: "", desc: true });
                }}
                filters={
                    <>
                        <FilterGroup label="Search">
                            <SearchFilter value={query} onChange={setQuery} placeholder="Name, slug, or owner…" />
                        </FilterGroup>
                        <FilterGroup label="Status">
                            <SegmentedFilter
                                value={status}
                                onChange={setStatus}
                                options={[
                                    { value: "active", label: "Active" },
                                    { value: "pending_deletion", label: "Pending" },
                                    { value: "all", label: "All" },
                                ]}
                            />
                        </FilterGroup>
                    </>
                }
            >
                <DataTable
                    columns={columns}
                    rows={rows}
                    getRowId={(o) => o.id}
                    loading={isLoading}
                    error={error}
                    onRetry={() => refetch()}
                    errorTitle="Failed to load organizations"
                    onRowClick={(o) => nav(`/organizations/${o.id}`)}
                    sort={sort.by ? sort : undefined}
                    onSortChange={setSort}
                    storageKey="admin.organizations"
                    csvName="warmbly-organizations"
                    noun="organizations"
                    emptyTitle="No organizations"
                    emptyHint="No organizations match these filters."
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
