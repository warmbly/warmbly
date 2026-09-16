// /admin/discounts/* — the operator half of promo codes.
//
// The customer half (validate at the billing page, redeem at checkout) has
// always existed. This is what creates one: the backend stores the code and
// mints a matching one-off Stripe coupon per redemption, so nothing here talks
// to Stripe directly and a code is never created in the Stripe dashboard.

import { Request } from "@/lib/api/client";
import { buildSearchQuery } from "@/lib/api/client/admin/query";
import type {
    AdminDiscountRedemptionsResult,
    AdminDiscountSearch,
    AdminDiscountsResult,
    CreateDiscountCodeRequest,
    DiscountCode,
    UpdateDiscountCodeRequest,
} from "@/lib/api/models/admin";

export function listDiscounts(params: AdminDiscountSearch = {}): Promise<AdminDiscountsResult> {
    return Request({
        method: "GET",
        url: `/admin/discounts${buildSearchQuery(params as Record<string, unknown>)}`,
        authorization: true,
    });
}

export function getDiscount(id: string): Promise<DiscountCode> {
    return Request({ method: "GET", url: `/admin/discounts/${id}`, authorization: true });
}

export function createDiscount(body: CreateDiscountCodeRequest): Promise<DiscountCode> {
    return Request({ method: "POST", url: "/admin/discounts", authorization: true, data: body });
}

export function updateDiscount(
    id: string,
    body: UpdateDiscountCodeRequest,
): Promise<DiscountCode> {
    return Request({
        method: "PATCH",
        url: `/admin/discounts/${id}`,
        authorization: true,
        data: body,
    });
}

export function deleteDiscount(id: string): Promise<{ message: string }> {
    return Request({ method: "DELETE", url: `/admin/discounts/${id}`, authorization: true });
}

export function listDiscountRedemptions(
    id: string,
    params: { cursor?: string; limit?: number } = {},
): Promise<AdminDiscountRedemptionsResult> {
    return Request({
        method: "GET",
        url: `/admin/discounts/${id}/redemptions${buildSearchQuery(params as Record<string, unknown>)}`,
        authorization: true,
    });
}
