// The connect page's slice of the portal service's same-origin API. Mirrors portal/worker/portal.mjs
// and internal/formwire/connect.go.

import { apiHeaders } from "./api";
import { type Brand, parseBrand } from "./theme";

export type ConnectProvider = "gmail" | "outlook" | "smtp_imap" | "vendor";

export interface ConnectVendorField {
    key: string;
    label: string;
    secret: boolean;
    required: boolean;
    help?: string;
}

/** An inbox vendor a client can connect an account of, and what it asks for. */
export interface ConnectVendor {
    id: string;
    label: string;
    key_help_url?: string;
    fields: ConnectVendorField[];
}

export interface ConnectLink {
    workspace: string;
    /** Parsed from the answer's brand object; nothing in it is used as it arrived. */
    brand: Brand;
    providers: ConnectProvider[];
    max_mailboxes: number;
    connected: number;
    expires_at: string;
    workspace_full?: boolean;
    captcha_site_key?: string;
    /** Present when the link takes a vendor account. */
    vendors?: ConnectVendor[];
}

export interface MailService {
    host: string;
    port: number;
    username: string;
    password: string;
    security: "tls" | "starttls";
}

export interface SMTPIMAPPayload {
    email: string;
    name?: string;
    smtp: MailService;
    imap: MailService;
    captcha_token?: string;
}

export interface ConnectedMailbox {
    email: string;
    provider: string;
    connected: number;
    max_mailboxes: number;
}

/** A refusal with the backend's stable code; the page maps codes to its own copy. */
export class ConnectError extends Error {
    constructor(
        readonly code: string,
        message: string,
        readonly status: number,
    ) {
        super(message);
        this.name = "ConnectError";
    }
}

async function call<T>(token: string, path: string, init?: RequestInit): Promise<T> {
    const res = await fetch(`/api/connect/${encodeURIComponent(token)}${path}`, {
        ...init,
        headers: { ...apiHeaders(), ...(init?.body ? { "Content-Type": "application/json" } : {}) },
    });
    const body = (await res.json().catch(() => null)) as (T & { code?: string; message?: string }) | null;
    if (!res.ok) {
        throw new ConnectError(body?.code || "unavailable", body?.message || "Something went wrong. Try again.", res.status);
    }
    return body as T;
}

export async function fetchConnectLink(token: string): Promise<ConnectLink> {
    const raw = await call<Omit<ConnectLink, "brand"> & { brand?: unknown }>(token, "");
    return { ...raw, brand: parseBrand(raw.brand) };
}

export interface SigninStarted {
    url: string;
    result_key: string;
}

export interface SigninOutcome {
    status: "pending" | "connected" | "failed";
    email?: string;
    provider?: string;
    code?: string;
    message?: string;
    connected?: number;
    max_mailboxes?: number;
}

export function startConnectOAuth(token: string, provider: ConnectProvider): Promise<SigninStarted> {
    return call(token, "/oauth", { method: "POST", body: JSON.stringify({ provider }) });
}

export function connectSMTPIMAP(token: string, payload: SMTPIMAPPayload): Promise<ConnectedMailbox> {
    return call(token, "/smtp-imap", { method: "POST", body: JSON.stringify(payload) });
}

export function fetchSigninOutcome(token: string, resultKey: string): Promise<SigninOutcome> {
    return call(token, "/oauth/result", { method: "POST", body: JSON.stringify({ result_key: resultKey }) });
}

export interface VendorMailbox {
    id: string;
    email: string;
    name?: string;
    provider?: string;
    /** Already connected to the workspace. */
    connected?: boolean;
}

/** What a vendor key reaches. The key stays on the server under the ticket. */
export interface VendorMailboxes {
    ticket: string;
    vendor: string;
    mailboxes: VendorMailbox[];
    remaining: number;
}

export interface VendorImportStarted {
    mailboxes: number;
    connected: number;
    max_mailboxes: number;
}

export function listVendorMailboxes(
    token: string,
    payload: { vendor: string; fields: Record<string, string>; captcha_token?: string },
): Promise<VendorMailboxes> {
    return call(token, "/vendor", { method: "POST", body: JSON.stringify(payload) });
}

export function importVendorMailboxes(token: string, ticket: string, mailboxIds: string[]): Promise<VendorImportStarted> {
    return call(token, "/vendor/import", { method: "POST", body: JSON.stringify({ ticket, mailbox_ids: mailboxIds }) });
}
