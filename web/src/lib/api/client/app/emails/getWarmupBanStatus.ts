import type WarmupBanStatus from "@/lib/api/models/app/emails/WarmupBanStatus";
import Request from "../../Request";

// Reads whether a mailbox has been blocked from the warmup pool, why, and
// whether the owner can still appeal. Drives the ban banner + appeal form in
// the mailbox detail drawer's Warmup tab.
export default async function getWarmupBanStatus(id: string): Promise<WarmupBanStatus> {
    return await Request<WarmupBanStatus>({
        method: "GET",
        url: `/emails/${id}/warmup/ban-status`,
        authorization: true,
    });
}
