import Request from "../../Request";

export interface SnoozeRequest {
    thread_id: string;
    /** RFC3339 timestamp — the snooze releases when this passes. */
    snoozed_until: string;
}

export interface SnoozeResponse {
    id: string;
    thread_id: string;
    snoozed_until: string;
}

export async function snoozeThread(req: SnoozeRequest): Promise<SnoozeResponse> {
    return await Request<SnoozeResponse>({
        method: "POST",
        url: "/unibox/snooze",
        authorization: true,
        data: req,
    });
}

export async function unsnoozeThread(threadId: string): Promise<void> {
    const usp = new URLSearchParams({ thread_id: threadId });
    await Request<void>({
        method: "DELETE",
        url: `/unibox/snooze?${usp.toString()}`,
        authorization: true,
    });
}
