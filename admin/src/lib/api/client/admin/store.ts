// /admin/store/*: the domain and mailbox store. Pricing is the live wholesale list proxied
// from GraphKit for the settings preview; the rest is the Store section.

import { Request } from "@/lib/api/client";
import { buildSearchQuery } from "@/lib/api/client/admin/query";

export interface StoreDomainPrice {
    tld: string;
    /** Wholesale first-year price, micro-USD. */
    register_micros: number;
    /** Wholesale renewal price, micro-USD. */
    renew_micros: number;
}

export interface StoreRetailPrice {
    tld: string;
    /** First-year price at the instance's saved markup, micro-USD. */
    register_micros: number;
    /** Renewal price at the instance's saved markup, micro-USD. */
    renew_micros: number;
}

export interface StorePricing {
    currency: string;
    updated_at: string;
    domains: StoreDomainPrice[];
    mailboxes?: { provider: string; monthly_micros: number }[] | null;
    limits?: { mailboxes_per_domain_max: number; mailboxes_per_domain_recommended: number };
    /** The same TLDs at the saved settings; the preview computes from the form's unsaved values instead. */
    retail: StoreRetailPrice[];
}

export function getStorePricing(): Promise<StorePricing> {
    return Request({
        method: "GET",
        url: "/admin/store/pricing",
        authorization: true,
    });
}

// ---- Store operations: health, economics, instance-wide lists and actions ----
// Reads need the view-organizations bit, actions the manage-organizations bit.
// Every action is audited and converges, so a retried click changes nothing twice.

export type StoreMode = "live" | "sandbox" | "simulator" | "off";
export type StoreOrderStatus = "charging" | "processing" | "completed" | "partially_failed" | "failed";
export type StoreConnectState = "waiting" | "connecting" | "connected" | "failed";

export interface AdminStoreOrderStep {
    key: string;
    status: string;
    at?: string | null;
}

export interface AdminStoreOrderLine {
    type: "domain" | "mailboxes";
    domain: string;
    years?: number;
    provider?: string;
    count?: number;
    amount_micros: number;
    renew_micros?: number;
    monthly_micros?: number;
}

export interface AdminStoreOrder {
    id: string;
    organization_id: string;
    organization_name: string;
    status: StoreOrderStatus;
    /** Empty, or a Warmbly code. Upstream wording never reaches the panel. */
    failure_code: string;
    due_now_micros: number;
    charged_micros: number;
    refunded_micros: number;
    /** Line indexes already refunded. */
    refunded_lines: number[];
    placed: boolean;
    upstream_id: string;
    stuck: boolean;
    steps: AdminStoreOrderStep[];
    lines: AdminStoreOrderLine[];
    created_by: string | null;
    created_at: string;
    updated_at: string;
    completed_at: string | null;
}

export interface AdminStoreDomain {
    id: string;
    organization_id: string;
    organization_name: string;
    name: string;
    status: string;
    source: "purchased" | "connected";
    provider: string;
    expires_at: string | null;
    auto_renew: boolean;
    renew_micros: number;
    renewal_failures: number;
    renewal_attempt_at: string | null;
    last_notice: string;
    mailbox_count: number;
    tracking_host: string;
    upstream_id: string;
    created_at: string;
    updated_at: string;
}

export interface AdminStoreMailbox {
    id: string;
    organization_id: string;
    organization_name: string;
    domain_id: string;
    email: string;
    provider: string;
    status: string;
    is_admin: boolean;
    monthly_micros: number;
    paid_through: string | null;
    cancels_at: string | null;
    email_account_id: string | null;
    connect_method: string;
    connect_state: StoreConnectState;
    connect_error: string;
    connect_attempts: number;
    billing_failures: number;
    pending_charge: boolean;
    upstream_id: string;
    created_at: string;
    updated_at: string;
}

export interface StoreList<T> {
    data: T[];
    pagination: { next_cursor: string | null; has_more: boolean };
}

export interface StoreLoopStatus {
    last_started_at: string | null;
    last_finished_at: string | null;
    last_status: string;
    last_error: string;
    lag_seconds: number;
}

export interface StoreHealthCounts {
    orders_open: number;
    orders_stuck: number;
    orders_failed_7d: number;
    connects_failed: number;
    mailboxes_live: number;
    mailboxes_billing_failed: number;
    domains_live: number;
    domains_renewal_failed: number;
    domains_expiring_30d: number;
}

export interface StoreHealth {
    enabled: boolean;
    mode: StoreMode;
    metered: boolean;
    partner_balance: {
        balance_micros: number;
        low_threshold_micros: number;
        currency: string;
        livemode: boolean;
    } | null;
    partner_balance_error: "" | "store_unavailable";
    last_webhook_at: string | null;
    events_cursor_set: boolean;
    reconciler: StoreLoopStatus | null;
    billing: StoreLoopStatus | null;
    counts: StoreHealthCounts;
    alerts: { code: string; title: string; at: string }[];
    /** Mailboxes whose GraphKit period started without Warmbly having collected it. Should stay empty. */
    uncollected?: StoreUncollectedMailbox[];
}

export interface StoreUncollectedMailbox {
    id: string;
    organization_id: string;
    email: string;
    /** GraphKit's current period end. */
    period_end: string;
    /** How far Warmbly collected. */
    paid_through: string;
}

export interface StoreEconomics {
    currency: string;
    updated_at: string;
    mailboxes: {
        provider: string;
        wholesale_monthly_micros: number;
        retail_monthly_micros: number;
        margin_micros: number;
        scheduled_wholesale_monthly_micros: number | null;
        scheduled_from: string | null;
        live_count: number;
    }[];
    domains: {
        tld: string;
        min_years: number;
        wholesale_register_micros: number;
        retail_register_micros: number;
        wholesale_renew_micros: number;
        retail_renew_micros: number;
    }[];
    totals: {
        live_mailboxes: number;
        monthly_retail_micros: number;
        monthly_wholesale_micros: number;
        monthly_margin_micros: number;
    };
}

export interface StoreWorkspace {
    organization: { id: string; name: string };
    account: { exists: boolean; created_at: string | null; has_registrant: boolean };
    plan_active: boolean;
    managed_by_partner: boolean;
    balance_micros: number | null;
    monthly_spend_micros: number;
    orders: AdminStoreOrder[];
    domains: AdminStoreDomain[];
    mailboxes: AdminStoreMailbox[];
}

interface Paging {
    q?: string;
    org_id?: string;
    limit?: number;
    cursor?: string;
}

export interface StoreOrderFilters extends Paging {
    status?: string;
    failure_code?: string;
    stuck?: boolean;
}

export interface StoreDomainFilters extends Paging {
    status?: string;
    /** Sent as text so false is a filter rather than an omission. */
    auto_renew?: "true" | "false";
    expiring_days?: number;
    renewal_failed?: boolean;
}

export interface StoreMailboxFilters extends Paging {
    status?: string;
    connect_state?: string;
    billing_failed?: boolean;
}

/** A line refund pays that line's charge at most once; an amount refund needs a reason and an idempotency key. */
export type StoreRefund = { line: number } | { amount_micros: number; reason: string };

const get = <T>(url: string): Promise<T> => Request({ method: "GET", url, authorization: true });
const post = <T>(url: string): Promise<T> => Request({ method: "POST", url, authorization: true });

export const getStoreHealth = () => get<StoreHealth>("/admin/store/health");
export const getStoreEconomics = () => get<StoreEconomics>("/admin/store/economics");
export const getStoreWorkspace = (orgId: string) => get<StoreWorkspace>(`/admin/store/workspaces/${orgId}`);

export const listStoreOrders = (f: StoreOrderFilters = {}) =>
    get<StoreList<AdminStoreOrder>>(`/admin/store/orders${buildSearchQuery({ ...f })}`);
export const getStoreOrder = (id: string) => get<AdminStoreOrder>(`/admin/store/orders/${id}`);
export const listStoreDomains = (f: StoreDomainFilters = {}) =>
    get<StoreList<AdminStoreDomain>>(`/admin/store/domains${buildSearchQuery({ ...f })}`);
export const listStoreMailboxes = (f: StoreMailboxFilters = {}) =>
    get<StoreList<AdminStoreMailbox>>(`/admin/store/mailboxes${buildSearchQuery({ ...f })}`);

export const refreshStoreOrder = (id: string) => post<AdminStoreOrder>(`/admin/store/orders/${id}/refresh`);
/** Places a charged order that never reached GraphKit, under its original key. */
export const placeStoreOrder = (id: string) => post<AdminStoreOrder>(`/admin/store/orders/${id}/place`);

/** The key is sent on every refund, so a retried submission of the same dialog never pays twice. */
export function refundStoreOrder(
    id: string,
    body: StoreRefund,
    idempotencyKey: string,
): Promise<{ order: AdminStoreOrder; refunded_micros: number }> {
    return Request({
        method: "POST",
        url: `/admin/store/orders/${id}/refund`,
        authorization: true,
        data: body,
        headers: { "Idempotency-Key": idempotencyKey },
    });
}

export type StoreMailboxAction = "refresh" | "connect" | "cancel" | "resume";
export const storeMailboxAction = (id: string, action: StoreMailboxAction) =>
    post<AdminStoreMailbox>(`/admin/store/mailboxes/${id}/${action}`);

export type StoreDomainAction = "refresh" | "renew";
export const storeDomainAction = (id: string, action: StoreDomainAction) =>
    post<AdminStoreDomain>(`/admin/store/domains/${id}/${action}`);

export function setStoreDomainAutoRenew(id: string, autoRenew: boolean): Promise<AdminStoreDomain> {
    return Request({
        method: "PATCH",
        url: `/admin/store/domains/${id}`,
        authorization: true,
        data: { auto_renew: autoRenew },
    });
}

// ---- GraphKit connection: manage-settings; writes also need a fresh authentication ----

export interface StoreConnection {
    /** Environment GRAPHKIT_* variables win over stored settings. */
    source: "environment" | "settings" | "none";
    configured: boolean;
    mode: StoreMode;
    api_url: string;
    default_api_url: string;
    api_key_set: boolean;
    webhook_secret_set: boolean;
    /** What GraphKit must call. */
    webhook_url: string;
    /** False when CREDENTIALS_ENCRYPTION_KEY is unset; saving is refused. */
    sealing_available: boolean;
    updated_at: string | null;
    updated_by: string | null;
    /** Set when nothing is configured but live purchases exist. */
    purchases_without_connection: { mailboxes: number; domains: number } | null;
}

/** An omitted or empty secret keeps the stored one. */
export interface StoreConnectionInput {
    api_url: string;
    api_key?: string;
    webhook_secret?: string;
}

export interface StoreConnectionTest {
    mode: "live" | "test";
    balance_micros: number;
    currency: string;
    com_register_micros: number | null;
    com_renew_micros: number | null;
}

export const STORE_CONNECTION_KEY = ["admin", "store", "connection"] as const;

export function getStoreConnection(): Promise<StoreConnection> {
    return Request({ method: "GET", url: "/admin/store/connection", authorization: true });
}

export function saveStoreConnection(body: StoreConnectionInput): Promise<StoreConnection> {
    return Request({ method: "PUT", url: "/admin/store/connection", data: body, authorization: true });
}

export function testStoreConnection(body: StoreConnectionInput): Promise<StoreConnectionTest> {
    return Request({ method: "POST", url: "/admin/store/connection/test", data: body, authorization: true });
}

export function deleteStoreConnection(): Promise<StoreConnection> {
    return Request({ method: "DELETE", url: "/admin/store/connection", authorization: true });
}
