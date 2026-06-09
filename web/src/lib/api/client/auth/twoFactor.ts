import type Token from "../../models/auth/Token";
import Request from "../Request";

export interface TwoFactorEnrollStart {
    secret: string;
    otpauth_uri: string;
}

export async function twoFactorStatus(): Promise<{ enabled: boolean }> {
    return await Request<{ enabled: boolean }>({
        method: "GET",
        url: "/auth/2fa/status",
        authorization: true,
    });
}

export async function twoFactorEnrollStart(): Promise<TwoFactorEnrollStart> {
    return await Request<TwoFactorEnrollStart>({
        method: "POST",
        url: "/auth/2fa/enroll/start",
        authorization: true,
    });
}

export async function twoFactorEnrollConfirm(code: string): Promise<{ recovery_codes: string[] }> {
    return await Request<{ recovery_codes: string[] }>({
        method: "POST",
        url: "/auth/2fa/enroll/confirm",
        data: { code },
        authorization: true,
    });
}

export async function twoFactorDisable(code: string): Promise<void> {
    await Request<void>({
        method: "DELETE",
        url: "/auth/2fa",
        data: { code },
        authorization: true,
    });
}

// Public — the user is not fully authenticated yet (mid-login challenge).
export async function twoFactorVerify(pending_token: string, code: string): Promise<Token> {
    return await Request<Token>({
        method: "POST",
        url: "/auth/2fa/verify",
        data: { pending_token, code },
    });
}
