import type Token from "../../models/auth/Token";
import Request from "../Request";

export interface ClaimSetupInput {
    token: string;
    email: string;
    password: string;
    first_name?: string;
    last_name?: string;
}

/**
 * Exchanges the one-time link printed at first boot for the owner account.
 *
 * Returns a session directly: whoever holds the token has already proved they
 * control the server, so sending them back to a login form adds friction and
 * no security.
 */
export default async function claimSetup(data: ClaimSetupInput): Promise<Token> {
    return await Request<Token>({
        method: "POST",
        url: "/auth/setup",
        data,
    });
}
