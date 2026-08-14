// /admin/system/status — live health probes against the platform's
// backing services (postgres, redis, kafka, schema-registry, realtime,
// tracking). The backend runs the probes on each request, so a fetch
// here is a fresh check, not a cached snapshot.

import { Request } from "@/lib/api/client";

export interface SystemComponentStatus {
    name: string;
    ok: boolean;
    latency_ms: number;
    error?: string;
}

export interface SystemStatusResult {
    data: SystemComponentStatus[];
    checked_at: string;
}

export function getSystemStatus(): Promise<SystemStatusResult> {
    return Request({
        method: "GET",
        url: "/admin/system/status",
        authorization: true,
    });
}

// /admin/mail/status and /admin/mail/test — the platform's own mail transport.
//
// This is separate from the component probes above because platform mail is
// not a backing service that degrades: login codes, password resets and
// invitations all go through it, so a broken relay is a lockout.

export interface MailStatusResult {
    transport: string;
    delivers: boolean;
    healthy: boolean;
    detail: string;
    error?: string;
}

export function getMailStatus(): Promise<MailStatusResult> {
    return Request({
        method: "GET",
        url: "/admin/mail/status",
        authorization: true,
    });
}

export interface SendTestEmailResult {
    sent: boolean;
    transport: string;
    error?: string;
    note?: string;
}

export function sendTestEmail(to: string): Promise<SendTestEmailResult> {
    return Request({
        method: "POST",
        url: "/admin/mail/test",
        data: { to },
        authorization: true,
    });
}
