// How a conversation row enters and leaves the list.
//
// The cache edit that removes a row cannot tell the row why it is going, and
// the row's exit animation is resolved after it has already left the data. So
// the action records its intent here first, keyed by thread, and the exit
// variant reads it back at exit time. The row's node also gets `data-exit`, so
// the underlay's colour and label come from CSS: the exiting element keeps the
// markup of its last render, which predates the intent.

import type { TargetAndTransition, Variants } from "framer-motion";

export type RowExitKind = "archive" | "trash" | "inbox" | "snooze";

interface ExitIntent {
    kind: RowExitKind;
    /** Position among the rows leaving that are on screen, for the stagger. */
    rank: number;
    /** Off-screen rows leave at once; animating them only costs frames. */
    visible: boolean;
    at: number;
}

const intents = new Map<string, ExitIntent>();
// Only has to outlive the gap between the action and the cache edit; short so
// a row brought back by Undo does not inherit its old exit.
const INTENT_TTL_MS = 2_000;

const EASE_OUT = [0.16, 1, 0.3, 1] as const;
const EASE_IN = [0.4, 0, 1, 1] as const;
const STAGGER_S = 0.022;
const STAGGER_CAP = 14;

function rowElement(id: string): HTMLElement | null {
    if (typeof document === "undefined") return null;
    const sel =
        typeof CSS !== "undefined" && CSS.escape
            ? CSS.escape(id)
            : id.replace(/["\\]/g, "\\$&");
    return document.querySelector<HTMLElement>(`[data-thread-id="${sel}"]`);
}

/** Record why these rows are about to leave. Call before the cache edit. */
export function markRowExit(threadIds: string[], kind: RowExitKind): void {
    const now = Date.now();
    for (const [id, intent] of intents) {
        if (now - intent.at > INTENT_TTL_MS) intents.delete(id);
    }
    const viewport =
        typeof window === "undefined" ? 0 : window.innerHeight || 0;
    let rank = 0;
    // Reads only, no writes in between, so this is one layout pass.
    const found = threadIds.map((id) => ({ id, el: rowElement(id) }));
    for (const { id, el } of found) {
        const rect = el?.getBoundingClientRect();
        const visible = !!rect && rect.bottom > 0 && rect.top < viewport;
        intents.set(id, { kind, rank: visible ? rank++ : 0, visible, at: now });
    }
    for (const { el } of found) el?.setAttribute("data-exit", kind);
}

function readIntent(id: string): ExitIntent | undefined {
    const intent = intents.get(id);
    if (!intent) return undefined;
    if (Date.now() - intent.at > INTENT_TTL_MS) {
        intents.delete(id);
        return undefined;
    }
    return intent;
}

export interface RowMotionCustom {
    id: string;
    /** Inserted above rows already shown (a new arrival, an undo). */
    arrival: boolean;
    /** Position among the rows entering together, for the stagger. */
    rank: number;
}

// The slide each action leaves in: archive right, anything going back or to
// Trash left, a snooze lifts away.
const SLIDE: Record<RowExitKind, TargetAndTransition> = {
    archive: { x: 56 },
    trash: { x: -56 },
    inbox: { x: -56 },
    snooze: { y: -6, scale: 0.985 },
};

function exitDelay(intent: ExitIntent): number {
    return Math.min(intent.rank, STAGGER_CAP) * STAGGER_S;
}

/** The row's outer box: grows in on arrival, folds shut on the way out. */
export const rowShell: Variants = {
    enter: (c: RowMotionCustom) =>
        c.arrival ? { opacity: 0, height: 0 } : { opacity: 0, y: 6 },
    shown: (c: RowMotionCustom) =>
        c.arrival
            ? {
                  opacity: 1,
                  height: "auto",
                  transition: {
                      height: { duration: 0.24, ease: EASE_OUT },
                      opacity: { duration: 0.2, delay: 0.06 },
                  },
              }
            : {
                  opacity: 1,
                  y: 0,
                  transition: {
                      // Rows past the first screenful appear as they are.
                      duration: c.rank > 16 ? 0 : 0.26,
                      ease: EASE_OUT,
                      delay: Math.min(c.rank, 12) * 0.018,
                  },
              },
    exit: (c: RowMotionCustom) => {
        const intent = readIntent(c.id);
        if (!intent) {
            // Filed elsewhere (a teammate, a refetch): fade and fold.
            return {
                opacity: 0,
                height: 0,
                transition: {
                    opacity: { duration: 0.15 },
                    height: { duration: 0.22, ease: EASE_OUT, delay: 0.05 },
                },
            };
        }
        if (!intent.visible) {
            return { opacity: 0, height: 0, transition: { duration: 0 } };
        }
        const d = exitDelay(intent);
        return {
            height: 0,
            opacity: 0,
            transition: {
                height: { duration: 0.24, ease: EASE_OUT, delay: d + 0.2 },
                opacity: { duration: 0.2, delay: d + 0.24 },
            },
        };
    },
};

/** The row's content: slides out in the direction of the action. */
export const rowContent: Variants = {
    enter: {},
    shown: { x: 0, y: 0, scale: 1, opacity: 1 },
    exit: (c: RowMotionCustom) => {
        const intent = readIntent(c.id);
        if (!intent) return {};
        if (!intent.visible) return { opacity: 0, transition: { duration: 0 } };
        const d = exitDelay(intent);
        return {
            ...SLIDE[intent.kind],
            opacity: 0,
            transition: {
                default: { duration: 0.22, ease: EASE_IN, delay: d },
                opacity: { duration: 0.2, delay: d + 0.06 },
            },
        };
    },
};

/** What the slide uncovers: a tint and the action's name. */
export const rowUnderlay: Variants = {
    enter: { opacity: 0 },
    shown: { opacity: 0 },
    exit: (c: RowMotionCustom) => {
        const intent = readIntent(c.id);
        if (!intent || !intent.visible) return { opacity: 0, transition: { duration: 0 } };
        return {
            opacity: 1,
            transition: { duration: 0.12, delay: exitDelay(intent) },
        };
    },
};

