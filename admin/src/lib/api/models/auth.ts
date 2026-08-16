// Shapes returned by /auth/*. Kept in lockstep with web/src/lib/api/models/auth/.

import type { AdminToken } from "@/lib/auth/storage";

export interface LoginRequest {
    email: string;
    password: string;
    // Cloudflare Turnstile token. The backend's /auth/login verifies this
    // (TURNSTILE_BYPASS_TOKEN short-circuits it in dev). See TurnstileModal.
    turnstile: string;
}

// Step 1 (/auth/login) verifies the password + captcha.
//
// With code_required true it emailed a one-time code and returned a short-lived
// session token, not the access token. With code_required false the deployment
// has AUTH_LOGIN_CODE off (or already knows this device), so nothing was sent
// and the login is already resolved: either token, or a 2FA challenge.
export interface LoginStartResponse {
    session?: string;
    code_required: boolean;
    token?: LoginResponse;
    two_fa_required?: boolean;
    pending_token?: string;
    expires_in?: number;
}

// Returned by /auth/login/confirm: the token pair, or a 2FA challenge when the
// operator has TOTP enrolled.
export interface LoginConfirmResponse extends Partial<AdminToken> {
    two_fa_required?: boolean;
    pending_token?: string;
    expires_in?: number;
}

export interface TwoFAVerifyRequest {
    pending_token: string;
    code: string;
}

// Step 2 (/auth/login/confirm) exchanges that session + the emailed code for
// the real access/refresh token pair.
export interface LoginConfirmRequest {
    session: string;
    code: string;
}

// The token pair, returned by /auth/login/confirm and /auth/refresh.
export type LoginResponse = AdminToken;

export interface AdminProfile {
    id: string;
    email: string;
    first_name?: string;
    last_name?: string;
    avatar?: string;
    is_admin?: boolean;
    // Bitmask, not a list: the backend serializes models.AdminPermission (uint32).
    admin_permissions?: number;
    [k: string]: unknown;
}
