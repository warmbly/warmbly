// /admin/instance/* : the operator's view of this deployment.
//
// The environment is authoritative, so configuration and limits are read
// only. Instance settings is the one editable document and it holds only
// keys that no environment variable owns, so there is no precedence to
// resolve between the two tiers.

import { Request } from "@/lib/api/client";

export type ConfigSource = "env" | "default" | "derived" | "unset";
export type RuntimeChangeable = "boot-only" | "per-request";

export interface InstanceConfigEntry {
    key: string;
    // Always empty for a sensitive key: the fingerprint is the only disclosure.
    value: string;
    source: ConfigSource;
    sensitive: boolean;
    set: boolean;
    // First 4 hex chars of SHA-256 of the value, so two services can be
    // compared (same AUTH_SECRET?) without either secret being shown.
    fingerprint: string;
    group: string;
    effect: string;
    docs: string;
    runtime_changeable: RuntimeChangeable;
}

export interface InstanceConfigResult {
    entries: InstanceConfigEntry[];
}

export function getInstanceConfig(): Promise<InstanceConfigResult> {
    return Request({
        method: "GET",
        url: "/admin/instance/config",
        authorization: true,
    });
}

export type CheckSeverity = "error" | "warning" | "info";

export interface InstanceCheck {
    id: string;
    severity: CheckSeverity;
    title: string;
    message: string;
    // Site-relative docs path, e.g. "/development/configuration/#secrets".
    docs?: string;
    // Free-text subject (a mailbox address, a worker name); empty when instance-wide.
    target?: string;
}

export interface InstanceHealthSummary {
    error: number;
    warning: number;
    info: number;
}

export interface InstanceHealthResult {
    // Only non-ok checks are returned, so an empty list means all clear.
    checks: InstanceCheck[];
    summary: InstanceHealthSummary;
}

export function getInstanceHealth(): Promise<InstanceHealthResult> {
    return Request({
        method: "GET",
        url: "/admin/instance/health",
        authorization: true,
    });
}

export interface InstanceLimitEntry {
    name: string;
    value: string;
    unit?: string;
    description?: string;
}

export interface InstanceLimitGroup {
    title: string;
    entries: InstanceLimitEntry[];
}

export interface InstanceLimitsResult {
    groups: InstanceLimitGroup[];
}

export function getInstanceLimits(): Promise<InstanceLimitsResult> {
    return Request({
        method: "GET",
        url: "/admin/instance/limits",
        authorization: true,
    });
}

export interface InstanceSettings {
    invitations: {
        links_enabled: boolean;
        ttl_hours: number;
    };
    access: {
        allow_invited_signup: boolean;
    };
    // Mailbox sync fair use. Zero is never stored: the backend clamps every
    // value into its band and resolves an absent key to the compiled default.
    sync: {
        backfill_days: number;
        backfill_messages: number;
        daily_messages_per_mailbox: number;
        daily_messages_per_org: number;
    };
    // How long event-level history is kept. Every window bounds personal data:
    // opens and clicks carry a client, a device and a location, funnel events
    // carry a visitor's path, and the audit trail carries IP addresses, user
    // agents and change payloads. None of them affect a count or a routing
    // decision; campaign progress keeps its own summary.
    retention: {
        engagement_event_days: number;
        form_event_days: number;
        audit_log_days: number;
    };
    // The sending-domain authentication gate. A mailbox whose domain has been
    // failing SPF or DMARC for longer than the grace window stops sending cold
    // mail and warmup mail until the records are fixed.
    deliverability: {
        enforce_domain_auth: boolean;
        auth_grace_hours: number;
    };
    // Operator notification channels. Targets and secrets are redacted on
    // read: a chat webhook URL is a bearer credential, so the server returns a
    // recognisable preview and treats the preview (or an empty string) as
    // "keep what is stored" on write.
    // Optional so a client that does not manage channels (the instance
    // settings page) can PUT without them; an absent section keeps the
    // stored channels untouched.
    notifications?: {
        channels: NotifyChannel[];
    };
}

export type NotifyChannelType = "discord" | "slack" | "webhook" | "email";

export interface NotifyChannel {
    id: string;
    name: string;
    type: NotifyChannelType;
    /** Webhook URL, or the address for an email channel. Redacted on read. */
    target: string;
    /** HMAC secret for the generic webhook transport. Redacted on read. */
    secret?: string;
    /** Subscribed event keys. Empty means every event. */
    events: string[];
    enabled: boolean;
}

export type NotifySeverity = "info" | "warning" | "urgent";

export interface NotifyEventDef {
    key: string;
    label: string;
    description: string;
    group: string;
    severity: NotifySeverity;
    self_host_relevant: boolean;
}

export interface NotifyEventsResult {
    events: NotifyEventDef[];
    self_hosted: boolean;
}

export function getNotificationEvents(): Promise<NotifyEventsResult> {
    return Request({
        method: "GET",
        url: "/admin/instance/notifications/events",
        authorization: true,
    });
}

/** Send a test alert. Pass `id` for a saved channel, or the unsaved fields. */
export function testNotificationChannel(body: {
    id?: string;
    type?: NotifyChannelType;
    name?: string;
    target?: string;
    secret?: string;
}): Promise<{ delivered: boolean }> {
    return Request({
        method: "POST",
        url: "/admin/instance/notifications/test",
        data: body,
        authorization: true,
    });
}

export function getInstanceSettings(): Promise<InstanceSettings> {
    return Request({
        method: "GET",
        url: "/admin/instance/settings",
        authorization: true,
    });
}

export function putInstanceSettings(body: InstanceSettings): Promise<InstanceSettings> {
    return Request({
        method: "PUT",
        url: "/admin/instance/settings",
        data: body,
        authorization: true,
    });
}
