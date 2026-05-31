import Request from "../../Request";

// DELETE /unibox/scheduled/:task_id — DB-only cancel. The queued
// Cloud Task still fires; the server short-circuits on the cancelled
// status. Idempotent: a second cancel returns 404 because the task
// is no longer pending.
export default async function cancelScheduled(taskId: string): Promise<void> {
    await Request<void>({
        method: "DELETE",
        url: `/unibox/scheduled/${encodeURIComponent(taskId)}`,
        authorization: true,
    });
}
