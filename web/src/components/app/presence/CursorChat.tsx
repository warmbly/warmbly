// CursorChat — press "/" on a collaborative surface to type a short message
// that floats next to your live cursor; teammates on the same resource see it
// attached to your pointer in real time. The text rides the existing cursor
// frames (see useLiveCursors.setChat), so there is no extra transport.
//
// The whole widget is pointer-events-none: it is opened and driven entirely by
// the keyboard, so the pointer keeps interacting with (and streaming from) the
// surface underneath while you type. Enter sends (the message lingers a few
// seconds, then fades for everyone); Escape or clicking anywhere closes it.

import React from "react";
import { createPortal } from "react-dom";

const SENT_LINGER_MS = 3500;

function isTypingTarget(t: EventTarget | null): boolean {
    const el = t as HTMLElement | null;
    if (!el) return false;
    return el.tagName === "INPUT" || el.tagName === "TEXTAREA" || el.isContentEditable;
}

export default function CursorChat({
    active,
    color,
    setChat,
}: {
    /** Whether the surface is collaborating (a teammate is here). */
    active: boolean;
    /** The local user's cursor color, so the bubble matches what teammates see. */
    color: string;
    /** From useLiveCursors: attaches/clears chat text on outgoing frames. */
    setChat: (text: string | null) => void;
}) {
    const [open, setOpen] = React.useState(false);
    const [text, setText] = React.useState("");
    const [sent, setSent] = React.useState<string | null>(null);
    const [pos, setPos] = React.useState<{ x: number; y: number } | null>(null);

    const posRef = React.useRef<{ x: number; y: number } | null>(null);
    const openRef = React.useRef(open);
    openRef.current = open;
    const setChatRef = React.useRef(setChat);
    setChatRef.current = setChat;
    const lingerTimer = React.useRef<number | null>(null);
    const inputRef = React.useRef<HTMLInputElement>(null);

    // Track the pointer in screen space; re-render with it only while open.
    React.useEffect(() => {
        let raf = 0;
        const onMove = (e: PointerEvent) => {
            posRef.current = { x: e.clientX, y: e.clientY };
            if (!openRef.current || raf) return;
            raf = requestAnimationFrame(() => {
                raf = 0;
                setPos(posRef.current);
            });
        };
        window.addEventListener("pointermove", onMove, { passive: true });
        return () => {
            window.removeEventListener("pointermove", onMove);
            if (raf) cancelAnimationFrame(raf);
        };
    }, []);

    const close = React.useCallback(() => {
        setOpen(false);
        setText("");
        setSent(null);
        setChatRef.current(null);
        if (lingerTimer.current != null) {
            clearTimeout(lingerTimer.current);
            lingerTimer.current = null;
        }
    }, []);

    // "/" opens the chat while the surface is live; it goes away with the peers.
    React.useEffect(() => {
        if (!active) {
            if (openRef.current) close();
            return;
        }
        const onKey = (e: KeyboardEvent) => {
            if (e.key !== "/" || e.metaKey || e.ctrlKey || e.altKey || e.isComposing) return;
            if (openRef.current || isTypingTarget(e.target)) return;
            e.preventDefault();
            setPos(posRef.current ?? { x: window.innerWidth / 2, y: window.innerHeight / 2 });
            setOpen(true);
        };
        window.addEventListener("keydown", onKey);
        return () => window.removeEventListener("keydown", onKey);
    }, [active, close]);

    React.useEffect(() => {
        if (open) inputRef.current?.focus();
    }, [open]);

    React.useEffect(() => () => close(), [close]);

    if (!open || !pos) return null;

    return createPortal(
        <div className="pointer-events-none fixed z-50" style={{ left: pos.x, top: pos.y }}>
            <div
                className="absolute left-3 top-4 w-max rounded-xl rounded-tl-sm px-2.5 py-1.5 shadow-md"
                style={{ backgroundColor: color }}
            >
                {sent ? (
                    <div className="max-w-[220px] break-words pb-1 text-[11.5px] leading-snug text-white/85">
                        {sent}
                    </div>
                ) : null}
                <input
                    ref={inputRef}
                    value={text}
                    maxLength={120}
                    placeholder="Say something…"
                    aria-label="Cursor chat"
                    onChange={(e) => {
                        const v = e.target.value;
                        setText(v);
                        setChat(v || null);
                        if (lingerTimer.current != null) {
                            clearTimeout(lingerTimer.current);
                            lingerTimer.current = null;
                        }
                        if (v) setSent(null);
                    }}
                    onKeyDown={(e) => {
                        // Mid-IME-composition Enter/Escape belong to the IME.
                        if (e.nativeEvent.isComposing) return;
                        if (e.key === "Escape") {
                            e.stopPropagation();
                            close();
                        } else if (e.key === "Enter" && text.trim()) {
                            e.stopPropagation();
                            // Send: the message stays on teammates' screens for a
                            // moment (it is already on the wire), then fades.
                            setSent(text);
                            setText("");
                            if (lingerTimer.current != null) clearTimeout(lingerTimer.current);
                            lingerTimer.current = window.setTimeout(() => {
                                lingerTimer.current = null;
                                setSent(null);
                                setChatRef.current(null);
                            }, SENT_LINGER_MS);
                        }
                    }}
                    onBlur={() => {
                        // An incidental blur (clicking through to the page)
                        // right after Enter must not cut the sent message
                        // short: close the input but let the linger timer
                        // clear the bubble for teammates on its own schedule.
                        if (lingerTimer.current != null && !text.trim()) {
                            setOpen(false);
                            setText("");
                            setSent(null);
                        } else {
                            close();
                        }
                    }}
                    className="w-[180px] bg-transparent text-[11.5px] leading-snug text-white outline-none placeholder:text-white/60"
                />
            </div>
        </div>,
        document.body,
    );
}
