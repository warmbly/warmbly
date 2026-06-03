import type { WarmupAppealResult } from "@/lib/api/models/app/emails/WarmupBanStatus";
import Request from "../../Request";

// Submits an appeal against a warmup-pool ban for a mailbox. The backend
// returns 400 if the mailbox isn't blocked, an appeal is already pending, or
// the reason is empty.
export default async function appealWarmupBan(id: string, reason: string): Promise<WarmupAppealResult> {
    return await Request<WarmupAppealResult>({
        method: "POST",
        url: `/emails/${id}/warmup/appeal`,
        data: { reason },
        authorization: true,
    });
}
