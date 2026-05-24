import type UniboxThread from "@/lib/api/models/app/unibox/UniboxThread";
import Request from "../../Request";

export default async function getThread(threadId: string): Promise<UniboxThread> {
    const params = new URLSearchParams();
    params.append("thread_id", threadId);
    const url = `/unibox/thread?${params.toString()}`;

    return await Request<UniboxThread>({
        method: "GET",
        url,
        authorization: true,
    })
}
