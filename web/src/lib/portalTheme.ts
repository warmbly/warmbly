// A brand's theme on the dashboard: the accent as the whole `sky` scale, the font, the corners and
// the default appearance. Everything written to the page is a number computed here or a constant
// picked by a validated choice; no stored string reaches CSS.

import type {
    PortalAppearance,
    PortalCorners,
    PortalFont,
    PortalSignInBackground,
    PortalSignInLayout,
    PortalTheme,
    PublicBrand,
} from "@/lib/api/models/app/brand/Brand";
import { HEADING_MAX, MESSAGE_MAX } from "./displayName";
import { STEPS, accentScale, isHexColour, oklchCSS, stockScale, type AccentScale } from "./accentScale";
import { setDefaultTheme, type ResolvedTheme } from "./theme";

export const SIGN_IN_HEADING_MAX = HEADING_MAX;
export const SIGN_IN_MESSAGE_MAX = MESSAGE_MAX;

const FONTS: readonly PortalFont[] = ["system", "geometric", "serif"];
const CORNERS: readonly PortalCorners[] = ["sharp", "round"];
const APPEARANCES: readonly PortalAppearance[] = ["light", "dark"];
const LAYOUTS: readonly PortalSignInLayout[] = ["split", "cover"];
const BACKGROUNDS: readonly PortalSignInBackground[] = ["accent", "dark", "image"];

/** The stack each font choice sets; the two webfonts are bundled and load only when chosen. */
export const FONT_STACKS: Record<Exclude<PortalFont, "">, string> = {
    system: 'system-ui, -apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, "Helvetica Neue", Arial, sans-serif',
    geometric: '"Manrope Variable", system-ui, -apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, Arial, sans-serif',
    serif: '"Source Serif 4 Variable", "Iowan Old Style", "Palatino Linotype", Georgia, "Times New Roman", serif',
};

/** The base radius each corner choice sets; the stock one is 0.5rem. */
export const CORNER_RADIUS: Record<Exclude<PortalCorners, "">, string> = {
    sharp: "0.125rem",
    round: "0.75rem",
};

function choice<T extends string>(value: unknown, allowed: readonly T[]): T | undefined {
    return allowed.find((v) => v === value);
}

// Control characters and invisible direction or spacing marks have no place in a line of copy.
const UNPRINTABLE = /[\p{Cc}\u200b\u200e\u200f\u2028-\u202e\u2060-\u2064\ufeff]/gu;

function plainText(value: unknown, max: number): string | undefined {
    if (typeof value !== "string") return undefined;
    const text = value.replace(UNPRINTABLE, " ").replace(/\s+/g, " ").trim();
    return text ? Array.from(text).slice(0, max).join("") : undefined;
}

/** An https address with a real host and no credentials, normalised; empty for anything else. */
export function httpsURL(value: unknown): string {
    if (typeof value !== "string" || !value.trim()) return "";
    try {
        const u = new URL(value.trim());
        return u.protocol === "https:" && u.hostname.includes(".") && !u.username && !u.password ? u.href : "";
    } catch {
        return "";
    }
}

// Mirrors brandkit.NormalizeEmail: a plain mailbox@domain address.
const EMAIL_RE =
    /^[A-Za-z0-9_+-]+(?:\.[A-Za-z0-9_+-]+)*@[A-Za-z0-9](?:[A-Za-z0-9-]*[A-Za-z0-9])?(?:\.[A-Za-z0-9](?:[A-Za-z0-9-]*[A-Za-z0-9])?)+$/;

/** An email address fit for a mailto link; empty for anything else. */
export function supportEmail(value: unknown): string {
    if (typeof value !== "string") return "";
    const email = value.trim();
    return email.length <= 254 && email.indexOf("@") <= 64 && EMAIL_RE.test(email) ? email : "";
}

/** An image address a brand may show: http(s) or a path on this origin. */
export function brandImageURL(value: unknown): string | undefined {
    if (typeof value !== "string" || !value) return undefined;
    if (value.startsWith("/") && !value.startsWith("//")) return value;
    try {
        const u = new URL(value);
        return u.protocol === "https:" || u.protocol === "http:" ? u.href : undefined;
    } catch {
        return undefined;
    }
}

/** Accepts only well-formed theme values; an unknown choice or a malformed link is left out, which means the default. */
export function parsePortalTheme(raw: unknown): PortalTheme {
    if (!raw || typeof raw !== "object") return {};
    const r = raw as Record<string, unknown>;
    const theme: PortalTheme = {
        font: choice(r.font, FONTS),
        corners: choice(r.corners, CORNERS),
        appearance: choice(r.appearance, APPEARANCES),
        sign_in_layout: choice(r.sign_in_layout, LAYOUTS),
        sign_in_background: choice(r.sign_in_background, BACKGROUNDS),
        sign_in_heading: plainText(r.sign_in_heading, SIGN_IN_HEADING_MAX),
        sign_in_message: plainText(r.sign_in_message, SIGN_IN_MESSAGE_MAX),
        support_email: supportEmail(r.support_email) || undefined,
        support_url: httpsURL(r.support_url) || undefined,
        terms_url: httpsURL(r.terms_url) || undefined,
        privacy_url: httpsURL(r.privacy_url) || undefined,
    };
    for (const key of Object.keys(theme) as (keyof PortalTheme)[]) {
        if (theme[key] === undefined) delete theme[key];
    }
    return theme;
}

/** Where a brand's support link leads: its page, else its address; empty when it set neither. */
export function supportHref(theme: PortalTheme | undefined): string {
    if (!theme) return "";
    return theme.support_url || (theme.support_email ? `mailto:${theme.support_email}` : "");
}

/** The sign-in background in effect: an image choice with no image uploaded falls back to the accent. */
export function signInBackground(theme: PortalTheme | undefined, imageURL: string | undefined): PortalSignInBackground {
    const bg = theme?.sign_in_background ?? "";
    return bg === "image" && !imageURL ? "accent" : bg;
}

type Variables = Record<string, string>;

// The names styles/theme-accent.css and styles/theme-dark.css read: light, dark and solid-fill contexts.
function scaleVariables(scale: AccentScale): Variables {
    const out: Variables = {};
    for (const step of STEPS) {
        out[`--wb-sky-${step}`] = oklchCSS(scale.light[step]);
        out[`--wb-sky-d-${step}`] = oklchCSS(scale.dark[step]);
        if (step >= 500) out[`--wb-sky-s-${step}`] = oklchCSS(scale.solid[step]);
    }
    return out;
}

function lookVariables(theme: PortalTheme): Variables {
    const out: Variables = {};
    if (theme.font) {
        out["--wb-font"] = FONT_STACKS[theme.font];
        out["--wb-font-sans"] = FONT_STACKS[theme.font];
    }
    if (theme.corners) out["--radius"] = CORNER_RADIUS[theme.corners];
    return out;
}

/** The custom properties a theme sets on the root; empty when it changes nothing. */
export function themeVariables(accent: string | undefined, theme: PortalTheme): Variables {
    const scale = accentScale(accent);
    return { ...(scale ? scaleVariables(scale) : {}), ...lookVariables(theme) };
}

// The stacks global.css sets when no brand font is chosen.
const STOCK_FONT = "-apple-system, BlinkMacSystemFont, 'Segoe UI', 'Roboto', sans-serif";
const STOCK_FONT_SANS = "'Inter Variable', -apple-system, BlinkMacSystemFont, 'Segoe UI', system-ui, sans-serif";

/**
 * The same theme for one subtree in one appearance (a preview, or a branded page on an unbranded
 * address). Every value is explicit, so it also undoes a theme the page around it carries. The
 * subtree's root sets `font-family: var(--wb-font)` itself.
 */
export function scopedVariables(accent: string | undefined, theme: PortalTheme, mode: ResolvedTheme): Variables {
    const scale = accentScale(accent) ?? stockScale();
    const out = scaleVariables(scale);
    for (const step of STEPS) out[`--color-sky-${step}`] = oklchCSS(scale[mode][step]);
    out["--radius"] = theme.corners ? CORNER_RADIUS[theme.corners] : "0.5rem";
    out["--wb-font"] = theme.font ? FONT_STACKS[theme.font] : STOCK_FONT;
    out["--wb-font-sans"] = theme.font ? FONT_STACKS[theme.font] : STOCK_FONT_SANS;
    return out;
}

const FONT_FILES: Partial<Record<PortalFont, { face: string; load: () => Promise<unknown> }>> = {
    geometric: { face: '"Manrope Variable"', load: () => import("@fontsource-variable/manrope/wght.css") },
    serif: { face: '"Source Serif 4 Variable"', load: () => import("@fontsource-variable/source-serif-4/wght.css") },
};

/** Loads a chosen webfont; resolves either way, and a failure leaves the fallback stack. */
export function loadThemeFont(font: PortalFont | undefined): Promise<void> {
    const file = font ? FONT_FILES[font] : undefined;
    if (!file) return Promise.resolve();
    return file
        .load()
        .then(() => (typeof document !== "undefined" ? document.fonts?.load(`1em ${file.face}`) : undefined))
        .then(
            () => undefined,
            () => undefined,
        );
}

/** Waits for a chosen webfont, but no longer than capMs, so a slow font never holds the page. */
export function themeFontReady(font: PortalFont | undefined, capMs: number): Promise<void> {
    return Promise.race([loadThemeFont(font), new Promise<void>((resolve) => setTimeout(resolve, capMs))]);
}

type ThemedBrand = Pick<PublicBrand, "accent_color" | "theme">;

let applied: string[] = [];
let active = false;

/**
 * Puts a brand's theme on the page, or takes it off (null). `ownAddress` is an agency's portal
 * domain: only there does the brand's default appearance apply, and it is remembered for the next
 * visit's first paint. Anywhere else the viewer keeps the appearance they already have.
 */
export function applyBrandTheme(brand: ThemedBrand | null, ownAddress = false) {
    if (typeof document === "undefined") return;
    if (!brand && !active && !ownAddress) return;
    const root = document.documentElement;
    const theme = parsePortalTheme(brand?.theme);
    const accent = isHexColour(brand?.accent_color) ? brand.accent_color : undefined;
    const variables = themeVariables(accent, theme);

    for (const name of applied) if (!(name in variables)) root.style.removeProperty(name);
    for (const [name, value] of Object.entries(variables)) root.style.setProperty(name, value);
    applied = Object.keys(variables);

    if (accent) root.dataset.accent = "";
    else delete root.dataset.accent;

    void loadThemeFont(theme.font);
    if (ownAddress) setDefaultTheme(theme.appearance || null, true);
    active = !!brand;
}
