import Request from "../../Request";

export default async function createPortalSession(): Promise<{ url: string }> {
    return await Request<{ url: string }>({
        method: "POST",
        url: `/subscription/portal`,
        authorization: true,
    })
}
