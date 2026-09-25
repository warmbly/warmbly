import Request from "../../Request";
import { pairedChunks, UNIBOX_BULK_MAX } from "./chunks";

// The three folders a user can file a conversation into. sent/drafts/spam are
// verdicts the provider reaches, and the backend refuses them here.
export type FilableFolder = "inbox" | "archive" | "trash";

// PATCH /unibox/folder re-files messages. Archive in the thread header is
// "archive", Delete is "trash", Move to inbox is "inbox". Store-side only: the
// provider copy stays put, and the sync knows not to undo it.
//
// Address it by thread wherever the caller has one. A list row knows its
// conversation and not the message ids inside it, and filing part of a
// conversation leaves it in the view it was filed out of.
export default async function moveFolder(data: {
    ids?: string[];
    threadIds?: string[];
    folder: FilableFolder;
}): Promise<void> {
    for (const part of pairedChunks(data.ids ?? [], data.threadIds ?? [], UNIBOX_BULK_MAX)) {
        await Request<void>({
            method: "PATCH",
            url: `/unibox/folder`,
            data: {
                email_ids: part.a,
                thread_ids: part.b,
                folder: data.folder,
            },
            authorization: true,
        });
    }
}
