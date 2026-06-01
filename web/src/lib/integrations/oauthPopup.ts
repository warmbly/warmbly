// Drives the OAuth connect popup for third-party integrations. Mirrors the
// mailbox-onboarding flow: we open the provider authorization URL in a centered
// popup; the backend's /integrations/oauth/callback page postMessages the
// {code, state} back to this opener; we resolve with them so the caller can
// finish the handshake. The window-name carries no secret — the CSRF/PKCE
// state lives server-side, keyed by the `state` nonce.

export interface OAuthPopupResult {
    code: string;
    state: string;
}

const POPUP_MESSAGE_SOURCE = "warmbly-integration-oauth";

export function openOAuthPopup(authUrl: string): Promise<OAuthPopupResult> {
    return new Promise((resolve, reject) => {
        const width = 600;
        const height = 720;
        const left = window.screenX + Math.max(0, (window.outerWidth - width) / 2);
        const top = window.screenY + Math.max(0, (window.outerHeight - height) / 2);
        const popup = window.open(
            authUrl,
            "warmbly_oauth",
            `width=${width},height=${height},left=${left},top=${top},menubar=no,toolbar=no,location=yes`,
        );
        if (!popup) {
            reject(new Error("Popup blocked. Allow popups for this site and try again."));
            return;
        }

        let settled = false;
        const cleanup = () => {
            window.removeEventListener("message", onMessage);
            window.clearInterval(closedTimer);
        };

        const onMessage = (event: MessageEvent) => {
            const data = event.data as
                | { source?: string; code?: string; state?: string; error?: string }
                | undefined;
            if (!data || data.source !== POPUP_MESSAGE_SOURCE) return;
            settled = true;
            cleanup();
            try {
                popup.close();
            } catch {
                /* ignore */
            }
            if (data.error) {
                reject(new Error(data.error));
                return;
            }
            if (data.code && data.state) {
                resolve({ code: data.code, state: data.state });
                return;
            }
            reject(new Error("Authorization was cancelled."));
        };

        window.addEventListener("message", onMessage);

        // Detect a manually-closed popup so the caller's promise doesn't hang.
        const closedTimer = window.setInterval(() => {
            if (popup.closed && !settled) {
                cleanup();
                reject(new Error("Authorization window was closed before finishing."));
            }
        }, 600);
    });
}
