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
    return Request({
        method: "GET",
        url: "/admin/warmup/pools",
        authorization: true,
    });
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
): Promise<WarmupAppealsResult> {
    const usp = new URLSearchParams();
    if (status !== "all") usp.set("status", status);
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
