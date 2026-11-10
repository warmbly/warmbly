// The agency's brand as the client pages use it: parsed strictly from the API answer, plus the colour maths
// that turns one accent into the tones the pages draw with. Pure and import-free, so node:test loads it as is.

export type FontChoice = "default" | "system" | "geometric" | "serif";
export type Corners = "default" | "sharp" | "round";
export type Appearance = "light" | "dark";

export interface Theme {
    font: FontChoice;
    corners: Corners;
    appearance: Appearance;
    supportEmail?: string;
    supportURL?: string;
    termsURL?: string;
    privacyURL?: string;
}

export interface Brand {
    name: string;
    logoURL?: string;
    iconURL?: string;
    websiteURL?: string;
    /** Always #rrggbb, lower case. */
    accent: string;
    poweredBy?: { name: string; url?: string };
    theme: Theme;
}

/** The four choices that change how a page is drawn; all of it is enums and one validated colour. */
export interface Look {
    accent: string;
    font: FontChoice;
    corners: Corners;
    appearance: Appearance;
}

export const DEFAULT_ACCENT = "#0284c7";
export const DEFAULT_LOOK: Look = { accent: DEFAULT_ACCENT, font: "default", corners: "default", appearance: "light" };

const FONTS: readonly FontChoice[] = ["system", "geometric", "serif"];
const CORNERS: readonly Corners[] = ["sharp", "round"];
const APPEARANCES: readonly Appearance[] = ["light", "dark"];

const HEX = /^#[0-9a-f]{6}$/i;
const EMAIL = /^[a-z0-9._+-]{1,64}@[a-z0-9-]+(\.[a-z0-9-]+)+$/i;
const MAX_URL = 2048;
const MAX_NAME = 120;

function record(v: unknown): Record<string, unknown> {
    return typeof v === "object" && v !== null && !Array.isArray(v) ? (v as Record<string, unknown>) : {};
}

function oneOf<T extends string>(v: unknown, allowed: readonly T[], fallback: T): T {
    return typeof v === "string" && (allowed as readonly string[]).includes(v) ? (v as T) : fallback;
}

function text(v: unknown, max: number): string {
    return typeof v === "string" ? v.trim().slice(0, max) : "";
}

/** parseHex accepts exactly #rrggbb; anything else is no colour at all. */
export function parseHex(v: unknown): string | undefined {
    return typeof v === "string" && HEX.test(v) ? v.toLowerCase() : undefined;
}

function urlWith(v: unknown, protocols: readonly string[]): string | undefined {
    if (typeof v !== "string" || v.length === 0 || v.length > MAX_URL) return undefined;
    let u: URL;
    try {
        u = new URL(v);
    } catch {
        return undefined;
    }
    if (!protocols.includes(u.protocol) || u.username || u.password) return undefined;
    return u.href;
}

/** httpsURL is the rule for every link the theme adds. */
export function httpsURL(v: unknown): string | undefined {
    return urlWith(v, ["https:"]);
}

// The logo, icon, website and powered-by link keep the rule they had: a self-host may serve them over plain http.
function webURL(v: unknown): string | undefined {
    return urlWith(v, ["https:", "http:"]);
}

export function parseEmail(v: unknown): string | undefined {
    const s = text(v, 255);
    return s.length <= 254 && EMAIL.test(s) ? s : undefined;
}

export function parseTheme(raw: unknown): Theme {
    const t = record(raw);
    return {
        font: oneOf(t.font, FONTS, "default"),
        corners: oneOf(t.corners, CORNERS, "default"),
        appearance: oneOf(t.appearance, APPEARANCES, "light"),
        supportEmail: parseEmail(t.support_email),
        supportURL: httpsURL(t.support_url),
        termsURL: httpsURL(t.terms_url),
        privacyURL: httpsURL(t.privacy_url),
    };
}

export function parseBrand(raw: unknown): Brand {
    const b = record(raw);
    const powered = record(b.powered_by);
    const poweredName = text(powered.name, MAX_NAME);
    return {
        name: text(b.name, MAX_NAME),
        logoURL: webURL(b.logo_url),
        iconURL: webURL(b.icon_url),
        websiteURL: webURL(b.website_url),
        accent: parseHex(b.accent_color) ?? DEFAULT_ACCENT,
        poweredBy: poweredName ? { name: poweredName, url: webURL(powered.url) } : undefined,
        theme: parseTheme(b.theme),
    };
}

export function lookOf(brand: Brand): Look {
    return { accent: brand.accent, font: brand.theme.font, corners: brand.theme.corners, appearance: brand.theme.appearance };
}

/** parseLook reads a remembered look back with the same rules as a fresh one. */
export function parseLook(raw: unknown): Look {
    const l = record(raw);
    return {
        accent: parseHex(l.accent) ?? DEFAULT_ACCENT,
        font: oneOf(l.font, FONTS, "default"),
        corners: oneOf(l.corners, CORNERS, "default"),
        appearance: oneOf(l.appearance, APPEARANCES, "light"),
    };
}

// Colour. Every function takes and returns #rrggbb.

const WHITE = "#ffffff";
const BLACK = "#000000";
/** The dark text a light accent carries. */
export const DARK_TEXT = "#0f172a";

/** The grounds each appearance draws on. styles.css declares the same values; theme.test.mjs holds them together. */
export const SURFACES: Record<Appearance, { page: string; card: string; ink: string }> = {
    light: { page: "#f6f7f9", card: "#ffffff", ink: "#0f172a" },
    dark: { page: "#0b0f17", card: "#131923", ink: "#e8ecf3" },
};

// White text is kept down to this contrast, then the button is deepened until it reads at TEXT_MIN.
const WHITE_TEXT_MIN = 3;
const TEXT_MIN = 4.5;
const GRAPHIC_MIN = 3;

function channels(hex: string): [number, number, number] {
    const n = parseInt(hex.slice(1), 16);
    return [(n >> 16) & 255, (n >> 8) & 255, n & 255];
}

/** mix returns a with the given share (0 to 1) of b blended in. */
export function mix(a: string, b: string, share: number): string {
    const ca = channels(a);
    const cb = channels(b);
    const s = Math.min(1, Math.max(0, share));
    return "#" + ca.map((v, i) => Math.round(v + (cb[i] - v) * s).toString(16).padStart(2, "0")).join("");
}

export function luminance(hex: string): number {
    const [r, g, b] = channels(hex).map((v) => {
        const c = v / 255;
        return c <= 0.03928 ? c / 12.92 : ((c + 0.055) / 1.055) ** 2.4;
    });
    return 0.2126 * r + 0.7152 * g + 0.0722 * b;
}

/** contrast is the WCAG ratio, from 1 to 21. */
export function contrast(a: string, b: string): number {
    const la = luminance(a);
    const lb = luminance(b);
    return (Math.max(la, lb) + 0.05) / (Math.min(la, lb) + 0.05);
}

/** textOn picks what is written on the accent: white on a dark one, dark text on a light one. */
export function textOn(accent: string): string {
    return contrast(accent, WHITE) >= WHITE_TEXT_MIN ? WHITE : DARK_TEXT;
}

// toward walks from one colour to another and stops at the first step that passes.
function toward(from: string, to: string, ok: (c: string) => boolean): string {
    const steps = 25;
    for (let i = 0; i < steps; i++) {
        const c = mix(from, to, i / steps);
        if (ok(c)) return c;
    }
    return to;
}

export interface Tones {
    /** Button fill. */
    accent: string;
    accentHover: string;
    /** Text on the button. */
    onAccent: string;
    /** Button border: the fill itself unless the fill would vanish into the card. */
    accentEdge: string;
    /** Accent as text on the page, the card and the soft tint. */
    link: string;
    /** Soft tint for selected and hovered surfaces. */
    soft: string;
    /** The halo around a focused field. */
    halo: string;
    /** Focus outline. */
    ring: string;
    /** Chart marks. */
    chart: string;
}

export function deriveTones(accent: string, appearance: Appearance): Tones {
    const s = SURFACES[appearance];
    const onAccent = textOn(accent);
    const fill = onAccent === WHITE ? toward(accent, BLACK, (c) => contrast(c, WHITE) >= TEXT_MIN) : accent;
    const soft = mix(s.card, accent, appearance === "dark" ? 0.16 : 0.08);
    const grounds = [s.page, s.card];
    const graphic = toward(accent, s.ink, (c) => grounds.every((g) => contrast(c, g) >= GRAPHIC_MIN));
    return {
        accent: fill,
        accentHover: mix(fill, BLACK, onAccent === WHITE ? 0.14 : 0.08),
        onAccent,
        accentEdge: contrast(fill, s.card) >= 1.6 ? fill : mix(fill, s.ink, 0.28),
        link: toward(accent, s.ink, (c) => [...grounds, soft].every((g) => contrast(c, g) >= TEXT_MIN)),
        soft,
        halo: mix(s.card, accent, appearance === "dark" ? 0.36 : 0.24),
        ring: graphic,
        chart: graphic,
    };
}

const TONE_NAMES: Record<keyof Tones, string> = {
    accent: "accent",
    accentHover: "accent-hover",
    onAccent: "on-accent",
    accentEdge: "accent-edge",
    link: "link",
    soft: "soft",
    halo: "halo",
    ring: "ring",
    chart: "chart",
};

/** toneVars is every custom property the pages take from the accent, for both appearances (print always reads the light set). */
export function toneVars(accent: string): Record<string, string> {
    const out: Record<string, string> = {};
    for (const [prefix, appearance] of [["l", "light"], ["d", "dark"]] as const) {
        const tones = deriveTones(accent, appearance);
        for (const key of Object.keys(TONE_NAMES) as (keyof Tones)[]) {
            out[`--pt-${prefix}-${TONE_NAMES[key]}`] = tones[key];
        }
    }
    return out;
}
