// useTypewriter — animates AI text "arriving" instead of popping in at once.
// Eased character reveal over a duration scaled to the text length (capped so
// long drafts never feel slow). Honors prefers-reduced-motion by applying the
// full text immediately.

import React from "react";

function easeOutCubic(t: number): number {
    return 1 - Math.pow(1 - t, 3);
}

export function prefersReducedMotion(): boolean {
    return (
        typeof window !== "undefined" &&
        window.matchMedia("(prefers-reduced-motion: reduce)").matches
    );
}

export default function useTypewriter() {
    const frame = React.useRef<number | null>(null);
    const [running, setRunning] = React.useState(false);

    const cancel = React.useCallback(() => {
        if (frame.current !== null) {
            cancelAnimationFrame(frame.current);
            frame.current = null;
        }
        setRunning(false);
    }, []);

    // Reveals `text` progressively through apply(partial); apply always
    // receives a prefix of the final text, so the caller composes it into the
    // surrounding value however it likes.
    const run = React.useCallback(
        (text: string, apply: (partial: string) => void, onDone?: () => void) => {
            cancel();
            if (!text || prefersReducedMotion()) {
                apply(text);
                onDone?.();
                return;
            }
            setRunning(true);
            const duration = Math.min(1300, Math.max(420, text.length * 5));
            const start = performance.now();
            const tick = (now: number) => {
                const t = Math.min(1, (now - start) / duration);
                apply(text.slice(0, Math.round(easeOutCubic(t) * text.length)));
                if (t < 1) {
                    frame.current = requestAnimationFrame(tick);
                } else {
                    frame.current = null;
                    setRunning(false);
                    onDone?.();
                }
            };
            frame.current = requestAnimationFrame(tick);
        },
        [cancel],
    );

    React.useEffect(() => cancel, [cancel]);

    return { run, cancel, running };
}
