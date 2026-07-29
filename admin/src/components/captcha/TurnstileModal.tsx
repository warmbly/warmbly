// Cloudflare Turnstile, mirroring the dashboard's component so the admin
// sign-in satisfies the same /auth/login captcha gate.
//
// In dev it short-circuits to the bypass token (no widget, no network) that
// the backend's TURNSTILE_BYPASS_TOKEN accepts; in prod it renders the
// invisible Turnstile widget and delivers a real token. Either way the parent
// gets a token via onToken and sends it as `turnstile` on login.

import { useCallback, useEffect, useRef, type ComponentProps } from "react";
import Turnstile, { type BoundTurnstileObject } from "react-turnstile";
import { TURNSTILE_KEY } from "@/lib/env";

interface Props {
    visible: boolean;
    onToken: (token: string) => void;
    onError?: (message?: string) => void;
}

export function TurnstileModal({ visible, onToken, onError }: Props) {
    const defaultDevBypassToken = "warmbly-local-turnstile-bypass";
    const bypassToken = import.meta.env.DEV
        ? import.meta.env.VITE_TURNSTILE_BYPASS_TOKEN?.trim() || defaultDevBypassToken
        : "";

    const tokenRef = useRef("");
    const waitingRef = useRef(false);
    const timeoutRef = useRef<ReturnType<typeof setTimeout> | null>(null);
    const turnstileRef = useRef<BoundTurnstileObject | null>(null);
    const onTokenRef = useRef(onToken);
    const onErrorRef = useRef(onError);
    onTokenRef.current = onToken;
    onErrorRef.current = onError;

    const deliver = useCallback((token: string) => {
        if (timeoutRef.current) {
            clearTimeout(timeoutRef.current);
            timeoutRef.current = null;
        }
        waitingRef.current = false;
        onTokenRef.current(token);
        setTimeout(() => turnstileRef.current?.reset(), 50);
    }, []);

    const handleVerify = useCallback(
        (token: string, bound?: BoundTurnstileObject) => {
            if (bound) turnstileRef.current = bound;
            if (waitingRef.current) {
                deliver(token);
            } else {
                tokenRef.current = token;
            }
        },
        [deliver],
    );

    const fail = useCallback((message = "Verification failed. Please try again.") => {
        if (timeoutRef.current) {
            clearTimeout(timeoutRef.current);
            timeoutRef.current = null;
        }
        waitingRef.current = false;
        tokenRef.current = "";
        turnstileRef.current?.reset();
        onErrorRef.current?.(message);
    }, []);

    const execute = useCallback(() => {
        if (timeoutRef.current) {
            clearTimeout(timeoutRef.current);
            timeoutRef.current = null;
        }
        waitingRef.current = true;
        timeoutRef.current = setTimeout(() => {
            fail("Verification timed out. Please try again.");
        }, 10000);
        turnstileRef.current?.execute();
    }, [fail]);

    useEffect(() => {
        if (visible && bypassToken) {
            onTokenRef.current(bypassToken);
            return;
        }
        if (visible) {
            if (tokenRef.current) {
                const t = tokenRef.current;
                tokenRef.current = "";
                deliver(t);
            } else {
                execute();
            }
        } else {
            if (timeoutRef.current) {
                clearTimeout(timeoutRef.current);
                timeoutRef.current = null;
            }
            waitingRef.current = false;
        }
    }, [visible, bypassToken, deliver, execute]);

    if (bypassToken) return null;

    const turnstileProps = {
        ref: turnstileRef,
        sitekey: TURNSTILE_KEY,
        onVerify: handleVerify,
        onExpire: () => {
            tokenRef.current = "";
            turnstileRef.current?.reset();
        },
        onError: () => fail(),
        onTimeout: () => fail("Verification timed out. Please try again."),
        onAfterInteractive: (bound: BoundTurnstileObject) => {
            turnstileRef.current = bound;
            if (visible && waitingRef.current) bound.execute();
        },
        size: "invisible" as const,
    };
    return <Turnstile {...(turnstileProps as unknown as ComponentProps<typeof Turnstile>)} />;
}
