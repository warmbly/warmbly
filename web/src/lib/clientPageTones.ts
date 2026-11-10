// The colours a client page (a connect link, a shared report) takes from a brand's accent, for the
// Branding preview. Mirrors deriveTones in portal/src/theme.ts, which is what those pages run.

import { contrastRatio, isHexColour } from "./accentScale";
import type { ResolvedTheme } from "./theme";

const WHITE = "#ffffff";
const BLACK = "#000000";
const DARK_TEXT = "#0f172a";
const DEFAULT_ACCENT = "#0284c7";
const WHITE_TEXT_MIN = 3;
const TEXT_MIN = 4.5;
const GRAPHIC_MIN = 3;

export interface Surfaces {
    page: string;
    card: string;
    ink: string;
    muted: string;
    line: string;
}

export const CLIENT_SURFACES: Record<ResolvedTheme, Surfaces> = {
    light: { page: "#f6f7f9", card: "#ffffff", ink: "#0f172a", muted: "#64748b", line: "#e2e8f0" },
    dark: { page: "#0b0f17", card: "#131923", ink: "#e8ecf3", muted: "#94a0b3", line: "#253042" },
};

export interface ClientTones {
    /** Button fill, and the text written on it. */
    accent: string;
    onAccent: string;
    /** Accent as text. */
    link: string;
    /** Soft tint for selected surfaces. */
    soft: string;
    /** Chart marks and meters. */
    chart: string;
}

function channels(hex: string): [number, number, number] {
    const n = parseInt(hex.slice(1), 16);
    return [(n >> 16) & 255, (n >> 8) & 255, n & 255];
}

/** a with the given share (0 to 1) of b blended in. */
export function mixHex(a: string, b: string, share: number): string {
    const ca = channels(a);
    const cb = channels(b);
    const s = Math.min(1, Math.max(0, share));
    return "#" + ca.map((v, i) => Math.round(v + (cb[i] - v) * s).toString(16).padStart(2, "0")).join("");
}

// Walks from one colour toward another and stops at the first step that passes.
function toward(from: string, to: string, ok: (c: string) => boolean): string {
    const steps = 25;
    for (let i = 0; i < steps; i++) {
        const c = mixHex(from, to, i / steps);
        if (ok(c)) return c;
    }
    return to;
}

export function clientPageTones(accentColour: string | undefined, appearance: ResolvedTheme): ClientTones {
    const accent = isHexColour(accentColour) ? accentColour.toLowerCase() : DEFAULT_ACCENT;
    const s = CLIENT_SURFACES[appearance];
    const onAccent = contrastRatio(accent, WHITE) >= WHITE_TEXT_MIN ? WHITE : DARK_TEXT;
    const fill = onAccent === WHITE ? toward(accent, BLACK, (c) => contrastRatio(c, WHITE) >= TEXT_MIN) : accent;
    const soft = mixHex(s.card, accent, appearance === "dark" ? 0.16 : 0.08);
    const grounds = [s.page, s.card];
    return {
        accent: fill,
        onAccent,
        link: toward(accent, s.ink, (c) => [...grounds, soft].every((g) => contrastRatio(c, g) >= TEXT_MIN)),
        soft,
        chart: toward(accent, s.ink, (c) => grounds.every((g) => contrastRatio(c, g) >= GRAPHIC_MIN)),
    };
}
