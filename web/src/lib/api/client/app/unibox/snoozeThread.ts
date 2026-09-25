import Request from "../../Request";
import { inChunks, UNIBOX_BULK_MAX } from "./chunks";

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

// The selection bar's form: one call for a whole selection rather than one per
// row. The server answers with `data` when several are named.
export async function snoozeThreads(
    threadIds: string[],
    until: Date,
): Promise<void> {
    await inChunks(threadIds, UNIBOX_BULK_MAX, (chunk) =>
        Request<{ data: SnoozeResponse[] }>({
            method: "POST",
            url: "/unibox/snooze",
            authorization: true,
            data: { thread_ids: chunk, snoozed_until: until.toISOString() },
        }),
    );
}

export async function unsnoozeThread(threadId: string): Promise<void> {
    return await unsnoozeThreads([threadId]);
}

// Ids travel in the query string here, so the batches are smaller than the
// server's cap to keep each URL a sane length.
export async function unsnoozeThreads(threadIds: string[]): Promise<void> {
    await inChunks(threadIds, 100, (chunk) =>
        Request<void>({
            method: "DELETE",
            url: `/unibox/snooze?${new URLSearchParams({ thread_id: chunk.join(",") }).toString()}`,
            authorization: true,
        }),
    );
}
