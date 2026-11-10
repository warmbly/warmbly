// Signing in to an agency portal from the main address: the main address signs the
// person in, asks for a one-time code for the portal host, and sends the browser there.

import { useQuery } from "@tanstack/react-query";

import { createPortalHandoff } from "@/lib/api/client/auth/portalHandoff";
import Request from "@/lib/api/client/Request";
import { API_BASE_URL } from "@/lib/information";
import { parsePortalInfo, safeHandoffRedirect, type Portal } from "@/lib/portal";

const HANDOFF_HOST_KEY = "portal_handoff_host";

/** Keeps the portal host across a provider round trip, for this tab only. */
export function rememberPortalHandoff(host: string): void {
    try {
        if (host) sessionStorage.setItem(HANDOFF_HOST_KEY, host);
        else sessionStorage.removeItem(HANDOFF_HOST_KEY);
    } catch {
        /* storage blocked: the sign-in lands on the main dashboard instead */
    }
}

export function takePortalHandoff(): string {
    try {
        const host = sessionStorage.getItem(HANDOFF_HOST_KEY) ?? "";
        sessionStorage.removeItem(HANDOFF_HOST_KEY);
        return host;
    } catch {
        return "";
    }
}

/** Sends the browser to the portal with a code; with a token, ends the session that was signed in only for this. */
export async function goToPortal(host: string, accessToken?: string): Promise<void> {
    try {
        const out = await createPortalHandoff(host, { accessToken, endSession: !!accessToken });
        const target = safeHandoffRedirect(out.redirect_url, host);
        if (!target) throw new Error("The portal address did not match.");
        window.location.replace(target);
    } catch (err) {
        if (accessToken) {
            await Request<void>({ method: "POST", url: "/auth/logout", headers: { Authorization: `Bearer ${accessToken}` } }).catch(() => undefined);
        }
        throw err;
    }
}

/** The agency portal at host when it signs its clients in on this address; null otherwise. */
export function usePortalForSignIn(host: string) {
    return useQuery({
        queryKey: ["portal-sign-in", host],
        enabled: !!host,
        staleTime: 60_000,
        queryFn: async (): Promise<Portal | null> => {
            const res = await fetch(`${API_BASE_URL}/auth/portal?host=${encodeURIComponent(host)}`, { credentials: "omit" });
            if (!res.ok) throw new Error("The portal could not be looked up.");
            const info = parsePortalInfo(await res.json());
            return info.portal && info.sign_in === "warmbly" ? info : null;
        },
    });
}
