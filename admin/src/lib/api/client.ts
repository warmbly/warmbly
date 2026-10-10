// Axios instance + the Request<T> helper used by every API module.
//
// Behaviour mirrors web/src/lib/api/client/Request.ts:
//   - access token attached on `authorization: true` calls
//   - one-flight refresh lock so concurrent requests share a single
//     /auth/refresh round-trip when the access token expires
//   - 401 retried once after a refresh, then bubbles a SessionExpired
//   - reauth_required opens the confirm-it-is-you dialog, then retries once

import axios, { type AxiosRequestConfig } from "axios";
import { API_URL } from "@/lib/env";
import { noteStep } from "@/lib/observability";
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
    // Machine-readable code + request id from the backend's standard error
    // envelope ({ error, message, code, request_id }). Surfaced in the UI so a
    // failed page can show exactly what broke instead of a generic message.
    code?: string;
    requestId?: string;
    // The call that failed, path only: no query string, so no filter value
    // reaches an error report.
    method?: string;
    path?: string;
    body?: T;
    constructor(message: string, status: number, body?: T) {
        super(message);
        this.name = "APIError";
        this.status = status;
        this.body = body;
        const b = body as { code?: string; request_id?: string } | undefined;
        this.code = b?.code;
        this.requestId = b?.request_id;
    }
}

const http = axios.create({ baseURL: API_URL });

interface AuthRequestConfig extends AxiosRequestConfig {
    authorization?: boolean;
    // Set on the reauth call itself, so a wrong code reaches the dialog.
    skipReauthPrompt?: boolean;
}

// Long enough to find an authenticator; a missing dialog fails instead of hanging.
const REAUTH_PROMPT_TIMEOUT_MS = 2 * 60 * 1000;

// promptForReauth asks ReauthDialog (mounted in the app shell) to confirm the
// operator, resolving on success and rejecting on cancel.
export function promptForReauth(): Promise<void> {
    return new Promise<void>((resolve, reject) => {
        let settled = false;
        let timer = 0;
        const finish = (fn: () => void) => () => {
            if (settled) return;
            settled = true;
            window.clearTimeout(timer);
            fn();
        };
        timer = window.setTimeout(
            finish(() => reject(new Error("no confirmation prompt is available here"))),
            REAUTH_PROMPT_TIMEOUT_MS,
        );
        window.dispatchEvent(
            new CustomEvent("reauth-required", {
                detail: {
                    resolve: finish(resolve),
                    reject: finish(() => reject(new Error("cancelled"))),
                    // A mounted prompt takes over; the operator may take as long as they need.
                    ack: () => window.clearTimeout(timer),
                },
            }),
        );
    });
}

let refreshPromise: Promise<AdminToken> | null = null;

async function refreshTokens(refreshToken: string): Promise<AdminToken> {
    const res = await axios.post<AdminToken>(`${API_URL}/v1/auth/refresh`, {
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
            const body = (err.response?.data ?? {}) as { error?: string; message?: string; code?: string };
            // A change that needs a fresher proof of identity: confirm, then retry once.
            if (
                body.code === "reauth_required" &&
                config.authorization &&
                !config.skipReauthPrompt &&
                typeof window !== "undefined"
            ) {
                await promptForReauth();
                const retry = await http.request<T>({ ...config, headers });
                return retry.data;
            }
            const failure = new APIError(
                body.error || body.message || err.message || "Request failed",
                status,
                err.response?.data,
            );
            noteFailure(config, failure);
            throw failure;
        }
        throw err;
    }
}

// noteFailure leaves the failed call on the trail the next exception carries.
//
// A page that throws because a response came back empty is unreadable on its
// own and obvious next to "GET /admin/orgs/:id 500 req-abc123". The request id
// is the backend's own, so the same incident is findable in its logs.
//
// The path and nothing else about the call travels: no query string, no body,
// no header.
function noteFailure(config: AuthRequestConfig, failure: APIError): void {
    // Axios sends an omitted method as GET.
    const method = config.method?.toUpperCase() ?? "GET";
    const path = config.url?.split("?")[0] ?? "";
    failure.method = method;
    failure.path = path;

    const properties: Record<string, string | number | boolean> = {
        method,
        path,
        status: failure.status,
    };
    if (failure.code) properties.code = failure.code;
    if (failure.requestId) properties.request_id = failure.requestId;

    noteStep(`${method} ${path} ${failure.status || "failed"}`, properties);
}
