// /admin/partners: resellers that create and run workspaces through the partner API, their keys and webhook.

import { Request } from "@/lib/api/client";

export type PartnerMode = "live" | "test";

export interface AdminPartner {
    id: string;
    slug: string;
    name: string;
    display_name: string;
    status: "active" | "suspended";
    service_user_id: string;
    allowed_plans: string[];
    allowed_hosts: string[];
    delegation_domains: string[];
    webhook_url: string | null;
    webhook_secret_set: boolean;
    webhook_secret_rotated_at?: string;
    callback_base_url: string | null;
    callback_key_set: boolean;
    wholesale_sending_warmup_day_micros: number;
    wholesale_warmup_only_day_micros: number;
    rate_limit_per_second: number;
    created_at: string;
    updated_at: string;
}

export interface AdminPartnerKey {
    id: string;
    mode: PartnerMode;
    name: string;
    key_prefix: string;
    key_suffix: string;
    last_used_at?: string;
    revoked_at?: string;
    created_at: string;
}

export interface AdminPartnerDetail extends AdminPartner {
    keys: AdminPartnerKey[];
}

export interface AdminPartnerOrganization {
    id: string;
    mode: PartnerMode;
    external_id: string;
    organization_id: string | null;
    organization_name?: string;
    plan_key: string;
    state: "active" | "suspended" | "cancelled";
    suspended_reason: string | null;
    campaign_sends_per_mailbox_per_day: number;
    group_external_id: string | null;
    group_name: string | null;
    deletes_at: string | null;
    created_at: string;
}

export interface AdminPartnerUpdate {
    name?: string;
    display_name?: string;
    status?: "active" | "suspended";
    allowed_hosts?: string[];
    delegation_domains?: string[];
    webhook_url?: string;
    callback_base_url?: string;
    wholesale_sending_warmup_day_micros?: number;
    wholesale_warmup_only_day_micros?: number;
    rate_limit_per_second?: number;
}

export function listPartners(): Promise<{ data: AdminPartner[] }> {
    return Request({ method: "GET", url: "/admin/partners", authorization: true });
}

export function getPartner(id: string): Promise<AdminPartnerDetail> {
    return Request({ method: "GET", url: `/admin/partners/${id}`, authorization: true });
}

export function createPartner(body: { slug: string; name: string; display_name?: string; allowed_hosts?: string[] }): Promise<AdminPartnerDetail> {
    return Request({ method: "POST", url: "/admin/partners", authorization: true, data: body });
}

export function updatePartner(id: string, body: AdminPartnerUpdate): Promise<AdminPartnerDetail> {
    return Request({ method: "PATCH", url: `/admin/partners/${id}`, authorization: true, data: body });
}

/** The new signing secret, shown once. The previous one keeps signing for a day. */
export function rotatePartnerWebhookSecret(id: string): Promise<{ secret: string }> {
    return Request({ method: "POST", url: `/admin/partners/${id}/webhook-secret`, authorization: true });
}

export function setPartnerCallbackKey(id: string, key: string): Promise<AdminPartnerDetail> {
    return Request({ method: "PUT", url: `/admin/partners/${id}/callback-key`, authorization: true, data: { key } });
}

/** A partner API key; `secret` exists only in this answer. */
export function createPartnerKey(id: string, body: { mode: PartnerMode; name?: string }): Promise<AdminPartnerKey & { secret: string }> {
    return Request({ method: "POST", url: `/admin/partners/${id}/keys`, authorization: true, data: body });
}

export function revokePartnerKey(id: string, keyId: string): Promise<void> {
    return Request({ method: "DELETE", url: `/admin/partners/${id}/keys/${keyId}`, authorization: true });
}

export function listPartnerOrganizations(
    id: string,
    cursor?: string,
): Promise<{ data: AdminPartnerOrganization[]; pagination: { next_cursor: string | null; has_more: boolean } }> {
    const qs = new URLSearchParams({ limit: "50" });
    if (cursor) qs.set("cursor", cursor);
    return Request({ method: "GET", url: `/admin/partners/${id}/organizations?${qs}`, authorization: true });
}

/** One workspace's enabled mailbox-days in a statement. */
export interface AdminPartnerStatementLine {
    external_id: string;
    sending_warmup_days: number;
    warmup_only_days: number;
    amount_micros: number;
}

/** A month of a partner's live enabled mailbox-days at its wholesale rates; closed_at is null while the month runs. */
export interface AdminPartnerStatement {
    id: string;
    month: string;
    sending_warmup_days: number;
    warmup_only_days: number;
    sending_warmup_day_micros: number;
    warmup_only_day_micros: number;
    amount_micros: number;
    closed_at: string | null;
    lines: AdminPartnerStatementLine[] | null;
}

export function listPartnerStatements(id: string): Promise<{ data: AdminPartnerStatement[] }> {
    return Request({ method: "GET", url: `/admin/partners/${id}/statements`, authorization: true });
}

/** month is YYYY-MM: a closed statement, or the running month at today's rates. */
export function getPartnerStatement(id: string, month: string): Promise<AdminPartnerStatement> {
    return Request({ method: "GET", url: `/admin/partners/${id}/statements/${month}`, authorization: true });
}
