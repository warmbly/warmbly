import { useEffect, useRef, useCallback } from "react";
import Turnstile from "react-turnstile";

interface Props {
    visible: boolean;
    onToken: (t: string) => void;
}

export function TurnstileModal({ visible, onToken }: Props) {
    const defaultDevBypassToken = "warmbly-local-turnstile-bypass";
    const bypassToken = import.meta.env.DEV
        ? (import.meta.env.VITE_TURNSTILE_BYPASS_TOKEN?.trim() || defaultDevBypassToken)
        : "";
    const tokenRef = useRef("");
    const waitingRef = useRef(false);
    const turnstileRef = useRef<{ reset(): void } | null>(null);
    const onTokenRef = useRef(onToken);
    onTokenRef.current = onToken;

    const deliver = useCallback((token: string) => {
        onTokenRef.current(token);
        setTimeout(() => turnstileRef.current?.reset(), 50);
    }, []);

    const handleVerify = useCallback((token: string) => {
        if (waitingRef.current) {
            waitingRef.current = false;
            deliver(token);
        } else {
            tokenRef.current = token;
        }
    }, [deliver]);

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
                waitingRef.current = true;
            }
        } else {
            waitingRef.current = false;
        }
    }, [visible, bypassToken, deliver]);

    if (bypassToken) return null;

    // The Turnstile component's typings don't surface `ref` publicly,
    // but the underlying widget does support it. Cast the props bag
    // to allow it without disabling TS for the whole call.
    const turnstileProps = {
        ref: turnstileRef,
        sitekey: import.meta.env.VITE_TURNSTILE_KEY!,
        onVerify: handleVerify,
        onExpire: () => { tokenRef.current = ""; turnstileRef.current?.reset(); },
        size: "invisible" as const,
    };
    return <Turnstile {...(turnstileProps as unknown as React.ComponentProps<typeof Turnstile>)} />;
}
