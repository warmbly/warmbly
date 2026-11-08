import type Token from "../../models/auth/Token";
import Request from "../Request";

export interface PortalHandoff {
    redirect_url: string;
    expires_in: number;
}

/**
 * Asks the main address for a one-time code that signs this account in to one
 * agency portal. `accessToken` names a session that was signed in only for this,
 * which `endSession` then ends; without it the saved session is used and kept.
 */
export async function createPortalHandoff(host: string, opts: { accessToken?: string; endSession?: boolean } = {}): Promise<PortalHandoff> {
    return await Request<PortalHandoff>({
        method: "POST",
        url: "/auth/portal/handoff",
        data: { host, end_session: !!opts.endSession },
        ...(opts.accessToken
            ? { headers: { Authorization: `Bearer ${opts.accessToken}` } }
            : { authorization: true }),
    });
}

/** The portal trades its code, once, for a session that reaches this agency's clients. */
export async function exchangePortalHandoff(code: string): Promise<Token> {
    return await Request<Token>({ method: "POST", url: "/auth/portal/exchange", data: { code } });
}
