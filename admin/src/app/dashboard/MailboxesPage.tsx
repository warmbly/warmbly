// Cross-org mailbox browser. Search, filter by status, and — via the
// ?org=<id> query param (linked from the Organizations browser) — scope to a
// single org's mailboxes. Cursor-paged + CSV export.

import { useEffect, useState } from "react";
import { useQuery, keepPreviousData } from "@tanstack/react-query";
import { Link, useSearchParams } from "react-router-dom";
import { X } from "lucide-react";
import { PageHeader } from "@/components/layout/PageHeader";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Explorer, FilterGroup, SearchFilter, SegmentedFilter } from "@/components/data/Explorer";
import { DataTable, type Column } from "@/components/data/DataTable";
import { useCursorPager } from "@/lib/useCursorPager";
import { searchMailboxes } from "@/lib/api/client/admin/mailboxes";
import type { AdminMailboxRow } from "@/lib/api/models/admin";

type StatusFilter = "active" | "inactive" | "all";
const STALE_MS = 24 * 60 * 60 * 1000;

const columns: Column<AdminMailboxRow>[] = [
    {
        id: "email",
        header: "Mailbox",
        cell: (m) => (
            <div>
                <div className="font-medium">{m.email}</div>
                <div className="text-[10px] uppercase tracking-wide text-muted-foreground">{m.provider}</div>
            </div>
        ),
        csv: (m) => m.email,
    },
    {
        id: "owner",
        header: "Owner",
        cell: (m) => (
            <Link to={`/users/${m.user_id}`} className="text-xs text-[var(--admin-accent-strong)] hover:underline">
                {m.owner_email}
            </Link>
        ),
        csv: (m) => m.owner_email,
    },
    {
        id: "org",
        header: "Organization",
        cell: (m) =>
            m.organization_id ? (
                <Link to={`/organizations/${m.organization_id}`} className="text-xs text-[var(--admin-accent-strong)] hover:underline">
                    {m.org_name || m.organization_id}
                </Link>
            ) : (
                <span className="text-xs text-muted-foreground">—</span>
            ),
        csv: (m) => m.org_name || "",
    },
    {
        id: "status",
        header: "Status",
        cell: (m) =>
            m.status === "active" ? (
                <span className="text-xs text-emerald-600">active</span>
            ) : (
                <Badge variant="outline" className="text-[10px] border-zinc-300 text-zinc-600">
                    {m.status}
                </Badge>
            ),
        csv: (m) => m.status,
    },
    {
        id: "warmup",
        header: "Warmup",
        cell: (m) =>
            m.warmup_enabled ? (
                <Badge variant="outline" className="text-[10px] border-amber-300 bg-amber-50 text-amber-700">
                    on
                </Badge>
            ) : (
                <span className="text-xs text-muted-foreground">off</span>
            ),
        csv: (m) => (m.warmup_enabled ? "on" : "off"),
    },
    { id: "limit", header: "Daily cap", align: "right", cell: (m) => <span className="tabular-nums">{m.campaign_limit}</span>, csv: (m) => m.campaign_limit },
    {
        id: "synced",
        header: "Last synced",
        cell: (m) => {
            if (!m.last_synced_at) return <span className="text-xs text-muted-foreground">never</span>;
            const stale = Date.now() - new Date(m.last_synced_at).getTime() > STALE_MS;
            return (
                <span className={stale ? "text-xs text-amber-700" : "text-xs text-muted-foreground"}>
                    {new Date(m.last_synced_at).toLocaleString()}
                    {stale && " ⚠"}
                </span>
            );
        },
        csv: (m) => m.last_synced_at || "",
    },
    {
        id: "created",
        header: "Connected",
        cell: (m) => <span className="text-xs text-muted-foreground">{new Date(m.created_at).toLocaleDateString()}</span>,
        csv: (m) => m.created_at,
        defaultHidden: true,
    },
];

export default function MailboxesPage() {
    const [params, setParams] = useSearchParams();
    const orgId = params.get("org") || undefined;
    const [query, setQuery] = useState("");
    const [status, setStatus] = useState<StatusFilter>("active");
    const pager = useCursorPager();
    const { reset } = pager;

    useEffect(() => {
        reset();
    }, [query, status, orgId, reset]);

    const { data, isLoading, error, refetch } = useQuery({
        queryKey: ["admin", "mailboxes", { query, status, orgId, cursor: pager.cursor }],
        queryFn: () =>
            searchMailboxes({
                q: query.trim() || undefined,
                status,
                org_id: orgId,
                cursor: pager.cursor,
                limit: 50,
            }),
        staleTime: 30_000,
        placeholderData: keepPreviousData,
    });

    const rows = data?.data ?? [];

    function clearOrg() {
        const next = new URLSearchParams(params);
        next.delete("org");
        setParams(next, { replace: true });
    }

    return (
        <div>
            <PageHeader title="Mailboxes" description="Every connected mailbox across the platform. Filter, scope to an org, and export." />
            <Explorer
                activeCount={(query ? 1 : 0) + (status !== "active" ? 1 : 0) + (orgId ? 1 : 0)}
                onReset={() => {
                    setQuery("");
                    setStatus("active");
                    clearOrg();
                }}
                filters={
                    <>
                        <FilterGroup label="Search">
                            <SearchFilter value={query} onChange={setQuery} placeholder="Email, owner, or org…" />
                        </FilterGroup>
                        <FilterGroup label="Status">
                            <SegmentedFilter
                                value={status}
                                onChange={setStatus}
                                options={[
                                    { value: "active", label: "Active" },
                                    { value: "inactive", label: "Inactive" },
                                    { value: "all", label: "All" },
                                ]}
                            />
                        </FilterGroup>
                        {orgId && (
                            <FilterGroup label="Scope">
                                <div className="flex items-center justify-between rounded-md border border-[var(--admin-accent)]/30 bg-[var(--admin-accent-soft)]/40 px-2 py-1.5 text-[12px]">
                                    <span className="truncate">One organization</span>
                                    <Button variant="ghost" size="icon-xs" onClick={clearOrg} title="Clear org filter">
                                        <X className="size-3" />
                                    </Button>
                                </div>
                            </FilterGroup>
                        )}
                    </>
                }
            >
                <DataTable
                    columns={columns}
                    rows={rows}
                    getRowId={(m) => m.id}
                    loading={isLoading}
                    error={error}
                    onRetry={() => refetch()}
                    errorTitle="Failed to load mailboxes"
                    storageKey="admin.mailboxes"
                    csvName="warmbly-mailboxes"
                    noun="mailboxes"
                    emptyTitle="No mailboxes"
                    emptyHint="No mailboxes match these filters."
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
