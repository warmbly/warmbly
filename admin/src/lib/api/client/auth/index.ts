// Thin wrappers around the auth endpoints we need in the admin app.
// Login + me + logout — registration / password reset / onboarding live
// in the dashboard app and are not duplicated here.

import { Request } from "@/lib/api/client";
import type { LoginRequest, LoginResponse, AdminProfile } from "@/lib/api/models/auth";

export function login(input: LoginRequest): Promise<LoginResponse> {
    return Request<LoginResponse>({
        method: "POST",
        url: "/auth/login",
        data: input,
    });
}

export function getMe(): Promise<AdminProfile> {
    return Request<AdminProfile>({
        method: "GET",
        url: "/auth/me",
        authorization: true,
    });
}

export function logout(): Promise<void> {
    return Request<void>({
        method: "POST",
        url: "/auth/logout",
        authorization: true,
    });
}
