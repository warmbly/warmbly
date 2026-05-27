// Token persistence for the admin app.
//
// The backend's session model is Bearer-token based: /auth/login returns
// an access_token + refresh_token pair, and /auth/refresh swaps the
// refresh for a new pair. The dashboard uses the same shape and keeps
// the tokens in localStorage; we mirror that here so an admin doesn't
// have to learn a different login flow.
//
// Important: localStorage is per-origin, so if the admin app and the
// dashboard live on different origins (admin.warmbly.app vs
// app.warmbly.app), the admin will log in once on each. That's
// intentional — the admin surface should require a fresh, deliberate
// auth event, not silently inherit a dashboard session.

const TOKEN_KEY = "warmbly_admin_token";

export interface AdminToken {
    access_token: string;
    access_token_expires_at: string; // ISO
    refresh_token: string;
    refresh_token_expires_at: string; // ISO
}

export function getToken(): AdminToken | null {
    const raw = localStorage.getItem(TOKEN_KEY);
    if (!raw) return null;
    try {
        return JSON.parse(raw) as AdminToken;
    } catch {
        return null;
    }
}

export function setToken(t: AdminToken | null) {
    if (!t) {
        localStorage.removeItem(TOKEN_KEY);
        return;
    }
    localStorage.setItem(TOKEN_KEY, JSON.stringify(t));
}

export function clearToken() {
    localStorage.removeItem(TOKEN_KEY);
}

export function isExpired(iso: string): boolean {
    return new Date(iso).getTime() <= Date.now();
}
