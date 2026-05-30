// Fetches every message in a thread. email_id is optional now —
// the server scans across every mailbox the user owns when omitted,
// which is the right default for a unified-inbox click.

import type UniboxThread from "@/lib/api/models/app/unibox/UniboxThread";
import Request from "../../Request";

export default async function getThread(
    threadId: string,
    opts: { emailId?: string; limit?: number } = {},
): Promise<UniboxThread> {
    const params = new URLSearchParams();
    params.append("thread_id", threadId);
    if (opts.emailId) params.append("email_id", opts.emailId);
    if (opts.limit) params.append("limit", String(opts.limit));

    return await Request<UniboxThread>({
        method: "GET",
        url: `/unibox/thread?${params.toString()}`,
        authorization: true,
    })
}
