// The instance-wide Orders, Domains and Mailboxes tabs. Filters live in the
// query string, so a count on the Health tab links straight to its list.

import { useEffect, useState } from "react";
import { keepPreviousData, useQuery } from "@tanstack/react-query";
import { useSearchParams } from "react-router-dom";
import { Building2, X } from "lucide-react";
import { Button } from "@/components/ui/button";
import { DataTable } from "@/components/data/DataTable";
import { Explorer, FilterGroup, SearchFilter, SegmentedFilter, SelectFilter, ToggleFilter } from "@/components/data/Explorer";
import { Input } from "@/components/ui/input";
import { useCursorPager } from "@/lib/useCursorPager";
import {
    listStoreDomains,
    listStoreMailboxes,
    listStoreOrders,
    type AdminStoreDomain,
    type AdminStoreMailbox,
    type AdminStoreOrder,
} from "@/lib/api/client/admin/store";
import { CONNECT_STATES, DOMAIN_STATUSES, MAILBOX_STATUSES, ORDER_STATUSES, label } from "./format";
import { domainColumns, mailboxColumns, orderColumns } from "./columns";
import { DomainDrawer, MailboxDrawer, OrderDrawer } from "./StoreDrawers";

const PAGE = 50;

// Filter state read from and written to ?key=value, keeping ?tab.
function useParamFilters(keys: string[]) {
    const [params, setParams] = useSearchParams();
    const get = (k: string) => params.get(k) ?? "";
    const set = (k: string, v: string) =>
        setParams(
            (p) => {
                if (v) p.set(k, v);
                else p.delete(k);
                return p;
            },
            { replace: true },
        );
    const reset = () =>
        setParams(
            (p) => {
                for (const k of keys) p.delete(k);
                return p;
            },
            { replace: true },
        );
    const active = keys.filter((k) => params.get(k)).length;
    const signature = keys.map((k) => `${k}=${params.get(k) ?? ""}`).join("&");
    return { get, set, reset, active, signature };
}

function anyOption(noun: string, values: readonly string[]) {
    return [{ value: "any", label: `Any ${noun}` }, ...values.map((v) => ({ value: v, label: label(v) }))];
}

function WorkspaceScope({ onClear }: { onClear: () => void }) {
    return (
        <FilterGroup label="Scope">
            <div className="flex h-7 items-center gap-1.5 rounded-md border border-[color-mix(in_oklab,var(--admin-accent)_30%,transparent)] bg-[var(--admin-accent-weak)] pr-0.5 pl-2 text-[12.5px] text-[var(--admin-accent-strong)]">
                <Building2 className="size-3.5 shrink-0" />
                <span className="min-w-0 flex-1 truncate font-medium">One workspace</span>
                <Button variant="ghost" size="icon-xs" onClick={onClear} title="Clear workspace filter" className="text-[var(--admin-accent-strong)] hover:bg-[var(--admin-accent-soft)]">
                    <X className="size-3" />
                </Button>
            </div>
        </FilterGroup>
    );
}

// ---- Orders ------------------------------------------------------------------

const ORDER_KEYS = ["q", "org", "status", "failure_code", "stuck"];
const ORDER_COLUMNS = orderColumns(true);

export function OrdersTab() {
    const f = useParamFilters(ORDER_KEYS);
    const pager = useCursorPager();
    const { reset } = pager;
    const [open, setOpen] = useState<AdminStoreOrder | null>(null);
    useEffect(() => reset(), [f.signature, reset]);

    const q = useQuery({
        queryKey: ["admin", "store", "orders", f.signature, pager.cursor],
        queryFn: () =>
            listStoreOrders({
                q: f.get("q").trim() || undefined,
                org_id: f.get("org") || undefined,
                status: f.get("status") || undefined,
                failure_code: f.get("failure_code").trim() || undefined,
                stuck: f.get("stuck") === "true" || undefined,
                limit: PAGE,
                cursor: pager.cursor,
            }),
        placeholderData: keepPreviousData,
        staleTime: 30_000,
    });
    const rows = q.data?.data ?? [];

    return (
        <Explorer
            activeCount={f.active}
            onReset={f.reset}
            filters={
                <>
                    <FilterGroup label="Search">
                        <SearchFilter value={f.get("q")} onChange={(v) => f.set("q", v)} placeholder="Order id or workspace…" />
                    </FilterGroup>
                    {f.get("org") && <WorkspaceScope onClear={() => f.set("org", "")} />}
                    <FilterGroup label="Status">
                        <SelectFilter value={f.get("status") || "any"} onChange={(v) => f.set("status", v === "any" ? "" : v)} options={anyOption("status", ORDER_STATUSES)} />
                        <ToggleFilter checked={f.get("stuck") === "true"} onChange={(c) => f.set("stuck", c ? "true" : "")} label="Stuck only" />
                    </FilterGroup>
                    <FilterGroup label="Failure code">
                        <Input value={f.get("failure_code")} onChange={(e) => f.set("failure_code", e.target.value)} placeholder="e.g. domain_unavailable" className="h-8 font-mono text-[12px]" />
                    </FilterGroup>
                </>
            }
        >
            <DataTable
                columns={ORDER_COLUMNS}
                rows={rows}
                getRowId={(o) => o.id}
                loading={q.isLoading}
                error={q.error}
                onRetry={() => q.refetch()}
                onRowClick={setOpen}
                selectedRowId={open?.id ?? null}
                errorTitle="Failed to load store orders"
                storageKey="admin.store.orders"
                csvName="warmbly-store-orders"
                noun="orders"
                emptyTitle="No orders"
                emptyHint="No store orders match these filters."
                pager={{
                    canPrev: pager.canPrev,
                    canNext: !!q.data?.pagination.has_more,
                    onPrev: pager.prev,
                    onNext: () => pager.next(q.data?.pagination.next_cursor),
                    page: pager.page,
                    shown: rows.length,
                }}
            />
            <OrderDrawer order={open} onClose={() => setOpen(null)} />
        </Explorer>
    );
}

// ---- Domains -----------------------------------------------------------------

const DOMAIN_KEYS = ["q", "org", "status", "auto_renew", "expiring_days", "renewal_failed"];
const DOMAIN_COLUMNS = domainColumns(true);
const EXPIRING_OPTIONS = [
    { value: "any", label: "Any time" },
    { value: "7", label: "Within 7 days" },
    { value: "30", label: "Within 30 days" },
    { value: "60", label: "Within 60 days" },
    { value: "90", label: "Within 90 days" },
];

export function DomainsTab() {
    const f = useParamFilters(DOMAIN_KEYS);
    const pager = useCursorPager();
    const { reset } = pager;
    const [open, setOpen] = useState<AdminStoreDomain | null>(null);
    useEffect(() => reset(), [f.signature, reset]);

    const autoRenew = f.get("auto_renew");
    const expiring = Number(f.get("expiring_days"));
    const q = useQuery({
        queryKey: ["admin", "store", "domains", f.signature, pager.cursor],
        queryFn: () =>
            listStoreDomains({
                q: f.get("q").trim() || undefined,
                org_id: f.get("org") || undefined,
                status: f.get("status") || undefined,
                auto_renew: autoRenew === "true" || autoRenew === "false" ? autoRenew : undefined,
                expiring_days: expiring > 0 ? expiring : undefined,
                renewal_failed: f.get("renewal_failed") === "true" || undefined,
                limit: PAGE,
                cursor: pager.cursor,
            }),
        placeholderData: keepPreviousData,
        staleTime: 30_000,
    });
    const rows = q.data?.data ?? [];
    const expiringValue = f.get("expiring_days");

    return (
        <Explorer
            activeCount={f.active}
            onReset={f.reset}
            filters={
                <>
                    <FilterGroup label="Search">
                        <SearchFilter value={f.get("q")} onChange={(v) => f.set("q", v)} placeholder="Domain…" />
                    </FilterGroup>
                    {f.get("org") && <WorkspaceScope onClear={() => f.set("org", "")} />}
                    <FilterGroup label="Status">
                        <SelectFilter value={f.get("status") || "any"} onChange={(v) => f.set("status", v === "any" ? "" : v)} options={anyOption("status", DOMAIN_STATUSES)} />
                    </FilterGroup>
                    <FilterGroup label="Expires">
                        <SelectFilter
                            value={expiringValue || "any"}
                            onChange={(v) => f.set("expiring_days", v === "any" ? "" : v)}
                            options={
                                expiringValue && !EXPIRING_OPTIONS.some((o) => o.value === expiringValue)
                                    ? [...EXPIRING_OPTIONS, { value: expiringValue, label: `Within ${expiringValue} days` }]
                                    : EXPIRING_OPTIONS
                            }
                        />
                    </FilterGroup>
                    <FilterGroup label="Auto-renew">
                        <SegmentedFilter
                            value={autoRenew === "true" ? "on" : autoRenew === "false" ? "off" : "any"}
                            onChange={(v) => f.set("auto_renew", v === "on" ? "true" : v === "off" ? "false" : "")}
                            options={[
                                { value: "any", label: "Any" },
                                { value: "on", label: "On" },
                                { value: "off", label: "Off" },
                            ]}
                        />
                        <ToggleFilter checked={f.get("renewal_failed") === "true"} onChange={(c) => f.set("renewal_failed", c ? "true" : "")} label="Renewal failed" />
                    </FilterGroup>
                </>
            }
        >
            <DataTable
                columns={DOMAIN_COLUMNS}
                rows={rows}
                getRowId={(d) => d.id}
                loading={q.isLoading}
                error={q.error}
                onRetry={() => q.refetch()}
                onRowClick={setOpen}
                selectedRowId={open?.id ?? null}
                errorTitle="Failed to load store domains"
                storageKey="admin.store.domains"
                csvName="warmbly-store-domains"
                noun="domains"
                emptyTitle="No domains"
                emptyHint="No store domains match these filters."
                pager={{
                    canPrev: pager.canPrev,
                    canNext: !!q.data?.pagination.has_more,
                    onPrev: pager.prev,
                    onNext: () => pager.next(q.data?.pagination.next_cursor),
                    page: pager.page,
                    shown: rows.length,
                }}
            />
            <DomainDrawer domain={open} onClose={() => setOpen(null)} />
        </Explorer>
    );
}

// ---- Mailboxes ---------------------------------------------------------------

const MAILBOX_KEYS = ["q", "org", "status", "connect_state", "billing_failed"];
const MAILBOX_COLUMNS = mailboxColumns(true);

export function MailboxesTab() {
    const f = useParamFilters(MAILBOX_KEYS);
    const pager = useCursorPager();
    const { reset } = pager;
    const [open, setOpen] = useState<AdminStoreMailbox | null>(null);
    useEffect(() => reset(), [f.signature, reset]);

    const q = useQuery({
        queryKey: ["admin", "store", "mailboxes", f.signature, pager.cursor],
        queryFn: () =>
            listStoreMailboxes({
                q: f.get("q").trim() || undefined,
                org_id: f.get("org") || undefined,
                status: f.get("status") || undefined,
                connect_state: f.get("connect_state") || undefined,
                billing_failed: f.get("billing_failed") === "true" || undefined,
                limit: PAGE,
                cursor: pager.cursor,
            }),
        placeholderData: keepPreviousData,
        staleTime: 30_000,
    });
    const rows = q.data?.data ?? [];

    return (
        <Explorer
            activeCount={f.active}
            onReset={f.reset}
            filters={
                <>
                    <FilterGroup label="Search">
                        <SearchFilter value={f.get("q")} onChange={(v) => f.set("q", v)} placeholder="Email address…" />
                    </FilterGroup>
                    {f.get("org") && <WorkspaceScope onClear={() => f.set("org", "")} />}
                    <FilterGroup label="Status">
                        <SelectFilter value={f.get("status") || "any"} onChange={(v) => f.set("status", v === "any" ? "" : v)} options={anyOption("status", MAILBOX_STATUSES)} />
                    </FilterGroup>
                    <FilterGroup label="Connect">
                        <SelectFilter value={f.get("connect_state") || "any"} onChange={(v) => f.set("connect_state", v === "any" ? "" : v)} options={anyOption("connect state", CONNECT_STATES)} />
                    </FilterGroup>
                    <FilterGroup label="Billing">
                        <ToggleFilter checked={f.get("billing_failed") === "true"} onChange={(c) => f.set("billing_failed", c ? "true" : "")} label="Billing failed" />
                    </FilterGroup>
                </>
            }
        >
            <DataTable
                columns={MAILBOX_COLUMNS}
                rows={rows}
                getRowId={(m) => m.id}
                loading={q.isLoading}
                error={q.error}
                onRetry={() => q.refetch()}
                onRowClick={setOpen}
                selectedRowId={open?.id ?? null}
                errorTitle="Failed to load store mailboxes"
                storageKey="admin.store.mailboxes"
                csvName="warmbly-store-mailboxes"
                noun="mailboxes"
                emptyTitle="No mailboxes"
                emptyHint="No store mailboxes match these filters."
                pager={{
                    canPrev: pager.canPrev,
                    canNext: !!q.data?.pagination.has_more,
                    onPrev: pager.prev,
                    onNext: () => pager.next(q.data?.pagination.next_cursor),
                    page: pager.page,
                    shown: rows.length,
                }}
            />
            <MailboxDrawer mailbox={open} onClose={() => setOpen(null)} />
        </Explorer>
    );
}
