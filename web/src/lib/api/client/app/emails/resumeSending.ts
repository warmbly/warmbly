import Request from "../../Request";

export interface SendPauseRelease {
    hold_released: boolean;
    cooldown_cleared: boolean;
}

// Bodyless and idempotent: lifts a provider pause or refusal hold the owner says is fixed.
export default async function resumeSending(id: string): Promise<SendPauseRelease> {
    return await Request<SendPauseRelease>({
        method: "POST",
        url: `/emails/${id}/resume-sending`,
        authorization: true,
    });
}
