import { useEffect, useMemo, useRef } from "react";
import type { BoundTurnstileObject } from "react-turnstile";
import { API_BASE_URL } from "@/lib/information";
import { captchaFrameURL, currentPortal, mainHost, readCaptchaMessage } from "@/lib/portal";

interface Props {
    onLoad?: (bound: BoundTurnstileObject) => void;
    onVerify: (token: string, bound: BoundTurnstileObject) => void;
    onExpire?: () => void;
    onError?: (error?: unknown, bound?: BoundTurnstileObject) => void;
    /** The challenge is always on screen here, so a caller's no-answer timeout should stop. */
    onBeforeInteractive?: () => void;
}

function apiOrigin(): string {
    try {
        return new URL(API_BASE_URL, window.location.href).origin;
    } catch {
        return "";
    }
}

// On a portal domain the Turnstile widget cannot render (Cloudflare lists only the main hosts), so the
// API origin renders it in a frame and posts the token back to this page.
export default function CaptchaFrame({ onLoad, onVerify, onExpire, onError, onBeforeInteractive }: Props) {
    const frameRef = useRef<HTMLIFrameElement>(null);
    const tokenRef = useRef("");
    const handlers = useRef({ onLoad, onVerify, onExpire, onError, onBeforeInteractive });
    handlers.current = { onLoad, onVerify, onExpire, onError, onBeforeInteractive };
    const origin = useMemo(apiOrigin, []);

    const bound = useMemo<BoundTurnstileObject>(() => ({
        // The frame runs its challenge on load; there is nothing to start.
        execute: () => handlers.current.onBeforeInteractive?.(),
        reset: () => {
            tokenRef.current = "";
            frameRef.current?.contentWindow?.postMessage({ type: "warmbly:captcha-reset" }, origin);
        },
        getResponse: () => undefined,
        isExpired: () => !tokenRef.current,
    }), [origin]);

    useEffect(() => {
        handlers.current.onLoad?.(bound);
        const listener = (event: MessageEvent) => {
            const msg = readCaptchaMessage(event, origin, frameRef.current?.contentWindow);
            if (!msg) return;
            tokenRef.current = msg.token;
            if (msg.token) handlers.current.onVerify(msg.token, bound);
            else if (msg.error) handlers.current.onError?.(undefined, bound);
            else handlers.current.onExpire?.();
        };
        window.addEventListener("message", listener);
        return () => window.removeEventListener("message", listener);
    }, [bound, origin]);

    return (
        <div className="flex justify-center">
            <iframe
                ref={frameRef}
                src={captchaFrameURL(API_BASE_URL, window.location.origin)}
                title="Security check"
                width={300}
                height={65}
                className="border-0"
                style={{ colorScheme: "light" }}
            />
        </div>
    );
}

/** Shown on a portal whose API cannot render the bridge: the check only runs on the main address. */
export function CaptchaUnavailable() {
    const portal = currentPortal();
    const url = portal.portal ? portal.main_url : "";
    return (
        <p className="rounded-md border border-amber-200 bg-amber-50 px-3 py-2 text-[12.5px] leading-relaxed text-amber-800">
            The security check cannot run on this address. Sign in at{" "}
            {url ? <a href={url} className="font-medium underline">{mainHost(portal)}</a> : mainHost(portal)} instead.
        </p>
    );
}

