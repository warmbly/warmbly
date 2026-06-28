import Request from "../../Request";
import type { ReferralAttributionsPage } from "@/lib/api/models/app/subscription/Referral";

// GET /subscription/referral/attributions — the org's referrals, paginated by
// the same opaque next_cursor every other list uses.
export default async function listReferralAttributions(
    cursor?: string | null,
    limit = 20,
): Promise<ReferralAttributionsPage> {
    const qs = new URLSearchParams();
    if (cursor) qs.set("cursor", cursor);
    if (limit) qs.set("limit", String(limit));
    const suffix = qs.toString() ? `?${qs.toString()}` : "";

    const result = await Request<ReferralAttributionsPage>({
        method: "GET",
        url: `/subscription/referral/attributions${suffix}`,
        authorization: true,
    });

    return {
        data: result.data ?? [],
        pagination: result.pagination ?? { has_more: false, next_cursor: null },
    };
}
