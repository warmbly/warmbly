// /admin/warmup/* — pool health, blocked-account triage, appeal review.

import { Request } from "@/lib/api/client";
import type {
    AdminBlockedAccountsResult,
    BlockAccountRequest,
    ReviewAppealRequest,
    WarmupAppeal,
    WarmupAppealsResult,
    WarmupPoolHealthSummary,
    WarmupPoolInfo,
} from "@/lib/api/models/admin";

export function getWarmupHealthSummary(): Promise<WarmupPoolHealthSummary> {
    return Request({
        method: "GET",
        url: "/admin/warmup/health",
        authorization: true,
    });
}

export function listWarmupPools(): Promise<WarmupPoolInfo[]> {
    // The backend wraps the list as { pools: [...] } and returns null (not [])
    // when empty — unwrap + default so the page always gets an array to map.
    return Request<{ pools: WarmupPoolInfo[] | null }>({
        method: "GET",
        url: "/admin/warmup/pools",
        authorization: true,
    }).then((r) => r.pools ?? []);
}

export function listBlockedWarmupAccounts(
    cursor?: string,
    limit = 50,
): Promise<AdminBlockedAccountsResult> {
    const usp = new URLSearchParams();
    if (cursor) usp.set("cursor", cursor);
    usp.set("limit", String(limit));
    return Request({
        method: "GET",
        url: `/admin/warmup/blocked?${usp.toString()}`,
        authorization: true,
    });
}

export function blockWarmupAccount(
    accountId: string,
    body: BlockAccountRequest,
): Promise<void> {
    return Request({
        method: "POST",
        url: `/admin/warmup/block/${accountId}`,
        authorization: true,
        data: body,
    });
}

export function unblockWarmupAccount(accountId: string): Promise<void> {
    return Request({
        method: "POST",
        url: `/admin/warmup/unblock/${accountId}`,
        authorization: true,
    });
}

export function listWarmupAppeals(
    status: "pending" | "approved" | "rejected" | "all" = "pending",
    cursor?: string,
    limit = 50,
): Promise<WarmupAppealsResult> {
    const usp = new URLSearchParams();
    if (status !== "all") usp.set("status", status);
    if (cursor) usp.set("cursor", cursor);
    usp.set("limit", String(limit));
    return Request({
        method: "GET",
        url: `/admin/warmup/appeals?${usp.toString()}`,
        authorization: true,
    });
}

export function getWarmupAppeal(id: string): Promise<WarmupAppeal> {
    return Request({
        method: "GET",
        url: `/admin/warmup/appeals/${id}`,
        authorization: true,
    });
}

export function approveAppeal(
    id: string,
    body: ReviewAppealRequest,
): Promise<void> {
    return Request({
        method: "POST",
        url: `/admin/warmup/appeals/${id}/approve`,
        authorization: true,
        data: body,
    });
}

export function rejectAppeal(
    id: string,
    body: ReviewAppealRequest,
): Promise<void> {
    return Request({
        method: "POST",
        url: `/admin/warmup/appeals/${id}/reject`,
        authorization: true,
        data: body,
    });
}

// ---- abuse signals and admin action history ----
// Shapes mirror AdminWarmupAbuseRow / AdminWarmupAction in
// internal/models/admin_ops.go.

export type WarmupAbuseWindow = "24h" | "7d" | "30d";

export interface AdminWarmupAbuseRow {
    email_account_id: string;
    email: string;
    organization_id?: string | null;
    organization_name: string;
    attempts: number;
    last_attempt_at: string;
    blocked: boolean;
    spam_score: number;
    health_state: string;
}

export interface AdminWarmupAction {
    id: string;
    admin_user_id: string;
    admin_email: string;
    email_account_id: string;
    email: string;
    action: string;
    reason?: string | null;
    created_at: string;
}

export function listWarmupAbuse(
    window: WarmupAbuseWindow = "7d",
    limit = 100,
): Promise<{ data: AdminWarmupAbuseRow[] | null }> {
    return Request({
        method: "GET",
        url: `/admin/warmup/abuse?window=${window}&limit=${limit}`,
        authorization: true,
    });
}

export function listWarmupActions(limit = 200): Promise<{ data: AdminWarmupAction[] | null }> {
    return Request({
        method: "GET",
        url: `/admin/warmup/actions?limit=${limit}`,
        authorization: true,
    });
}
