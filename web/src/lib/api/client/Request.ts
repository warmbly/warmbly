import type { AxiosRequestConfig } from "axios"
import Client from "./Client"
import getToken from "@/lib/helper/getToken"
import isExpired from "@/lib/helper/isExpired";
import { AuthError, noToken, sessionExpired } from "@/lib/errors/auth";
import refreshTokenFn from "./auth/refreshToken";
import setToken from "@/lib/helper/setToken";
import reviveDates from "@/lib/helper/reviveDates";
import { isRequestCancelled, type AppError } from "./normalizeError";
import { announceSessionEnded, endSession } from "@/lib/auth";
import type Token from "@/lib/api/models/auth/Token";

interface AuthRequestConfig extends AxiosRequestConfig {
    authorization?: boolean
    // Set on the re-authentication call itself, so a wrong password there
    // reaches the caller instead of reopening the prompt that made it.
    skipReauthPrompt?: boolean
    // Aborts the HTTP request; pass TanStack Query's queryFn signal here.
    signal?: AbortSignal
}

// promptForReauth opens the global "confirm it is you" dialog and resolves when
// the person confirms, rejects when they cancel.
//
// Some changes need a proof of identity newer than the session: minting an API
// key, registering a passkey, transferring a workspace, scheduling a deletion.
// Handling it here means every one of those retries automatically once the
// prompt is satisfied, rather than each call site growing its own dialog.
function promptForReauth(): Promise<void> {
    if (typeof window === "undefined") return Promise.reject(new Error("no window"));
    return new Promise<void>((resolve, reject) => {
        let settled = false;
        const once = (fn: () => void) => () => {
            if (settled) return;
            settled = true;
            fn();
        };

        // ReauthModal is mounted at the root layout. A gated call made when no
        // prompt is mounted or responded to would otherwise leave this promise
        // pending forever and the mutation spinning with nothing on screen, so a
        // listener that never answers is treated as a refusal.
        const timer = window.setTimeout(
            once(() => reject(new Error("no confirmation prompt is available here"))),
            REAUTH_PROMPT_TIMEOUT_MS,
        );
        const finish = (fn: () => void) =>
            once(() => {
                window.clearTimeout(timer);
                fn();
            });

        window.dispatchEvent(
            new CustomEvent("reauth-required", {
                detail: { resolve: finish(resolve), reject: finish(() => reject(new Error("cancelled"))) },
            }),
        );
    });
}

// Long enough for someone to find their authenticator, short enough that a
// missing prompt surfaces as an error rather than a hang.
const REAUTH_PROMPT_TIMEOUT_MS = 2 * 60 * 1000;

// Refresh lock: only one refresh at a time, others wait for it
let refreshPromise: Promise<Token> | null = null;

// refusedRefresh separates "the server rejected this refresh token" from "the
// request never got an answer". Only the first ends a session: a 500, a
// timeout, a CORS failure or a dropped connection says nothing about whether
// the token is still good. Treating them alike meant one bad minute on the API
// signed out everyone whose access token happened to expire during it, and
// threw away a refresh token the server had never refused.
function refusedRefresh(err: unknown): boolean {
    const status = (err as AppError | null)?.status;
    return typeof status === "number" && status >= 400 && status < 500;
}

async function ensureValidToken(): Promise<Token> {
    const token = getToken();
    if (!token) {
        // The token can go away without this tab being the one that dropped it:
        // a sign-out in another tab, storage cleared, a session ended while
        // this tab was in the background. Every page it has rendered is still
        // mounted and every poll on them still runs, so without saying so here
        // the polls throw this once a minute for the life of the tab and
        // nothing ever navigates.
        announceSessionEnded();
        throw noToken();
    }

    if (token.access_token && !isExpired(token.access_token_expires_at)) {
        return token;
    }

    // Access token expired — need to refresh
    if (!token.refresh_token || isExpired(token.refresh_token_expires_at)) {
        endSession();
        throw sessionExpired();
    }

    // If a refresh is already in progress, wait for it
    if (refreshPromise) {
        try {
            await refreshPromise;
        } catch (err) {
            // The starter already decided the session's fate. A transient
            // failure has to reach the caller as itself, or every waiter
            // reports a session that ended when it did not.
            if (!refusedRefresh(err)) throw err;
            throw sessionExpired();
        }
        const updated = getToken();
        if (updated && updated.access_token && !isExpired(updated.access_token_expires_at)) {
            return updated;
        }
        throw sessionExpired();
    }

    // Start a new refresh
    refreshPromise = refreshTokenFn(token.refresh_token);
    try {
        const newToken = await refreshPromise;
        setToken(newToken);
        return newToken;
    } catch (err) {
        if (!refusedRefresh(err)) throw err;
        endSession();
        throw sessionExpired();
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
        // Cancelled on purpose: no retry, no reauth prompt, no session change.
        if (isRequestCancelled(error)) throw error;
        const appErr = error as AppError;

        // Only an authorized 401 means the session is gone: refresh, retry once,
        // then give up. A public 401 reaches the caller with its code intact.
        if (config.authorization && (appErr?.status === 401 || appErr?.redirect)) {
            try {
                const token = await ensureValidToken();
                config.headers = {
                    ...config.headers,
                    Authorization: `Bearer ${token.access_token}`,
                }
                const res = await Client.request(config)
                return reviveDates(res.data)
            } catch (retryErr) {
                // ensureValidToken has already ended the session when it had
                // to, so its own AuthError travels untouched. Anything else
                // that is not a refusal is the API failing, not the session
                // ending, and must not sign the person out.
                if (retryErr instanceof AuthError) throw retryErr;
                if (!refusedRefresh(retryErr)) throw retryErr;
                endSession();
                throw sessionExpired();
            }
        }

        // A change that needs a fresher proof of identity: prompt, then retry
        // once. Checked before the generic 403 branch below so it gets its own
        // dialog rather than the permission-denied one.
        if (
            appErr?.code === "reauth_required" &&
            config.authorization &&
            !config.skipReauthPrompt &&
            typeof window !== "undefined"
        ) {
            await promptForReauth();
            const res = await Client.request(config);
            return reviveDates(res.data);
        }

        // A denied WRITE action (edit/save/delete) gets one clear, app-wide
        // popup explaining the missing permission (or plan). Reads that 403 are
        // intentionally left to page-level gating (locked surfaces / NoAccess),
        // so we only surface this for mutating methods.
        // A full mailbox allowance is a 403 with its own code and its own
        // dialog (MailboxAllowanceDialog), so it is not a permission problem.
        if (appErr?.status === 403 && appErr.code !== "mailbox_allowance_reached" && typeof window !== "undefined") {
            const method = String(config.method ?? "get").toUpperCase();
            if (method !== "GET" && method !== "HEAD") {
                window.dispatchEvent(
                    new CustomEvent("permission-denied", {
                        detail: { message: appErr.message, code: appErr.code },
                    }),
                );
            }
        }
        throw error;
    }
}
