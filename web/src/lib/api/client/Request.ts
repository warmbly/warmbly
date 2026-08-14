import type { AxiosRequestConfig } from "axios"
import Client from "./Client"
import getToken from "@/lib/helper/getToken"
import isExpired from "@/lib/helper/isExpired";
import { NoToken, SessionExpired } from "@/lib/errors/auth";
import refreshTokenFn from "./auth/refreshToken";
import setToken from "@/lib/helper/setToken";
import reviveDates from "@/lib/helper/reviveDates";
import type { AppError } from "./normalizeError";
import { clearTokens } from "@/lib/auth";
import type Token from "@/lib/api/models/auth/Token";
import { CONFENGE_OPERATOR_MODE } from "@/lib/information";
import { ensureConfengeOperatorSession } from "./auth/confengeOperatorSession";

interface AuthRequestConfig extends AxiosRequestConfig {
    authorization?: boolean
}

// Refresh lock: only one refresh at a time, others wait for it
let refreshPromise: Promise<Token> | null = null;

async function ensureValidToken(): Promise<Token> {
    const token = getToken();
    if (!token) {
        if (CONFENGE_OPERATOR_MODE) return ensureConfengeOperatorSession();
        throw NoToken;
    }

    if (token.access_token && !isExpired(token.access_token_expires_at)) {
        return token;
    }

    // Access token expired — need to refresh
    if (!token.refresh_token || isExpired(token.refresh_token_expires_at)) {
        clearTokens();
        if (CONFENGE_OPERATOR_MODE) return ensureConfengeOperatorSession(true);
        throw SessionExpired;
    }

    // If a refresh is already in progress, wait for it
    if (refreshPromise) {
        try {
            await refreshPromise;
            const updated = getToken();
            if (updated && updated.access_token && !isExpired(updated.access_token_expires_at)) {
                return updated;
            }
            throw SessionExpired;
        } catch {
            throw SessionExpired;
        }
    }

    // Start a new refresh
    refreshPromise = refreshTokenFn(token.refresh_token);
    try {
        const newToken = await refreshPromise;
        setToken(newToken);
        return newToken;
    } catch {
        clearTokens();
        if (CONFENGE_OPERATOR_MODE) return ensureConfengeOperatorSession(true);
        throw SessionExpired;
    } finally {
        refreshPromise = null;
    }
}

export default async function Request<T>(config: AuthRequestConfig): Promise<T> {
    if (config.authorization) {
        const token = await ensureValidToken();

        config.headers = {
            ...config.headers,
            Authorization: `Bearer ${token.access_token}`,
        }
    }

    try {
        const res = await Client.request(config)
        return reviveDates(res.data)
    } catch (error) {
        const appErr = error as AppError;

        // If we get a 401 on an authorized request, try refreshing once
        if (config.authorization && (appErr?.status === 401)) {
            try {
                if (CONFENGE_OPERATOR_MODE) {
                    clearTokens();
                    const token = await ensureConfengeOperatorSession(true);
                    config.headers = {
                        ...config.headers,
                        Authorization: `Bearer ${token.access_token}`,
                    };
                    const res = await Client.request(config);
                    return reviveDates(res.data);
                }
                const token = await ensureValidToken();
                config.headers = {
                    ...config.headers,
                    Authorization: `Bearer ${token.access_token}`,
                }
                const res = await Client.request(config)
                return reviveDates(res.data)
            } catch {
                clearTokens();
                throw SessionExpired;
            }
        }

        if (appErr?.status === 401 || appErr?.redirect) {
            clearTokens();
            throw SessionExpired;
        }

        // A denied WRITE action (edit/save/delete) gets one clear, app-wide
        // popup explaining the missing permission (or plan). Reads that 403 are
        // intentionally left to page-level gating (locked surfaces / NoAccess),
        // so we only surface this for mutating methods.
        if (appErr?.status === 403 && typeof window !== "undefined") {
            const method = String(config.method ?? "get").toUpperCase();
            if (method !== "GET" && method !== "HEAD") {
                window.dispatchEvent(
                    new CustomEvent("permission-denied", {
                        detail: { message: appErr.message },
                    }),
                );
            }
        }
        throw error;
    }
}
