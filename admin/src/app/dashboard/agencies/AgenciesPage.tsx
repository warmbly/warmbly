// Agencies: plan, status, clients and how much of the shared send pool each has used this period.

import { useState } from "react";
import { keepPreviousData, useQuery } from "@tanstack/react-query";
import { useNavigate } from "react-router-dom";
import { Plus } from "lucide-react";
import { PageHeader } from "@/components/layout/PageHeader";
import { DataTable, type Column } from "@/components/data/DataTable";
import { StatusBadge } from "@/components/ui/kit";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { useAdminPerm } from "@/hooks/useAdminPerm";
import { AdminPerm } from "@/lib/auth/permissions";
import { listAgencies, type AdminAgency } from "@/lib/api/client/admin/agencies";
import { AGENCY_STATUS_TONE, poolLabel } from "./agencyFormat";
import { NewAgencyDialog } from "./NewAgencyDialog";

const PAGE = 50;

const COLUMNS: Column<AdminAgency>[] = [
    { id: "name", header: "Agency", cell: (a) => <span className="font-medium">{a.name}</span>, csv: (a) => a.name },
    { id: "owner", header: "Owner", cell: (a) => <span className="text-muted-foreground">{a.owner_email}</span>, csv: (a) => a.owner_email },
    {
        id: "plan",
        header: "Plan",
        cell: (a) => (
            <span className="inline-flex items-center gap-1.5">
                {a.plan?.name ?? "None"}
                {/* A plan an operator gave, rather than one bought through Stripe. */}
                {a.granted ? (
                    <StatusBadge tone="info" title="Given by an operator, not bought through Stripe">
                        Given
                    </StatusBadge>
                ) : (
                    a.managed_plan && (
                        <StatusBadge tone="warning" title="The plan an operator gave has lapsed">
                            Lapsed
                        </StatusBadge>
                    )
                )}
            </span>
        ),
        csv: (a) => a.plan?.name ?? "",
    },
    { id: "source", header: "Plan source", defaultHidden: true, cell: (a) => planSource(a), csv: (a) => planSource(a) },
    {
        id: "status",
        header: "Status",
        cell: (a) => <StatusBadge tone={AGENCY_STATUS_TONE[a.status] ?? "neutral"}>{a.status.replace(/_/g, " ")}</StatusBadge>,
        csv: (a) => a.status,
    },
    { id: "clients", header: "Clients", align: "right", cell: (a) => a.client_count.toLocaleString(), csv: (a) => a.client_count },
    { id: "pool", header: "Pool this period", align: "right", cell: (a) => <span className="tabular-nums">{poolLabel(a)}</span>, csv: (a) => a.pool_used },
];

function planSource(a: AdminAgency): string {
    if (a.granted) return "Operator";
    return a.plan_id ? "Stripe" : "None";
}

export default function AgenciesPage() {
    const nav = useNavigate();
    const canManage = useAdminPerm(AdminPerm.ManageOrganizations);
    const [creating, setCreating] = useState(false);
    const [search, setSearch] = useState("");
    const [page, setPage] = useState(0);
    const q = useQuery({
        queryKey: ["admin", "agencies", search, page],
        queryFn: () => listAgencies({ search: search.trim(), limit: PAGE, offset: page * PAGE }),
        placeholderData: keepPreviousData,
    });
    const rows = q.data?.data ?? [];
    const total = q.data?.total ?? 0;

    return (
        <>
            <PageHeader title="Agencies" description="Agencies, their plan, clients and shared monthly send pool." meta={total ? total.toLocaleString() : undefined}>
                <Input
                    value={search}
                    onChange={(e) => {
                        setSearch(e.target.value);
                        setPage(0);
                    }}
                    placeholder="Search name or owner email"
                    className="h-8 w-64"
                />
                {canManage && (
                    <Button size="sm" onClick={() => setCreating(true)}>
                        <Plus className="size-3.5" /> New agency
                    </Button>
                )}
            </PageHeader>
            <NewAgencyDialog open={creating && canManage} onOpenChange={setCreating} />
            <DataTable
                columns={COLUMNS}
                rows={rows}
                getRowId={(a) => a.id}
                loading={q.isLoading}
                error={q.error}
                onRetry={() => void q.refetch()}
                onRowClick={(a) => nav(`/agencies/${a.id}`)}
                emptyTitle="No agencies"
                emptyHint="Agencies appear here once someone creates one from the dashboard, or an operator opens one here."
                csvName="agencies"
                noun="agencies"
                pager={{
                    canPrev: page > 0,
                    canNext: (page + 1) * PAGE < total,
                    onPrev: () => setPage((p) => Math.max(0, p - 1)),
                    onNext: () => setPage((p) => p + 1),
                    page: page + 1,
                    shown: rows.length,
                    total,
                }}
            />
        </>
    );
}
