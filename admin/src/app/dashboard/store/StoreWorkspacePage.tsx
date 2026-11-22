// One workspace's store: its GraphKit account, balance and spend, and every
// order, domain and mailbox it holds, with the same drawers and actions as
// the instance-wide lists.

import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { Link, useParams } from "react-router-dom";
import { PageHeader } from "@/components/layout/PageHeader";
import { Skeleton } from "@/components/ui/skeleton";
import { Property, PropertyList, Section, StatusBadge } from "@/components/ui/kit";
import { DataTable } from "@/components/data/DataTable";
import { ErrorState } from "@/components/ErrorState";
import {
    getStoreWorkspace,
    type AdminStoreDomain,
    type AdminStoreMailbox,
    type AdminStoreOrder,
} from "@/lib/api/client/admin/store";
import { domainColumns, mailboxColumns, orderColumns } from "./columns";
import { DomainDrawer, MailboxDrawer, OrderDrawer } from "./StoreDrawers";
import { storeLink, usd } from "./format";

const CRUMBS = [{ label: "Store", to: "/store" }];
const ORDER_COLUMNS = orderColumns(false);
const DOMAIN_COLUMNS = domainColumns(false);
const MAILBOX_COLUMNS = mailboxColumns(false);
const linkCls = "underline decoration-border-strong underline-offset-2 hover:text-[var(--admin-accent-strong)]";

export default function StoreWorkspacePage() {
    const { orgId = "" } = useParams<{ orgId: string }>();
    const [order, setOrder] = useState<AdminStoreOrder | null>(null);
    const [domain, setDomain] = useState<AdminStoreDomain | null>(null);
    const [mailbox, setMailbox] = useState<AdminStoreMailbox | null>(null);
    const q = useQuery({ queryKey: ["admin", "store", "workspace", orgId], queryFn: () => getStoreWorkspace(orgId), enabled: !!orgId });

    if (q.isLoading) {
        return (
            <div>
                <PageHeader breadcrumbs={CRUMBS} title={<Skeleton className="h-4 w-40" />} />
                <Skeleton className="h-64 w-full rounded-xl" />
            </div>
        );
    }
    if (q.error || !q.data) {
        return (
            <div>
                <PageHeader breadcrumbs={CRUMBS} title="Workspace" />
                <ErrorState error={q.error} title="Failed to load this workspace's store" onRetry={() => q.refetch()} />
            </div>
        );
    }

    const w = q.data;
    return (
        <div>
            <PageHeader breadcrumbs={CRUMBS} title={w.organization.name || "Workspace"} />
            <div className="grid gap-8 lg:grid-cols-[minmax(0,1fr)_17rem] lg:gap-10">
                <div className="order-2 min-w-0 lg:order-1">
                    <Section title={<Count label="Orders" n={w.orders.length} />} description="The latest 50." className="mt-0">
                        <DataTable
                            columns={ORDER_COLUMNS}
                            rows={w.orders}
                            getRowId={(o) => o.id}
                            onRowClick={setOrder}
                            selectedRowId={order?.id ?? null}
                            storageKey="admin.store.ws.orders"
                            csvName="warmbly-store-orders"
                            noun="orders"
                            emptyTitle="No orders"
                            emptyHint="This workspace has not bought anything."
                        />
                    </Section>
                    <Section title={<Count label="Domains" n={w.domains.length} />}>
                        <DataTable
                            columns={DOMAIN_COLUMNS}
                            rows={w.domains}
                            getRowId={(d) => d.id}
                            onRowClick={setDomain}
                            selectedRowId={domain?.id ?? null}
                            storageKey="admin.store.ws.domains"
                            csvName="warmbly-store-domains"
                            noun="domains"
                            emptyTitle="No domains"
                            emptyHint="No purchased or connected domains."
                        />
                    </Section>
                    <Section title={<Count label="Mailboxes" n={w.mailboxes.length} />}>
                        <DataTable
                            columns={MAILBOX_COLUMNS}
                            rows={w.mailboxes}
                            getRowId={(m) => m.id}
                            onRowClick={setMailbox}
                            selectedRowId={mailbox?.id ?? null}
                            storageKey="admin.store.ws.mailboxes"
                            csvName="warmbly-store-mailboxes"
                            noun="mailboxes"
                            emptyTitle="No mailboxes"
                            emptyHint="No purchased mailboxes."
                        />
                    </Section>
                </div>

                <aside className="order-1 min-w-0 lg:order-2">
                    <div className="lg:sticky lg:top-16">
                        <PropertyList title="Account" className="border-b border-border">
                            <Property label="GraphKit account">
                                {w.account.exists ? (
                                    <span title={w.account.created_at ? new Date(w.account.created_at).toLocaleString() : undefined}>
                                        Created {w.account.created_at ? new Date(w.account.created_at).toLocaleDateString() : ""}
                                    </span>
                                ) : (
                                    <span className="text-subtle-foreground">None yet</span>
                                )}
                            </Property>
                            <Property label="Registrant">{w.account.has_registrant ? "Saved" : <span className="text-subtle-foreground">Not saved</span>}</Property>
                            <Property label="Plan">
                                {w.plan_active ? <StatusBadge tone="success" dot>active</StatusBadge> : <StatusBadge tone="warning" dot>inactive</StatusBadge>}
                            </Property>
                            <Property label="Managed by partner">{w.managed_by_partner ? "Yes" : "No"}</Property>
                        </PropertyList>
                        <PropertyList title="Money" className="mt-5 border-b border-border">
                            <Property label="Balance">{w.balance_micros == null ? <span className="text-subtle-foreground">—</span> : usd(w.balance_micros)}</Property>
                            <Property label="Monthly spend">{usd(w.monthly_spend_micros)}</Property>
                        </PropertyList>
                        <div className="mt-4 flex flex-col gap-1.5 text-[13px]">
                            <Link to={`/organizations/${w.organization.id}`} className={linkCls}>
                                Open organization
                            </Link>
                            <Link to={storeLink("orders", { org: w.organization.id })} className={linkCls}>
                                All orders in the store list
                            </Link>
                        </div>
                    </div>
                </aside>
            </div>

            <OrderDrawer order={order} onClose={() => setOrder(null)} />
            <DomainDrawer domain={domain} onClose={() => setDomain(null)} />
            <MailboxDrawer mailbox={mailbox} onClose={() => setMailbox(null)} />
        </div>
    );
}

function Count({ label, n }: { label: string; n: number }) {
    return (
        <span className="inline-flex items-center gap-1.5">
            {label}
            <span className="font-normal tabular-nums text-muted-foreground">{n}</span>
        </span>
    );
}
