// Cloudflare Turnstile, mirroring the dashboard's component so the admin
// sign-in satisfies the same /auth/login captcha gate.
//
// In dev it short-circuits to the bypass token (no widget, no network) that
// the backend's TURNSTILE_BYPASS_TOKEN accepts; in prod it renders the
// invisible Turnstile widget and delivers a real token. Either way the parent
// gets a token via onToken and sends it as `turnstile` on login.
//
// `required` is the deployment's own answer, read from /v1/auth/config. With
// CAPTCHA_PROVIDER=none the backend verifies no token, and mounting the widget
// anyway meant an air-gapped or self-hosted instance could only sit on
// challenges.cloudflare.com until it timed out: the operator was locked out of
// their own admin panel. It defaults to true, so a config that could not be
// read keeps the widget rather than skipping the check.

import { useCallback, useEffect, useRef, type ComponentProps } from "react";
import Turnstile, { type BoundTurnstileObject } from "react-turnstile";
import { TURNSTILE_KEY } from "@/lib/env";

interface Props {
    visible: boolean;
    required?: boolean;
    onToken: (token: string) => void;
    onError?: (message?: string) => void;
}

export function TurnstileModal({ visible, required = true, onToken, onError }: Props) {
    const defaultDevBypassToken = "warmbly-local-turnstile-bypass";
    const devBypassToken = import.meta.env.DEV
        ? import.meta.env.VITE_TURNSTILE_BYPASS_TOKEN?.trim() || defaultDevBypassToken
        : "";
    // No widget, and the token the parent gets is whatever the backend will
    // accept: the dev bypass string, or "" when nothing is verified at all.
    const skipWidget = !required || devBypassToken !== "";
    const bypassToken = devBypassToken;

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
        if (visible && skipWidget) {
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
    }, [visible, skipWidget, bypassToken, deliver, execute]);

    if (skipWidget) return null;

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
