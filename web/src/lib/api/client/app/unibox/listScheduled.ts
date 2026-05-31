import type { UniboxScheduledResult } from "@/lib/api/models/app/unibox/UniboxScheduled";
import Request from "../../Request";

export default async function listScheduled(): Promise<UniboxScheduledResult> {
    return await Request<UniboxScheduledResult>({
        method: "GET",
        url: "/unibox/scheduled",
        authorization: true,
    });
}
