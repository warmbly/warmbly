// /admin/discounts — discount / promo code management.

import { Request } from "@/lib/api/client";
import type {
    AdminDiscountRedemptionsResult,
    AdminDiscountsResult,
    CreateDiscountRequest,
    Discount,
    Plan,
    UpdateDiscountRequest,
} from "@/lib/api/models/admin";

export interface ListDiscountsParams {
    status?: string;
    search?: string;
    cursor?: string;
    limit?: number;
}

function toQuery(params: ListDiscountsParams): string {
    const q = new URLSearchParams();
    if (params.status && params.status !== "all") q.set("status", params.status);
    if (params.search) q.set("search", params.search);
    if (params.cursor) q.set("cursor", params.cursor);
    if (params.limit) q.set("limit", String(params.limit));
    const s = q.toString();
    return s ? `?${s}` : "";
}

export function listDiscounts(
    params: ListDiscountsParams = {},
): Promise<AdminDiscountsResult> {
    return Request({
        method: "GET",
        url: `/admin/discounts${toQuery(params)}`,
        authorization: true,
    });
}

export function getDiscount(id: string): Promise<Discount> {
    return Request({ method: "GET", url: `/admin/discounts/${id}`, authorization: true });
}

export function createDiscount(body: CreateDiscountRequest): Promise<Discount> {
    return Request({
        method: "POST",
        url: "/admin/discounts",
        data: body,
        authorization: true,
    });
}

export function updateDiscount(
    id: string,
    body: UpdateDiscountRequest,
): Promise<Discount> {
    return Request({
        method: "PATCH",
        url: `/admin/discounts/${id}`,
        data: body,
        authorization: true,
    });
}

export function deleteDiscount(id: string): Promise<{ message: string }> {
    return Request({
        method: "DELETE",
        url: `/admin/discounts/${id}`,
        authorization: true,
    });
}

export function listDiscountRedemptions(
    id: string,
    cursor?: string,
): Promise<AdminDiscountRedemptionsResult> {
    const q = cursor ? `?cursor=${cursor}` : "";
    return Request({
        method: "GET",
        url: `/admin/discounts/${id}/redemptions${q}`,
        authorization: true,
    });
}

// Plans for the eligibility selector. Tolerates both {plans} and {data} shapes
// since the plans endpoint envelope differs from the paginated lists.
export async function listPlansForEligibility(): Promise<Plan[]> {
    const res = await Request<{ plans?: Plan[]; data?: Plan[] }>({
        method: "GET",
        url: "/admin/plans",
        authorization: true,
    });
    return res.plans ?? res.data ?? [];
}
