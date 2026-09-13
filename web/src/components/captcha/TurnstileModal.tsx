import { useEffect, useRef, useCallback } from "react";
import Turnstile, { type BoundTurnstileObject } from "react-turnstile";
import { TURNSTILE_KEY } from "@/lib/information";

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
    // The widget instance only ever arrives through onLoad. `Turnstile` is a
    // plain function component, not forwardRef, and its `userRef` prop is the
    // container div rather than the widget, so neither can reach reset() or
    // execute().
    const turnstileRef = useRef<BoundTurnstileObject | null>(null);
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
                // execution="execute" means nothing happens until asked. If
                // the widget has not loaded yet, onLoad below executes it.
                turnstileRef.current?.execute();
            }
        } else {
            waitingRef.current = false;
        }
    }, [visible, bypassToken, deliver]);

    if (bypassToken) return null;

    const turnstileProps = {
        sitekey: TURNSTILE_KEY,
        onLoad: (_widgetId: string, bound: BoundTurnstileObject) => {
            turnstileRef.current = bound;
            // The modal can open before the widget finishes loading, in which
            // case the effect above had nothing to execute.
            if (waitingRef.current) bound.execute();
        },
        onVerify: handleVerify,
        onExpire: () => { tokenRef.current = ""; turnstileRef.current?.reset(); },
        // Cloudflare removed the "invisible" size: it now answers
        // "expected compact, flexible, or normal" and the widget never
        // renders, so no token is ever produced. execution=execute defers the
        // challenge until .execute() is called and interaction-only keeps it
        // out of the layout unless a human actually has to do something.
        execution: "execute" as const,
        appearance: "interaction-only" as const,
    };
    return <Turnstile {...(turnstileProps as unknown as React.ComponentProps<typeof Turnstile>)} />;
}
