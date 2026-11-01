import Request from "../Request";

// Emails the signed-in user a single-use link to set a password.
export default async function sendPasswordLink(): Promise<void> {
    await Request<void>({
        method: "POST",
        url: "/auth/me/password/link",
        authorization: true,
    });
}
