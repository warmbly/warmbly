import Request from "../../Request";

export interface CancelSubscriptionInput {
    // true schedules the cancellation for the end of the paid period; false
    // clears a scheduled cancellation and resumes the subscription. The
    // endpoint binds this body, so it is required — sending none is a 400.
    cancel_at_period_end: boolean;
}

export default async function cancelSubscription(
    data: CancelSubscriptionInput,
): Promise<void> {
    return await Request<void>({
        method: "POST",
        url: `/subscription/cancel`,
        data,
        authorization: true,
    })
}
