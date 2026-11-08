// Agencies: plan, status, clients and how much of the shared send pool each has used this period.

import { useState } from "react";
import { keepPreviousData, useQuery } from "@tanstack/react-query";
import { useNavigate } from "react-router-dom";
import { PageHeader } from "@/components/layout/PageHeader";
import { DataTable, type Column } from "@/components/data/DataTable";
import { StatusBadge } from "@/components/ui/kit";
import { Input } from "@/components/ui/input";
import { listAgencies, type AdminAgency } from "@/lib/api/client/admin/agencies";
import { AGENCY_STATUS_TONE, poolLabel } from "./agencyFormat";

const PAGE = 50;

const COLUMNS: Column<AdminAgency>[] = [
    { id: "name", header: "Agency", cell: (a) => <span className="font-medium">{a.name}</span>, csv: (a) => a.name },
    { id: "owner", header: "Owner", cell: (a) => <span className="text-muted-foreground">{a.owner_email}</span>, csv: (a) => a.owner_email },
    { id: "plan", header: "Plan", cell: (a) => a.plan?.name ?? "None", csv: (a) => a.plan?.name ?? "" },
    {
        id: "status",
        header: "Status",
        cell: (a) => <StatusBadge tone={AGENCY_STATUS_TONE[a.status] ?? "neutral"}>{a.status.replace(/_/g, " ")}</StatusBadge>,
        csv: (a) => a.status,
    },
    { id: "clients", header: "Clients", align: "right", cell: (a) => a.client_count.toLocaleString(), csv: (a) => a.client_count },
    { id: "pool", header: "Pool this period", align: "right", cell: (a) => <span className="tabular-nums">{poolLabel(a)}</span>, csv: (a) => a.pool_used },
];

export default function AgenciesPage() {
    const nav = useNavigate();
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
            </PageHeader>
            <DataTable
                columns={COLUMNS}
                rows={rows}
                getRowId={(a) => a.id}
                loading={q.isLoading}
                error={q.error}
                onRetry={() => void q.refetch()}
                onRowClick={(a) => nav(`/agencies/${a.id}`)}
                emptyTitle="No agencies"
                emptyHint="Agencies appear here once someone creates one from the dashboard."
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
