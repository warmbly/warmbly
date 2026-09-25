import Request from "../../Request";
import { pairedChunks, UNIBOX_BULK_MAX } from "./chunks";

// PATCH /unibox/seen marks unibox emails seen/unseen. The backend body is
// { email_ids, thread_ids, folder, seen } (models.MarkSeen); callers pass
// { ids } for an explicit list, { threadIds } for whole conversations, or
// { folder } to sweep a whole folder, and seen defaults to true (mark as
// read). Sending the wrong field names makes the server bind an empty list and
// silently no-op, which is why the unread bar never cleared before.
export default async function markSeen(data: {
    ids?: string[];
    threadIds?: string[];
    folder?: string;
    seen?: boolean;
}): Promise<void> {
    if (data.folder) {
        return await Request<void>({
            method: "PATCH",
            url: `/unibox/seen`,
            data: { email_ids: [], thread_ids: [], folder: data.folder, seen: data.seen ?? true },
            authorization: true,
        });
    }
    for (const part of pairedChunks(data.ids ?? [], data.threadIds ?? [], UNIBOX_BULK_MAX)) {
        await Request<void>({
            method: "PATCH",
            url: `/unibox/seen`,
            data: {
                email_ids: part.a,
                thread_ids: part.b,
                seen: data.seen ?? true,
            },
            authorization: true,
        });
    }
}
