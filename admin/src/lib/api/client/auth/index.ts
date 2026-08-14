// Thin wrappers around the auth endpoints we need in the admin app.
// Login + me + logout — registration / password reset / onboarding live
// in the dashboard app and are not duplicated here.

import { Request } from "@/lib/api/client";
import type {
    LoginRequest,
    LoginStartResponse,
    LoginConfirmRequest,
    LoginConfirmResponse,
    LoginResponse,
    TwoFAVerifyRequest,
    AdminProfile,
} from "@/lib/api/models/auth";

// Step 1: verify password + captcha. Emails a one-time code, returns a session.
export function login(input: LoginRequest): Promise<LoginStartResponse> {
    return Request<LoginStartResponse>({
        method: "POST",
        url: "/v1/auth/login",
        data: input,
        timeout: 15_000,
    });
}

// Step 2: exchange the session + emailed code for the access/refresh token pair.
export function loginConfirm(input: LoginConfirmRequest): Promise<LoginConfirmResponse> {
    return Request<LoginConfirmResponse>({
        method: "POST",
        url: "/v1/auth/login/confirm",
        data: input,
        timeout: 15_000,
    });
}

// Exchanges the single-use pending token + a TOTP or recovery code for the
// real token pair. The admin panel had no 2FA step at all, so an operator who
// enrolled TOTP was locked out of it entirely.
export function verifyTwoFA(input: TwoFAVerifyRequest): Promise<LoginResponse> {
    return Request<LoginResponse>({
        method: "POST",
        url: "/v1/auth/2fa/verify",
        data: input,
        timeout: 15_000,
    });
}

export function getMe(): Promise<AdminProfile> {
    return Request<AdminProfile>({
        method: "GET",
        url: "/v1/auth/me",
        authorization: true,
    });
}

export function logout(): Promise<void> {
    return Request<void>({
        method: "POST",
        url: "/v1/auth/logout",
        authorization: true,
    });
}
