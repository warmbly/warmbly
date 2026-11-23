// The store: domains and Google Workspace / Microsoft 365 mailboxes bought inside Warmbly. Money is micro-USD.
import type Pagination from "@/lib/api/models/app/Pagination";

export type StoreProvider = "google" | "microsoft";

export interface StoreCard {
    brand: string;
    last4: string;
}

/** The ICANN registrant; the backend validates it and refuses with contact_invalid. */
export interface Registrant {
    first_name: string;
    last_name: string;
    organization: string;
    email: string;
    phone: string;
    address1: string;
    address2: string;
    city: string;
    state: string;
    postal_code: string;
    country: string;
}

/** Why the store is off: the instance has no GraphKit connection, or a reseller sells to this workspace instead. */
export type StoreOffReason = "not_configured" | "partner_managed";

/** Live purchases a workspace still holds while GraphKit is not configured. */
export interface StoreDisconnected {
    mailboxes: number;
    domains: number;
}

export interface StoreStatus {
    enabled: boolean;
    /** The operator paused new purchases; everything already bought keeps working. Absent from an older backend. */
    sales_paused?: boolean;
    /** The instance has a GraphKit connection (independent of this workspace); absent from an older backend. */
    configured?: boolean;
    /** Why enabled is false; absent when enabled. */
    reason?: StoreOffReason;
    /** With not_configured: a self-host, which shows buying as locked; a hosted instance leaves it false and the store hidden. Absent from an older backend. */
    locked?: boolean;
    /** With locked: the caller is an instance admin holding manage_settings and could connect GraphKit. */
    can_configure?: boolean;
    /** With can_configure: the admin panel page that connects GraphKit; absent when the backend does not know its URL. */
    configure_url?: string;
    /** GraphKit is not configured but this workspace has live purchases: renewals, monthly billing and Google connects are paused. */
    disconnected?: StoreDisconnected | null;
    metered: boolean;
    currency: string;
    mailbox_monthly_micros: number;
    domain_markup: "at_cost" | "markup" | string;
    limits: { mailboxes_per_domain_max: number; mailboxes_per_domain_recommended: number };
    providers: StoreProvider[];
    balance: { balance_micros: number; card: StoreCard | null } | null;
    registrant: Registrant | null;
    /** A draft from the billing details and the workspace owner, sent only while registrant is null. */
    registrant_prefill?: Registrant | null;
    /** Buying needs an active paid plan; always true on a self-host. */
    plan_active: boolean;
    /** Lets a metered checkout save a card in the page with Stripe.js; absent where nothing is charged. */
    stripe_publishable_key?: string;
    /** The least a store charge puts on the card; the rest stays on the balance. */
    min_card_charge_micros?: number;
    /** The day of the month every mailbox is billed together (1-28); null before the first mailbox. */
    billing_day?: number | null;
    /** The next monthly mailbox charge, collected a few days before the billing day; null when nothing is due. */
    next_bundle?: { charge_on: Date | string; mailboxes: number; amount_micros: number } | null;
    /** Where new domains forward unless the buyer changes it: the workspace's website, else the buyer's business domain; "" for none. */
    default_forwarding_url?: string;
    /** Whether mailbox profile photos can be uploaded on this instance. */
    photos?: boolean;
}

/** An uploaded mailbox profile photo, named by an order's mailbox line or a purchased mailbox. */
export interface StorePhoto {
    id: string;
    created_at: Date | string;
}

export type DomainUnavailableReason = "registered" | "premium" | "reserved" | "tld_unsupported" | string;

export interface DomainSearchResult {
    domain: string;
    display_name: string;
    tld: string;
    available: boolean;
    exact: boolean;
    /** The minimum registration term for the TLD; price_micros covers all of it. */
    years: number;
    price_micros: number;
    renew_micros: number;
    reason: DomainUnavailableReason | null;
}

export interface DomainSearchResponse {
    query: string;
    data: DomainSearchResult[];
}

export interface QuoteMailbox {
    local_part: string;
    first_name: string;
    last_name: string;
    /** The Google Workspace / Microsoft 365 admin of a new domain; at most one per mailboxes item. */
    admin?: boolean;
    /** An uploaded photo for this mailbox, over the line's photo_id. One photo may serve many mailboxes. */
    photo_id?: string;
    /** The finished signature set on the mailbox when it connects; both empty is none. */
    signature?: QuoteSignature;
}

export interface QuoteSignature {
    html: string;
    plain: string;
}

export type QuoteItem =
    | { type: "domain"; domain: string; years: number }
    | {
          type: "mailboxes";
          domain: string;
          provider: StoreProvider;
          mailboxes: QuoteMailbox[];
          /** An uploaded photo for every mailbox on the domain. */
          photo_id?: string;
      };

export type QuoteLine =
    | { type: "domain"; domain: string; years: number; amount_micros: number; renew_micros: number }
    | {
          type: "mailboxes";
          domain: string;
          provider: StoreProvider;
          count: number;
          monthly_micros: number;
          amount_micros: number;
          admin_email: string | null;
          /** A connected domain whose DNS moves: new nameservers go in at the registrar after the order. */
          nameservers_change: boolean;
      };

export interface QuotePayment {
    balance_micros: number;
    from_balance_micros: number;
    /** What the card is charged today; past the shortfall when a minimum card charge tops the balance up. */
    card_charge_micros: number;
    card: StoreCard | null;
    needs_payment_method: boolean;
    /** The balance once this order is paid. Optional until every backend sends it. */
    balance_after_micros?: number;
    /** The workspace's automatic top-up. Optional until every backend sends it. */
    auto_reload?: { enabled: boolean; threshold_micros: number; amount_micros: number; store_managed: boolean } | null;
}

export interface Quote {
    id: string;
    expires_at: Date | string;
    lines: QuoteLine[];
    due_now_micros: number;
    recurring_monthly_micros: number;
    yearly_domain_renewal_micros: number;
    payment: QuotePayment;
}

export type OrderStatus = "charging" | "processing" | "completed" | "partially_failed" | "failed";
export type StepKey = "register" | "dns" | "mail_auth" | "mailboxes" | "delegation" | "ready";
export type StepStatus = "pending" | "running" | "done" | "failed" | "skipped";

export interface OrderStep {
    key: StepKey;
    status: StepStatus;
    at?: Date | string | null;
}

export type MailAuthState = "ok" | "pending" | "failed" | string;

export type StoreDomainSource = "purchased" | "connected";
export type ConnectMethod = "nameservers" | "manual";
export type ConnectionStatus = "pending" | "active" | "moved" | "expired";

export interface DomainConnection {
    method: ConnectMethod;
    status: ConnectionStatus | string;
    /** A pending connection expires 48 hours after it was made. */
    expires_at: Date | string | null;
    checked_at: Date | string | null;
    /** What public DNS showed at the last check. */
    current_nameservers: string[];
}

/** A record that blocks mail; record_id set means it can be removed here, null means at the DNS provider. */
export interface DnsConflict {
    record_id: string | null;
    type: string;
    name: string;
    content: string;
}

export interface StoreDomain {
    id: string;
    name: string;
    /** registering, active, expired, redemption, transferring_out, transferred_out, failed, connecting, removed. */
    status: string;
    provider: StoreProvider | null;
    expires_at: Date | string | null;
    auto_renew: boolean;
    renew_micros: number;
    forwarding_url: string | null;
    mail_auth: { mx?: MailAuthState; spf?: MailAuthState; dkim?: MailAuthState; dmarc?: MailAuthState } | null;
    mailbox_count: number;
    created_at: Date | string;
    dns?: DnsRecord[];
    /** connected: registered elsewhere and added by the customer. */
    source: StoreDomainSource;
    /** Renew, auto-renew and transfer-out apply. */
    renewable: boolean;
    forwarding_status: "none" | "pending" | "active" | string;
    /** The pair the domain should be delegated to. */
    nameservers: string[];
    connection: DomainConnection | null;
    dns_conflicts: DnsConflict[];
    owner_verification: { status: "pending" | "verified" | string; email: string; deadline: Date | string | null } | null;
    transfer: { status: "requested" | string; requested_at: Date | string; auto_approve_at: Date | string | null } | null;
    /** The open and click tracking host Warmbly set up; "" until set. */
    tracking_host: string;
}

export type DnsRecordType = "A" | "AAAA" | "CNAME" | "MX" | "TXT" | "SRV" | "CAA";

export interface DnsRecord {
    id: string;
    type: DnsRecordType | string;
    /** Relative to the domain; "@" is the apex. */
    name: string;
    content: string;
    ttl: number;
    /** MX and SRV only. */
    priority: number | null;
    /** Kept by Warmbly for mail and forwarding: locked. */
    managed: boolean;
    /** mx, spf, dkim, dmarc, verification, forwarding, ownership, imported, tracking, or "". */
    purpose: string;
}

export interface DnsRecordInput {
    type: string;
    name: string;
    content: string;
    ttl?: number;
    priority?: number;
}

export interface DnsRecordPatch {
    name?: string;
    content?: string;
    ttl?: number;
    priority?: number;
}

export interface DnsCheckResult {
    nameservers: string[];
    records: (DnsRecord & { status: "ok" | "missing" | string })[];
}

export type ConnectState = "waiting" | "connecting" | "connected" | "failed";

export interface StoreMailbox {
    id: string;
    email: string;
    domain_id: string;
    provider: StoreProvider;
    status: string;
    first_name: string;
    last_name: string;
    monthly_micros: number;
    paid_through: Date | string | null;
    cancels_at: Date | string | null;
    /** When the ended mailbox was switched off in Warmbly; its history stays until it is disconnected. */
    cancelled_at?: Date | string | null;
    email_account_id: string | null;
    connect_state: ConnectState;
    /** The step only a person can take before it connects: the Microsoft 365 administrator approving Warmbly. */
    action_required?: "microsoft_consent" | null;
    /** Why the automatic approval with the domain's admin mailbox left that step to a person. */
    action_reason?: ConsentReason | null;
    created_at: Date | string;
    /** The domain's Google Workspace / Microsoft 365 admin. */
    is_admin: boolean;
    /** Campaign sending and warmup paused because the workspace has no plan. */
    paused: boolean;
    /** The profile photo at Google or Microsoft: none, pending (sent, not taken yet) or set. */
    photo?: "none" | "pending" | "set";
}

/** What stopped Warmbly approving itself in Microsoft 365 with the domain's admin mailbox. */
export type ConsentReason = "mfa_required" | "wrong_password" | "not_admin" | "wrong_tenant" | "sign_in_blocked" | "no_admin_sign_in" | "failed";

/** Where the domain's Microsoft 365 administrator approves Warmbly; url is null when nothing is left to approve. */
export interface MicrosoftConsentLink {
    url: string | null;
    expires_at?: Date | string;
    consented: boolean;
}

/** A fresh sign-in, shown once and never cached. */
export interface MailboxSignIn {
    email: string;
    username: string;
    password: string;
    login_url: string;
    admin_console_url: string | null;
}

export interface Order {
    id: string;
    status: OrderStatus;
    created_at: Date | string;
    completed_at: Date | string | null;
    due_now_micros: number;
    charged_micros: number;
    refunded_micros: number;
    steps: OrderStep[];
    domains: StoreDomain[];
    mailboxes: StoreMailbox[];
}

export interface CreateOrderRequest {
    quote_id: string;
    /** The total on screen; a different server total answers 409 price_changed and charges nothing. */
    expected_due_now_micros: number;
    forwarding_url?: string;
    /** Turns on the automatic top-up with this order, when the summary offered it. */
    enable_auto_reload?: boolean;
}

export interface StoreDomainPatch {
    auto_renew?: boolean;
    forwarding_url?: string;
}

export interface List<T> {
    data: T[];
    pagination: Pagination;
}
