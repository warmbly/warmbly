import Request from "../../Request";

// PATCH /unibox/seen marks unibox emails seen/unseen. The backend body is
// { email_ids, folder, seen } (models.MarkSeen); callers pass { ids } for an
// explicit list or { folder } to sweep a whole folder, and seen defaults to
// true (mark as read). Sending the wrong field names makes the server bind an
// empty list and silently no-op, which is why the unread bar never cleared
// before.
export default async function markSeen(data: { ids?: string[]; folder?: string; seen?: boolean }): Promise<void> {
    return await Request<void>({
        method: "PATCH",
        url: `/unibox/seen`,
        data: { email_ids: data.ids ?? [], folder: data.folder, seen: data.seen ?? true },
        authorization: true,
    })
}
