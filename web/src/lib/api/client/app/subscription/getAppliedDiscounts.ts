import Request from "../../Request";
import type { DiscountRedemptionsResult } from "@/lib/api/models/app/subscription/DiscountRedemption";

// GET /subscription/discounts — the org's promo redemption history.
export default async function getAppliedDiscounts(
    limit = 20,
): Promise<DiscountRedemptionsResult> {
    const qs = new URLSearchParams();
    if (limit) qs.set("limit", String(limit));
    const suffix = qs.toString() ? `?${qs.toString()}` : "";

    const result = await Request<DiscountRedemptionsResult>({
        method: "GET",
        url: `/subscription/discounts${suffix}`,
        authorization: true,
    });

    return { data: result.data ?? [] };
}
