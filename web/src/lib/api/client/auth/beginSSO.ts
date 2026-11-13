import type { Acquisition } from "@/lib/acquisition";
import Request from "../Request";

/** Where the binding secret waits while the browser is away at the provider. */
export const SSO_BINDING_KEY = "sso_binding";

/**
 * Starts a browser sign-in with one provider: "oidc", "google" or "apple".
 *
 * The backend returns the authorization URL rather than redirecting, because
 * the dashboard is a single-page app on a different origin from the API. State,
 * nonce and the PKCE verifier are minted and held server-side; the binding is
 * the one value the client keeps, and the session cannot be collected without
 * it, so a forwarded callback link cannot sign anyone in.
 *
 * acquisition is applied only if this sign-in creates a new workspace.
 */
export default async function beginSSO(provider: string, acquisition?: Acquisition): Promise<{ url: string }> {
    const res = await Request<{ url: string; binding: string }>({
        method: "POST",
        url: `/auth/${provider}/begin`,
        data: acquisition ? { acquisition } : undefined,
    });

    // sessionStorage, not localStorage: it belongs to this tab and this sign-in
    // attempt, and it survives the round trip to the provider and back.
    try {
        sessionStorage.setItem(SSO_BINDING_KEY, res.binding);
    } catch {
        // Storage can be unavailable (private mode, embedded webview). The
        // exchange then fails closed and says which browser to finish in,
        // which beats signing the wrong person in.
    }

    return { url: res.url };
}

/** Reads the binding for this sign-in attempt and spends it. */
export function takeSSOBinding(): string {
    try {
        const value = sessionStorage.getItem(SSO_BINDING_KEY) ?? "";
        sessionStorage.removeItem(SSO_BINDING_KEY);
        return value;
    } catch {
        return "";
    }
}
