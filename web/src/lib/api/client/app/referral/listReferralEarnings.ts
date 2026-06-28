import Request from "../../Request";
import type { ReferralEarningsPage } from "@/lib/api/models/app/subscription/Referral";

// GET /subscription/referral/earnings — the org's reward/clawback ledger,
// paginated by the same opaque next_cursor every other list uses.
export default async function listReferralEarnings(
    cursor?: string | null,
    limit = 20,
): Promise<ReferralEarningsPage> {
    const qs = new URLSearchParams();
    if (cursor) qs.set("cursor", cursor);
    if (limit) qs.set("limit", String(limit));
    const suffix = qs.toString() ? `?${qs.toString()}` : "";

    const result = await Request<ReferralEarningsPage>({
        method: "GET",
        url: `/subscription/referral/earnings${suffix}`,
        authorization: true,
    });

    return {
        data: result.data ?? [],
        pagination: result.pagination ?? { has_more: false, next_cursor: null },
    };
}
