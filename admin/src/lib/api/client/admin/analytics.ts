// /admin/analytics/*
//
// Shapes mirror internal/models/admin.go (PlatformOverview, AnalyticsTrends,
// the *Stats rows) and admin_ops.go (AdminAcquisition). The timeseries
// handlers take start_date/end_date and answer {stats: [...]}, so the
// helpers below translate a "last N days" window and unwrap the rows.

import { Request } from "@/lib/api/client";
import type {
    AnalyticsTrends,
    DailyEmailStat,
    HourlyEmailStat,
    UserGrowthStat,
} from "@/lib/api/models/admin";

// models.PlatformOverview. Optional so a page renders "—" for a field an
// older backend does not send instead of crashing.
export interface AdminPlatformOverview {
    total_users?: number;
    active_users?: number;
    new_users_today?: number;
    new_users_this_week?: number;
    total_campaigns?: number;
    active_campaigns?: number;
    total_emails_sent?: number;
    emails_sent_today?: number;
    total_workers?: number;
    active_workers?: number;
    warmup_blocked_count?: number;
    pending_appeals?: number;
    active_subscriptions?: number;
    trialing_users?: number;
}

export function getPlatformOverview(): Promise<AdminPlatformOverview> {
    return Request({
        method: "GET",
        url: "/admin/analytics/overview",
        authorization: true,
    });
}

export function getAnalyticsTrends(): Promise<AnalyticsTrends> {
    return Request({
        method: "GET",
        url: "/admin/analytics/trends",
        authorization: true,
    });
}

function isoDate(d: Date): string {
    return d.toISOString().slice(0, 10);
}

// parseDateRange on the backend reads start_date/end_date as YYYY-MM-DD.
function rangeQuery(days: number): string {
    const end = new Date();
    const start = new Date(end.getTime() - days * 24 * 60 * 60 * 1000);
    return `start_date=${isoDate(start)}&end_date=${isoDate(end)}`;
}

export async function getDailyEmailStats(days = 30): Promise<DailyEmailStat[]> {
    const res = await Request<{ stats: DailyEmailStat[] | null }>({
        method: "GET",
        url: `/admin/analytics/emails/daily?${rangeQuery(days)}`,
        authorization: true,
    });
    return res.stats ?? [];
}

export async function getHourlyEmailStats(): Promise<HourlyEmailStat[]> {
    const res = await Request<{ stats: HourlyEmailStat[] | null }>({
        method: "GET",
        url: "/admin/analytics/emails/hourly",
        authorization: true,
    });
    return res.stats ?? [];
}

export async function getUserGrowthStats(days = 30): Promise<UserGrowthStat[]> {
    const res = await Request<{ stats: UserGrowthStat[] | null }>({
        method: "GET",
        url: `/admin/analytics/users/growth?${rangeQuery(days)}`,
        authorization: true,
    });
    return res.stats ?? [];
}

// models.AdminAcquisition: signups by channel over a window.
export interface AdminAcquisitionChannel {
    source: string;
    medium: string;
    signups: number;
    converted: number;
}

export interface AdminAcquisitionReferrer {
    host: string;
    signups: number;
}

export interface AdminAcquisition {
    days: number;
    signups: number;
    with_channel: number;
    converted: number;
    trials_expiring_7d: number;
    channels: AdminAcquisitionChannel[] | null;
    referrers: AdminAcquisitionReferrer[] | null;
}

export function getAcquisition(days = 30): Promise<AdminAcquisition> {
    return Request({
        method: "GET",
        url: `/admin/analytics/acquisition?days=${days}`,
        authorization: true,
    });
}
