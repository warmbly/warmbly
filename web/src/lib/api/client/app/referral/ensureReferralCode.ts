import Request from "../../Request";
import type { ReferralCode } from "@/lib/api/models/app/subscription/Referral";

// POST /subscription/referral — idempotent "ensure" of the owner's referral
// code. GET already mints, so this is only needed for an explicit create.
export default async function ensureReferralCode(): Promise<ReferralCode> {
    return await Request<ReferralCode>({
        method: "POST",
        url: `/subscription/referral`,
        authorization: true,
    });
}
