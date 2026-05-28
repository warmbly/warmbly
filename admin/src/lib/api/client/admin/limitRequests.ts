// /admin/limit-requests — review queue for customer-submitted limit
// increase requests.

import { Request } from "@/lib/api/client";
import type {
    LimitIncreaseRequest,
    LimitRequestStatus,
} from "@/lib/api/models/admin";

export function listLimitRequests(
    status: LimitRequestStatus | "all" = "pending",
    limit = 50,
): Promise<{ data: LimitIncreaseRequest[] }> {
    const usp = new URLSearchParams();
    if (status !== "all") usp.set("status", status);
    usp.set("limit", String(limit));
    return Request({
        method: "GET",
        url: `/admin/limit-requests?${usp.toString()}`,
        authorization: true,
    });
}

export function approveLimitRequest(
    id: string,
    notes: string,
): Promise<LimitIncreaseRequest> {
    return Request({
        method: "POST",
        url: `/admin/limit-requests/${id}/approve`,
        authorization: true,
        data: { notes },
    });
}

export function rejectLimitRequest(
    id: string,
    notes: string,
): Promise<LimitIncreaseRequest> {
    return Request({
        method: "POST",
        url: `/admin/limit-requests/${id}/reject`,
        authorization: true,
        data: { notes },
    });
}
