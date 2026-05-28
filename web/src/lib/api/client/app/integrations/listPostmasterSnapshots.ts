import type { PostmasterSnapshot } from "@/lib/api/models/app/integrations/Integration";
import Request from "../../Request";

export default async function listPostmasterSnapshots(source?: string, target?: string): Promise<{ snapshots: PostmasterSnapshot[] }> {
    const qs = new URLSearchParams();
    if (source) qs.set("source", source);
    if (target) qs.set("target", target);
    const suffix = qs.toString() ? `?${qs.toString()}` : "";
    return await Request<{ snapshots: PostmasterSnapshot[] }>({
        method: "GET",
        url: `/integrations/postmaster/snapshots${suffix}`,
        authorization: true,
    });
}
