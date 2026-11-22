// Shared motion for the buy flow.

export const PRESS_SPRING = { type: "spring", stiffness: 520, damping: 30, mass: 0.6 } as const;

/** The ease every checkout transition shares. */
export const EASE = [0.22, 1, 0.36, 1] as const;

/** A domain card springing into the order and out of it. */
export const CARD_SPRING = { type: "spring", stiffness: 420, damping: 34, mass: 0.8 } as const;

/** Hover "lift" drawn with a shadow: moving the button would slide it out from under a cursor resting on its edge and make it jitter. */
export const HOVER_LIFT = { boxShadow: "0 6px 14px -8px rgba(15,23,42,0.35)" } as const;
export const REST_SHADOW = { boxShadow: "0 0 0 0 rgba(15,23,42,0)" } as const;
