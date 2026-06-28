import Request from "../../Request";
import type { ReferralSummary } from "@/lib/api/models/app/subscription/Referral";

// GET /subscription/referral — mints the code on first view, so this always
// returns a ready code + share_url.
export default async function getReferral(): Promise<ReferralSummary> {
    return await Request<ReferralSummary>({
        method: "GET",
        url: `/subscription/referral`,
        authorization: true,
    });
}
