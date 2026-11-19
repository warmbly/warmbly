// Table columns for store orders, domains and mailboxes, shared by the
// instance-wide tabs and the workspace drill-down (which drops the workspace column).

import { Link } from "react-router-dom";
import type { Column } from "@/components/data/DataTable";
import { StatusBadge } from "@/components/ui/kit";
import type { AdminStoreDomain, AdminStoreMailbox, AdminStoreOrder } from "@/lib/api/client/admin/store";
import { absolute, relative } from "@/app/dashboard/jobs/format";
import { TONE_TEXT } from "@/lib/tones";
import { cn } from "@/lib/utils";
import { label, providerLabel, statusTone, usd } from "./format";

const linkCls = "truncate text-foreground decoration-border-strong underline-offset-2 hover:text-[var(--admin-accent-strong)] hover:underline";
const DAY_MS = 24 * 60 * 60 * 1000;

function workspaceColumn<T extends { organization_id: string; organization_name: string }>(): Column<T> {
    return {
        id: "workspace",
        header: "Workspace",
        cell: (r) => (
            <Link to={`/store/workspaces/${r.organization_id}`} className={linkCls} onClick={(e) => e.stopPropagation()}>
                {r.organization_name || r.organization_id.slice(0, 8)}
            </Link>
        ),
        csv: (r) => r.organization_name,
    };
}

function when(ts: string | null | undefined) {
    if (!ts) return <span className="text-subtle-foreground">—</span>;
    return (
        <span className="whitespace-nowrap tabular-nums text-muted-foreground" title={absolute(ts)}>
            {relative(ts)}
        </span>
    );
}

function orderSummary(o: AdminStoreOrder): string {
    const domains = o.lines.filter((l) => l.type === "domain").length;
    const mailboxes = o.lines.reduce((n, l) => n + (l.type === "mailboxes" ? (l.count ?? 0) : 0), 0);
    const parts = [];
    if (domains) parts.push(`${domains} domain${domains === 1 ? "" : "s"}`);
    if (mailboxes) parts.push(`${mailboxes} mailbox${mailboxes === 1 ? "" : "es"}`);
    return parts.join(", ") || "Empty";
}

export function orderColumns(withWorkspace: boolean): Column<AdminStoreOrder>[] {
    const cols: Column<AdminStoreOrder>[] = [
        {
            id: "order",
            header: "Order",
            cell: (o) => (
                <div className="min-w-0 py-1">
                    <div className="truncate font-medium">{o.lines[0]?.domain ?? o.id.slice(0, 8)}</div>
                    <div className="text-xs text-muted-foreground">{orderSummary(o)}</div>
                </div>
            ),
            csv: (o) => o.id,
        },
        {
            id: "status",
            header: "Status",
            cell: (o) => (
                <span className="inline-flex flex-wrap items-center gap-1">
                    <StatusBadge tone={statusTone(o.status)} dot>
                        {label(o.status)}
                    </StatusBadge>
                    {o.stuck && <StatusBadge tone="warning">stuck</StatusBadge>}
                </span>
            ),
            csv: (o) => (o.stuck ? `${o.status} (stuck)` : o.status),
        },
        {
            id: "failure",
            header: "Failure",
            cell: (o) => (o.failure_code ? <span className="font-mono text-[12px] text-[var(--admin-danger)]">{o.failure_code}</span> : <span className="text-subtle-foreground">—</span>),
            csv: (o) => o.failure_code,
        },
        { id: "charged", header: "Charged", align: "right", cell: (o) => <span className="tabular-nums">{usd(o.charged_micros)}</span>, csv: (o) => o.charged_micros / 1e6 },
        {
            id: "refunded",
            header: "Refunded",
            align: "right",
            cell: (o) => <span className={cn("tabular-nums", o.refunded_micros ? "" : "text-subtle-foreground")}>{usd(o.refunded_micros)}</span>,
            csv: (o) => o.refunded_micros / 1e6,
        },
        { id: "created", header: "Created", cell: (o) => when(o.created_at), csv: (o) => o.created_at },
    ];
    if (withWorkspace) cols.splice(1, 0, workspaceColumn<AdminStoreOrder>());
    return cols;
}

export function domainColumns(withWorkspace: boolean): Column<AdminStoreDomain>[] {
    const cols: Column<AdminStoreDomain>[] = [
        {
            id: "name",
            header: "Domain",
            cell: (d) => (
                <div className="min-w-0 py-1">
                    <div className="truncate font-medium">{d.name}</div>
                    <div className="text-xs text-muted-foreground">
                        {d.source === "purchased" ? "Purchased" : "Connected"}, {providerLabel(d.provider)}
                    </div>
                </div>
            ),
            csv: (d) => d.name,
        },
        {
            id: "status",
            header: "Status",
            cell: (d) => (
                <span className="inline-flex flex-wrap items-center gap-1">
                    <StatusBadge tone={statusTone(d.status)} dot>
                        {label(d.status)}
                    </StatusBadge>
                    {d.renewal_failures > 0 && <StatusBadge tone="danger">renewal failed</StatusBadge>}
                </span>
            ),
            csv: (d) => d.status,
        },
        {
            id: "expires",
            header: "Expires",
            cell: (d) => {
                if (!d.expires_at) return <span className="text-subtle-foreground">—</span>;
                const soon = new Date(d.expires_at).getTime() - Date.now() < 30 * DAY_MS;
                return (
                    <span className={cn("whitespace-nowrap tabular-nums", soon ? TONE_TEXT.warning : "text-muted-foreground")} title={absolute(d.expires_at)}>
                        {new Date(d.expires_at).toLocaleDateString()}
                    </span>
                );
            },
            csv: (d) => d.expires_at ?? "",
        },
        {
            id: "auto_renew",
            header: "Auto-renew",
            cell: (d) =>
                d.source !== "purchased" ? (
                    <span className="text-subtle-foreground">—</span>
                ) : d.auto_renew ? (
                    <span className="text-muted-foreground">On</span>
                ) : (
                    <StatusBadge tone="warning">Off</StatusBadge>
                ),
            csv: (d) => (d.source === "purchased" ? (d.auto_renew ? "on" : "off") : ""),
        },
        { id: "renew", header: "Renews at", align: "right", cell: (d) => <span className="tabular-nums">{d.source === "purchased" ? usd(d.renew_micros) : "—"}</span>, csv: (d) => d.renew_micros / 1e6 },
        { id: "mailboxes", header: "Mailboxes", align: "right", cell: (d) => <span className="tabular-nums">{d.mailbox_count}</span>, csv: (d) => d.mailbox_count },
    ];
    if (withWorkspace) cols.splice(1, 0, workspaceColumn<AdminStoreDomain>());
    return cols;
}

export function mailboxColumns(withWorkspace: boolean): Column<AdminStoreMailbox>[] {
    const cols: Column<AdminStoreMailbox>[] = [
        {
            id: "email",
            header: "Mailbox",
            cell: (m) => (
                <div className="min-w-0 py-1">
                    <div className="truncate font-medium">{m.email}</div>
                    <div className="text-xs text-muted-foreground">{providerLabel(m.provider)}</div>
                </div>
            ),
            csv: (m) => m.email,
        },
        {
            id: "status",
            header: "Status",
            cell: (m) => (
                <span className="inline-flex flex-wrap items-center gap-1">
                    <StatusBadge tone={statusTone(m.status)} dot>
                        {label(m.status)}
                    </StatusBadge>
                    {m.cancels_at && m.status !== "cancelled" && <StatusBadge tone="warning">cancelling</StatusBadge>}
                </span>
            ),
            csv: (m) => m.status,
        },
        {
            id: "connect",
            header: "Connect",
            cell: (m) => <StatusBadge tone={statusTone(m.connect_state)}>{label(m.connect_state)}</StatusBadge>,
            csv: (m) => m.connect_state,
        },
        {
            id: "billing",
            header: "Billing",
            cell: (m) =>
                m.billing_failures > 0 ? (
                    <StatusBadge tone="danger">{m.billing_failures} failed</StatusBadge>
                ) : m.pending_charge ? (
                    <StatusBadge tone="info">charge pending</StatusBadge>
                ) : (
                    <span className="text-muted-foreground">OK</span>
                ),
            csv: (m) => m.billing_failures,
        },
        { id: "monthly", header: "Monthly", align: "right", cell: (m) => <span className="tabular-nums">{usd(m.monthly_micros)}</span>, csv: (m) => m.monthly_micros / 1e6 },
        {
            id: "paid_through",
            header: "Paid through",
            cell: (m) => (m.paid_through ? <span className="whitespace-nowrap tabular-nums text-muted-foreground">{new Date(m.paid_through).toLocaleDateString()}</span> : <span className="text-subtle-foreground">—</span>),
            csv: (m) => m.paid_through ?? "",
        },
    ];
    if (withWorkspace) cols.splice(1, 0, workspaceColumn<AdminStoreMailbox>());
    return cols;
}
