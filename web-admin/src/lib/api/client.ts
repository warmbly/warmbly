// Axios instance + the Request<T> helper used by every API module.
//
// Behaviour mirrors web/src/lib/api/client/Request.ts:
//   - access token attached on `authorization: true` calls
//   - one-flight refresh lock so concurrent requests share a single
//     /auth/refresh round-trip when the access token expires
//   - 401 retried once after a refresh, then bubbles a SessionExpired

import axios, { type AxiosRequestConfig } from "axios";
import { API_URL } from "@/lib/env";
import {
    clearToken,
    getToken,
    isExpired,
    setToken,
    type AdminToken,
} from "@/lib/auth/storage";

export class SessionExpiredError extends Error {
    constructor() {
        super("Session expired");
        this.name = "SessionExpiredError";
    }
}

export class APIError<T = unknown> extends Error {
    status: number;
    body?: T;
    constructor(message: string, status: number, body?: T) {
        super(message);
        this.name = "APIError";
        this.status = status;
        this.body = body;
    }
}

const http = axios.create({ baseURL: API_URL });

interface AuthRequestConfig extends AxiosRequestConfig {
    authorization?: boolean;
}

let refreshPromise: Promise<AdminToken> | null = null;

async function refreshTokens(refreshToken: string): Promise<AdminToken> {
    const res = await axios.post<AdminToken>(`${API_URL}/auth/refresh`, {
        refresh_token: refreshToken,
    });
    return res.data;
}

async function ensureValidToken(): Promise<AdminToken> {
    const token = getToken();
    if (!token) throw new SessionExpiredError();

    if (token.access_token && !isExpired(token.access_token_expires_at)) {
        return token;
    }

    if (!token.refresh_token || isExpired(token.refresh_token_expires_at)) {
        clearToken();
        throw new SessionExpiredError();
    }

    if (refreshPromise) {
        try {
            await refreshPromise;
            const next = getToken();
            if (next && !isExpired(next.access_token_expires_at)) return next;
            throw new SessionExpiredError();
        } catch {
            throw new SessionExpiredError();
        }
    }

    refreshPromise = refreshTokens(token.refresh_token);
    try {
        const next = await refreshPromise;
        setToken(next);
        return next;
    } catch {
        clearToken();
        throw new SessionExpiredError();
    } finally {
        refreshPromise = null;
    }
}

export async function Request<T>(config: AuthRequestConfig): Promise<T> {
    const headers = { ...(config.headers ?? {}) };

    if (config.authorization) {
        const tok = await ensureValidToken();
        (headers as Record<string, string>).Authorization = `Bearer ${tok.access_token}`;
    }

    try {
        const res = await http.request<T>({ ...config, headers });
        return res.data;
    } catch (err) {
        if (axios.isAxiosError(err)) {
            const status = err.response?.status ?? 0;
            // One-shot refresh on 401 for authorized requests.
            if (config.authorization && status === 401) {
                try {
                    const tok = await ensureValidToken();
                    (headers as Record<string, string>).Authorization =
                        `Bearer ${tok.access_token}`;
                    const retry = await http.request<T>({ ...config, headers });
                    return retry.data;
                } catch {
                    clearToken();
                    throw new SessionExpiredError();
                }
            }
            if (status === 401) {
                clearToken();
                throw new SessionExpiredError();
            }
            const body = (err.response?.data ?? {}) as { error?: string; message?: string };
            throw new APIError(
                body.error || body.message || err.message || "Request failed",
                status,
                err.response?.data,
            );
        }
        throw err;
    }
}
