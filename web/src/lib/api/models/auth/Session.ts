import type Token from "./Token";

/**
 * What POST /auth/login and /auth/register return.
 *
 * `session` plus `code_required: true` is the two-step flow: a code was emailed
 * and the caller confirms it. When the deployment has AUTH_LOGIN_CODE off, or
 * the device is already known, no code is sent and the login is already
 * complete, so the remaining fields carry the result instead.
 */
export default interface Session {
    session?: string;
    code_required: boolean;
    token?: Token;
    two_fa_required?: boolean;
    pending_token?: string;
    expires_in?: number;
}
