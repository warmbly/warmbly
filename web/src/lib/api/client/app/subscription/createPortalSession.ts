import Request from "../../Request";

export interface CreatePortalInput {
    // Where Stripe sends the browser back to. The endpoint binds this as
    // required, so omitting it is a 400.
    return_url: string;
}

export default async function createPortalSession(
    data: CreatePortalInput,
): Promise<{ url: string }> {
    // The endpoint answers with `portal_url`; normalise it here so callers
    // keep reading `url`.
    const res = await Request<{ portal_url: string }>({
        method: "POST",
        url: `/subscription/portal`,
        data,
        authorization: true,
    })
    return { url: res.portal_url }
}
