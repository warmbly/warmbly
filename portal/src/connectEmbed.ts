// Embed and sign-in plumbing for the connect page. The message names are the
// contract with the embed loader that frames /connect/<token>?embed=1; nothing
// posted to the parent names the link, the workspace or a mailbox.

/** Posted to the parent with the page height on every size change. */
export const CONNECT_RESIZE_MESSAGE = "warmbly:connect:resize";
/** Posted to the parent after each mailbox connects, with the link's new count. */
export const CONNECT_CONNECTED_MESSAGE = "warmbly:connect:connected";

export function startConnectResizeReporting(): () => void {
    if (window.parent === window) return () => {};
    const post = () => {
        try {
            window.parent.postMessage({ type: CONNECT_RESIZE_MESSAGE, height: document.documentElement.scrollHeight }, "*");
        } catch {
            // a sandboxed parent can refuse; the frame keeps its height
        }
    };
    post();
    const ro = new ResizeObserver(post);
    ro.observe(document.body);
    return () => ro.disconnect();
}

export function postConnected(connected: number) {
    if (window.parent === window) return;
    try {
        window.parent.postMessage({ type: CONNECT_CONNECTED_MESSAGE, connected }, "*");
    } catch {
        // same as above
    }
}

export interface SigninResult {
    connected?: string;
    provider?: string;
    error?: string;
}

/** readSigninResult takes the outcome the backend put in the fragment, and clears it from the address bar. */
export function readSigninResult(): SigninResult | null {
    const hash = window.location.hash.replace(/^#/, "");
    if (!hash) return null;
    const p = new URLSearchParams(hash);
    const out: SigninResult = {
        connected: p.get("connected") ?? undefined,
        provider: p.get("provider") ?? undefined,
        error: p.get("error") ?? undefined,
    };
    if (!out.connected && !out.error) return null;
    history.replaceState(null, "", window.location.pathname + window.location.search);
    return out;
}

/** closeIfSigninWindow closes a sign-in window whose opener survived; the opener collects the outcome itself. */
export function closeIfSigninWindow(): boolean {
    try {
        if (!window.opener) return false;
    } catch {
        return false;
    }
    window.setTimeout(() => window.close(), 1200);
    return true;
}

const SIGNIN_WINDOW_NAME = "warmbly-connect-signin";

/** openSigninWindow opens the window before the URL is known, so the browser counts it as the click's. */
export function openSigninWindow(): Window | null {
    const w = 520;
    const h = 680;
    const left = Math.max(0, window.screenX + (window.outerWidth - w) / 2);
    const top = Math.max(0, window.screenY + (window.outerHeight - h) / 2);
    return window.open("about:blank", SIGNIN_WINDOW_NAME, `width=${w},height=${h},left=${left},top=${top}`);
}

/** navigateForSignin is the fallback when no window could open: leave the frame if there is one. */
export function navigateForSignin(url: string) {
    try {
        if (window.top && window.top !== window) {
            window.top.location.href = url;
            return;
        }
    } catch {
        // a cross-origin top without user activation refuses; use this frame
    }
    window.location.href = url;
}
