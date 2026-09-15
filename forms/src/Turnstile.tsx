// Cloudflare Turnstile widget, rendered explicitly so React owns the mount
// point. resetTurnstile() (turnstileScript.ts) clears the used token after a
// rejected submit.

import { useEffect, useRef } from "react";

import { loadTurnstileScript } from "./turnstileScript";

export function Turnstile({ siteKey, onToken }: { siteKey: string; onToken: (token: string) => void }) {
    const el = useRef<HTMLDivElement>(null);
    const onTokenRef = useRef(onToken);
    onTokenRef.current = onToken;

    useEffect(() => {
        let widgetId: string | null = null;
        let cancelled = false;
        loadTurnstileScript()
            .then(() => {
                if (cancelled || !el.current || !window.turnstile) return;
                widgetId = window.turnstile.render(el.current, {
                    sitekey: siteKey,
                    callback: (token: string) => onTokenRef.current(token),
                    "expired-callback": () => onTokenRef.current(""),
                    "error-callback": () => onTokenRef.current(""),
                });
            })
            .catch(() => {
                // widget stays absent; the backend rejects with a clear message
            });
        return () => {
            cancelled = true;
            if (widgetId) {
                try {
                    window.turnstile?.remove(widgetId);
                } catch {
                    // already gone
                }
            }
        };
    }, [siteKey]);

    return (
        <div className="fld">
            <div ref={el} className="cf-turnstile" />
        </div>
    );
}
