import React, { useEffect, useRef } from "react";
import { motion, AnimatePresence } from "motion/react";
import { RiAppleFill } from "@remixicon/react";
import { KeyRound, Loader2Icon } from "lucide-react";
import { Google } from "../svg";
import { API_URL, PopupCenter } from "@/lib/information";
import { AUTH_CELL as CELL } from "./styles";

// Snappy-but-smooth spring used for the layout reflow when the passkey cell
// enters/leaves (sign-in ⇄ create-account) and the others resize to fill.
const spring = { type: "spring" as const, stiffness: 520, damping: 42, mass: 0.85 };

// Crossfade for the passkey glyph ⇄ spinner. Both sit absolutely stacked in a
// fixed 16px slot, so one dissolves into the other in place — the "Passkey"
// label never shifts. Soft ease-out + a hair of scale reads as a morph, not a
// hard swap.
const ICON_SWAP = {
    initial: { opacity: 0, scale: 0.55 },
    animate: { opacity: 1, scale: 1 },
    exit: { opacity: 0, scale: 0.55 },
    transition: { duration: 0.2, ease: [0.22, 1, 0.36, 1] as const },
};

// Inner content is layout="position" so it only translates (stays centered)
// while its button animates width — the icon + label never stretch.
function CellBody({ children }: { children: React.ReactNode }) {
    return (
        <motion.span layout="position" className="inline-flex items-center gap-2 whitespace-nowrap">
            {children}
        </motion.span>
    );
}

// Alternative sign-in row. When `passkey` is present (sign-in) it's a third,
// equal-width cell; on create-account it animates out and Google/Apple glide
// wider to fill — one fluid layout transition, no snap.
export default function ExternalLogin({
    passkey,
}: {
    passkey?: { onClick: () => void; onPrepare: () => void; loading: boolean; disabled?: boolean; label?: string };
}) {
    const passkeyRef = useRef<HTMLButtonElement | null>(null);

    useEffect(() => {
        const button = passkeyRef.current;
        if (!button || !passkey) return;

        // WebAuthn is strict about user activation in Safari. Register this as
        // a native click listener so the ceremony stays inside the browser's
        // trusted gesture path instead of React's delegated event wrapper.
        const handleClick = (event: MouseEvent) => {
            event.preventDefault();
            passkey.onClick();
        };

        button.addEventListener("click", handleClick);
        return () => button.removeEventListener("click", handleClick);
    }, [passkey]);

    return (
        <motion.div layout className="flex gap-2.5">
            <AnimatePresence mode="popLayout" initial={false}>
                {passkey && (
                    <motion.button
                        ref={passkeyRef}
                        layout
                        key="passkey"
                        type="button"
                        onPointerEnter={() => passkey.onPrepare()}
                        onPointerDown={() => passkey.onPrepare()}
                        onFocus={() => passkey.onPrepare()}
                        disabled={passkey.disabled}
                        aria-label="Sign in with a passkey"
                        aria-busy={passkey.loading}
                        initial={{ opacity: 0, scale: 0.85 }}
                        animate={{ opacity: 1, scale: 1 }}
                        exit={{ opacity: 0, scale: 0.85 }}
                        transition={{ ...spring, opacity: { duration: 0.15 } }}
                        // Stay full-opacity while busy: the spinner signals work,
                        // not the greyed-out disabled look.
                        className={`${CELL} flex-1 min-w-0 ${passkey.loading ? "!opacity-100" : ""}`}
                    >
                        <CellBody>
                            <span className="relative inline-flex h-4 w-4 shrink-0 items-center justify-center">
                                <AnimatePresence initial={false}>
                                    {passkey.loading ? (
                                        <motion.span
                                            key="spinner"
                                            {...ICON_SWAP}
                                            className="absolute inset-0 inline-flex items-center justify-center"
                                        >
                                            <Loader2Icon className="h-4 w-4 animate-spin text-sky-500" />
                                        </motion.span>
                                    ) : (
                                        <motion.span
                                            key="key"
                                            {...ICON_SWAP}
                                            className="absolute inset-0 inline-flex items-center justify-center"
                                        >
                                            <KeyRound className="h-4 w-4 text-sky-500" />
                                        </motion.span>
                                    )}
                                </AnimatePresence>
                            </span>
                            {passkey.label ?? "Passkey"}
                        </CellBody>
                    </motion.button>
                )}
            </AnimatePresence>

            <motion.button
                layout
                key="google"
                type="button"
                transition={spring}
                onClick={() => PopupCenter(`${API_URL}/auth/google/login`, "Google Login")}
                className={`${CELL} flex-1 min-w-0`}
            >
                <CellBody>
                    <Google className="w-4 shrink-0" />
                    Google
                </CellBody>
            </motion.button>

            <motion.button
                layout
                key="apple"
                type="button"
                transition={spring}
                onClick={() => PopupCenter(`${API_URL}/auth/apple/login`, "Apple Login")}
                className={`${CELL} flex-1 min-w-0`}
            >
                <CellBody>
                    <RiAppleFill className="size-4 shrink-0" />
                    Apple
                </CellBody>
            </motion.button>
        </motion.div>
    );
}
