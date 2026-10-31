import Request from "../../Request";
import type LinkedIdentity from "@/lib/api/models/auth/LinkedIdentity";

export default async function listLinkedIdentities(signal?: AbortSignal): Promise<LinkedIdentity[]> {
    const res = await Request<{ data: LinkedIdentity[] }>({
        method: "GET",
        url: "/auth/me/identities",
        authorization: true,
        signal,
    });
    return res.data;
}
