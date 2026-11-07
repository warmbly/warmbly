import Request from "../../Request";

// Needs a fresh confirmation; the client prompts for it on reauth_required.
export default async function unlinkIdentity(provider: string): Promise<void> {
    await Request<void>({
        method: "DELETE",
        url: `/auth/me/identities/${encodeURIComponent(provider)}`,
        authorization: true,
    });
}
