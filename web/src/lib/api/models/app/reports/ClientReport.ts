// Client reports: analytics shared outside the workspace. Mirrors internal/models/client_report.go.

import type MemberAccess from "@/lib/api/models/app/organizations/MemberAccess";

export type ClientReportRange = "last_7_days" | "last_30_days" | "this_month" | "last_month";

export type ClientReportCadence = "weekly" | "monthly";

export interface ClientReportSections {
    overview: boolean;
    campaigns: boolean;
    replies: boolean;
    positive_replies: boolean;
    mailboxes: boolean;
    daily_chart: boolean;
}

export interface ClientReportLink {
    /** First characters of the token, so a link can be recognised without being shown. */
    hint: string;
    created_at: Date;
    expires_at: Date | null;
    expired: boolean;
}

export interface ClientReportSchedule {
    recipients: string[];
    cadence: ClientReportCadence;
    /** 0 is Sunday; read for a weekly cadence. */
    weekday: number;
    /** 1..28; read for a monthly cadence. */
    month_day: number;
    hour: number;
    timezone: string;
    paused: boolean;
    last_sent_at: Date | null;
    next_run_at: Date;
}

export default interface ClientReport {
    id: string;
    created_by: string | null;
    name: string;
    access: MemberAccess;
    sections: ClientReportSections;
    show_reply_snippets: boolean;
    date_range: ClientReportRange;
    link: ClientReportLink | null;
    schedule: ClientReportSchedule | null;
    created_at: Date;
    updated_at: Date;
}

/** PATCH and POST body: absent fields are left unchanged (name is required on create). */
export interface ClientReportInput {
    name?: string;
    access?: MemberAccess;
    sections?: ClientReportSections;
    show_reply_snippets?: boolean;
    date_range?: ClientReportRange;
}

export interface ClientReportScheduleInput {
    recipients: string[];
    cadence: ClientReportCadence;
    weekday: number;
    month_day: number;
    hour: number;
    timezone: string;
    paused: boolean;
}

/** The only response that carries the link's URL; it cannot be read back later. */
export interface ClientReportLinkCreated {
    url: string;
    token: string;
    link: ClientReportLink;
    report: ClientReport;
}

export interface ClientReportPage {
    data: ClientReport[];
    pagination: { next_cursor: string | null; has_more: boolean };
}

export interface ClientReportOverview {
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

export interface ClientReportDay {
    date: string;
    sent: number;
    opens: number;
    replies: number;
}

export interface ClientReportCampaign {
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

export type ClientReportReplyClass = "positive" | "neutral" | "negative" | "unsubscribe" | "unknown" | "unclassified";

export interface ClientReportReply {
    name: string;
    email: string;
    campaign: string;
    subject: string;
    replied_at: Date;
    classification: ClientReportReplyClass;
    snippet?: string;
}

export interface ClientReportMailboxes {
    total: number;
    healthy: number;
    warning: number;
    error: number;
    warmup_sent: number;
    warmup_received: number;
    warmup_replied: number;
}

export interface ClientReportView {
    name: string;
    date_range: ClientReportRange;
    /** YYYY-MM-DD, inclusive. */
    from: string;
    to: string;
    generated_at: Date;
    sections: ClientReportSections;
    show_reply_snippets: boolean;
    overview?: ClientReportOverview;
    daily?: ClientReportDay[];
    campaigns?: ClientReportCampaign[];
    replies?: ClientReportReply[];
    mailboxes?: ClientReportMailboxes;
}
