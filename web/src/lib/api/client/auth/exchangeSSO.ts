import type Token from "../../models/auth/Token";
import Request from "../Request";

/**
 * Swaps the single-use code from an SSO redirect for the real session.
 *
 * The backend holds the session and hands back only an opaque code, so no token
 * ever lands in a URL, browser history or proxy log.
 */
export default async function exchangeSSO(code: string): Promise<Token> {
    return await Request<Token>({
        method: "POST",
        url: "/auth/oidc/exchange",
        data: { code },
    });
}
