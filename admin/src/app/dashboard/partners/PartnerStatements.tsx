// A partner's monthly statement: enabled mailbox-days per workspace at its wholesale rates. Closed months are final.

import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { Panel, Property, PropertyList, StatusBadge } from "@/components/ui/kit";
import { DataTable, type Column } from "@/components/data/DataTable";
import { getPartnerStatement, listPartnerStatements, type AdminPartnerStatementLine } from "@/lib/api/client/admin/partners";

function usd(m: number): string {
    return (m / 1_000_000).toLocaleString(undefined, { style: "currency", currency: "USD", minimumFractionDigits: 2, maximumFractionDigits: 6 });
}

function thisMonth(): string {
    return new Date().toISOString().slice(0, 7);
}

const LINE_COLUMNS: Column<AdminPartnerStatementLine>[] = [
    { id: "external", header: "Partner's workspace id", cell: (l) => <span className="font-mono text-xs">{l.external_id}</span>, csv: (l) => l.external_id },
    { id: "sw", header: "Sending & warmup days", align: "right", cell: (l) => l.sending_warmup_days, csv: (l) => l.sending_warmup_days },
    { id: "wo", header: "Warmup only days", align: "right", cell: (l) => l.warmup_only_days, csv: (l) => l.warmup_only_days },
    { id: "amount", header: "Amount", align: "right", cell: (l) => usd(l.amount_micros), csv: (l) => l.amount_micros / 1_000_000 },
];

export function PartnerStatements({ partnerId }: { partnerId: string }) {
    const [month, setMonth] = useState(thisMonth);
    const closed = useQuery({ queryKey: ["admin", "partners", partnerId, "statements"], queryFn: () => listPartnerStatements(partnerId) });
    const st = useQuery({ queryKey: ["admin", "partners", partnerId, "statements", month], queryFn: () => getPartnerStatement(partnerId, month) });
    const months = Array.from(new Set([thisMonth(), ...(closed.data?.data ?? []).map((s) => s.month)]));
    const s = st.data;
    return (
        <Panel
            title="Statement"
            description="Live enabled mailbox-days per workspace at the partner's wholesale rates. A month is final a day after it ends."
            actions={
                <select
                    aria-label="Month"
                    value={month}
                    onChange={(e) => setMonth(e.target.value)}
                    className="h-8 rounded-md border border-input bg-background px-2 text-sm"
                >
                    {months.map((m) => (
                        <option key={m} value={m}>
                            {m}
                        </option>
                    ))}
                </select>
            }
            bodyClassName="p-0"
        >
            {s && (
                <PropertyList className="border-b border-border">
                    <Property label="Status">
                        <StatusBadge tone={s.closed_at ? "success" : "info"}>{s.closed_at ? "Closed" : "Running"}</StatusBadge>
                    </Property>
                    <Property label="Sending & warmup">
                        {s.sending_warmup_days} days at {usd(s.sending_warmup_day_micros)}
                    </Property>
                    <Property label="Warmup only">
                        {s.warmup_only_days} days at {usd(s.warmup_only_day_micros)}
                    </Property>
                    <Property label="Total">{usd(s.amount_micros)}</Property>
                </PropertyList>
            )}
            <DataTable
                columns={LINE_COLUMNS}
                rows={s?.lines ?? []}
                getRowId={(l) => l.external_id}
                loading={st.isLoading}
                error={st.error}
                onRetry={() => void st.refetch()}
                emptyTitle="No enabled mailboxes"
                emptyHint="Days count once the partner turns a mailbox on."
                csvName={`partner-statement-${month}`}
                noun="workspaces"
            />
        </Panel>
    );
}
