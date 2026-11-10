// The shared report read the portal service exposes at /api/reports/:token.
// Mirrors internal/formwire/report.go and models.ClientReportView.

import { apiHeaders, StalePageError } from "./api";
import { type Brand, parseBrand } from "./theme";

export type ReportRange = "last_7_days" | "last_30_days" | "this_month" | "last_month";

export interface ReportSections {
    overview: boolean;
    campaigns: boolean;
    replies: boolean;
    positive_replies: boolean;
    mailboxes: boolean;
    daily_chart: boolean;
}

export interface ReportOverview {
    sent: number;
    opened: number;
    open_rate: number;
    clicked: number;
    replied: number;
    reply_rate: number;
    positive?: number;
    positive_rate?: number;
    bounced: number;
    bounce_rate: number;
}

export interface ReportDay {
    date: string;
    sent: number;
    opens: number;
    replies: number;
}

export interface ReportCampaign {
    name: string;
    status: string;
    sent: number;
    opens: number;
    open_rate: number;
    clicks: number;
    replies: number;
    reply_rate: number;
    positive?: number;
    bounces: number;
    bounce_rate: number;
}

export interface ReportReply {
    name: string;
    email: string;
    campaign: string;
    subject: string;
    replied_at: string;
    classification: string;
    snippet?: string;
}

export interface ReportMailboxes {
    total: number;
    healthy: number;
    warning: number;
    error: number;
    warmup_sent: number;
    warmup_received: number;
    warmup_replied: number;
}

export interface ReportView {
    name: string;
    date_range: ReportRange;
    from: string;
    to: string;
    generated_at: string;
    sections: ReportSections;
    show_reply_snippets: boolean;
    overview?: ReportOverview;
    daily?: ReportDay[];
    campaigns?: ReportCampaign[];
    replies?: ReportReply[];
    mailboxes?: ReportMailboxes;
}

export interface PublicReport {
    /** Parsed from the answer's brand object; nothing in it is used as it arrived. */
    brand: Brand;
    report: ReportView;
    ranges: ReportRange[];
}

export class ReportNotFoundError extends Error {
    constructor() {
        super("report not found");
        this.name = "ReportNotFoundError";
    }
}

export async function fetchReport(token: string, range: ReportRange | null): Promise<PublicReport> {
    const qs = range ? `?range=${encodeURIComponent(range)}` : "";
    const res = await fetch(`/api/reports/${encodeURIComponent(token)}${qs}`, { headers: apiHeaders() });
    if (!res.ok) {
        const body = (await res.json().catch(() => null)) as { error?: string } | null;
        if (res.status === 403 && body?.error === "stale_page") throw new StalePageError();
        if (res.status === 404) throw new ReportNotFoundError();
        throw new Error(`request failed (${res.status})`);
    }
    const raw = (await res.json()) as Omit<PublicReport, "brand"> & { brand?: unknown };
    return { ...raw, brand: parseBrand(raw.brand) };
}
