import type Session from "../../models/auth/Session";
import Request from "../Request";

/**
 * Answers a sign-in that came back `password_change_required`: the password
 * was an issued initial one, and this replaces it with the holder's own. The
 * result continues the sign-in in the same shape POST /auth/login returns.
 */
export default async function setInitialPassword(pending_token: string, password: string, device_token?: string): Promise<Session> {
    return await Request<Session>({
        method: "POST",
        url: "/auth/login/initial-password",
        data: { pending_token, password, device_token },
    });
}
