import type { UniboxScheduledResult } from "@/lib/api/models/app/unibox/UniboxScheduled";
import Request from "../../Request";

interface ListScheduledOpts {
    // When set, the server returns only queued sends targeting this
    // thread. Used by ThreadView to render scheduled replies inline.
    threadId?: string;
}

export default async function listScheduled(
    opts: ListScheduledOpts = {},
): Promise<UniboxScheduledResult> {
    const params = new URLSearchParams();
    if (opts.threadId) params.set("thread_id", opts.threadId);
    const qs = params.toString();
    return await Request<UniboxScheduledResult>({
        method: "GET",
        url: qs ? `/unibox/scheduled?${qs}` : "/unibox/scheduled",
        authorization: true,
    });
}
