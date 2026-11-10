// An accent colour turned into the 50 to 950 scale the dashboard draws its `sky` utilities from.
//
// Every step keeps the stock sky step's relative luminance, so a pairing that reads in the stock
// theme (white on 600, 700 on 50) reads the same whatever hue an agency picks. The accent's own
// hue and chroma are what change. A dark accent may pull 500 to 950 darker, never lighter.

export const STEPS = [50, 100, 200, 300, 400, 500, 600, 700, 800, 900, 950] as const;
export type Step = (typeof STEPS)[number];

export interface Oklch {
    /** Lightness, 0 to 1. */
    l: number;
    /** Chroma. */
    c: number;
    /** Hue in degrees. */
    h: number;
    /** Opacity, 0 to 1. */
    alpha: number;
}

export type Scale = Record<Step, Oklch>;

export interface AccentScale {
    /** The light theme. */
    light: Scale;
    /** The dark theme: tints become translucent washes and the dark shades turn light. */
    dark: Scale;
    /** A saturated fill on a dark ground, which keeps white text. */
    solid: Scale;
}

const HEX_RE = /^#[0-9a-fA-F]{6}$/;

// Tailwind's sky, as [lightness, chroma, hue].
const SKY: Record<Step, [number, number, number]> = {
    50: [0.977, 0.013, 236.62],
    100: [0.951, 0.026, 236.824],
    200: [0.901, 0.058, 230.902],
    300: [0.828, 0.111, 230.318],
    400: [0.746, 0.16, 232.661],
    500: [0.685, 0.169, 237.323],
    600: [0.588, 0.158, 241.966],
    700: [0.5, 0.134, 242.749],
    800: [0.443, 0.11, 240.79],
    900: [0.391, 0.09, 240.876],
    950: [0.293, 0.066, 243.157],
};

// styles/theme-dark.css remaps sky under .dark with these, as [lightness, chroma, hue, opacity].
const SKY_DARK: Record<Step, [number, number, number, number]> = {
    50: [0.68, 0.14, 240, 0.12],
    100: [0.68, 0.14, 240, 0.18],
    200: [0.7, 0.14, 240, 0.28],
    300: [0.72, 0.14, 240, 0.45],
    400: [0.76, 0.15, 238, 1],
    500: [0.72, 0.16, 240, 1],
    600: [0.76, 0.145, 240, 1],
    700: [0.82, 0.12, 238, 1],
    800: [0.88, 0.085, 238, 1],
    900: [0.93, 0.05, 238, 1],
    950: [0.96, 0.028, 238, 1],
};

// The same file's deeper fills for a dark ground; 50 to 400 stay the light theme's.
const SKY_SOLID: Partial<Record<Step, [number, number, number]>> = {
    500: [0.64, 0.16, 240],
    600: [0.59, 0.165, 242],
    700: [0.53, 0.16, 242],
    800: [0.47, 0.15, 242],
    900: [0.41, 0.13, 242],
    950: [0.31, 0.11, 242],
};

const REFERENCE_CHROMA = SKY[600][1];
// Below this an accent is a grey and its hue means nothing.
const ACHROMATIC = 0.004;
// How far a dark accent may pull the fills below the stock luminance.
const DARKEST_SHIFT = 0.45;

type RGB = [number, number, number];

const toLinear = (v: number) => (v <= 0.04045 ? v / 12.92 : Math.pow((v + 0.055) / 1.055, 2.4));
const toGamma = (v: number) => (v <= 0.0031308 ? v * 12.92 : 1.055 * Math.pow(v, 1 / 2.4) - 0.055);

function linearFromHex(hex: string): RGB {
    const n = parseInt(hex.slice(1), 16);
    return [toLinear(((n >> 16) & 255) / 255), toLinear(((n >> 8) & 255) / 255), toLinear((n & 255) / 255)];
}

function oklchFromLinear([r, g, b]: RGB): Oklch {
    const l = Math.cbrt(0.4122214708 * r + 0.5363325363 * g + 0.0514459929 * b);
    const m = Math.cbrt(0.2119034982 * r + 0.6806995451 * g + 0.1073969566 * b);
    const s = Math.cbrt(0.0883024619 * r + 0.2817188376 * g + 0.6299787005 * b);
    const L = 0.2104542553 * l + 0.793617785 * m - 0.0040720468 * s;
    const A = 1.9779984951 * l - 2.428592205 * m + 0.4505937099 * s;
    const B = 0.0259040371 * l + 0.7827717662 * m - 0.808675766 * s;
    const h = (Math.atan2(B, A) * 180) / Math.PI;
    return { l: L, c: Math.hypot(A, B), h: h < 0 ? h + 360 : h, alpha: 1 };
}

function linearFromOklch(L: number, c: number, h: number): RGB {
    const rad = (h * Math.PI) / 180;
    const A = c * Math.cos(rad);
    const B = c * Math.sin(rad);
    const l = Math.pow(L + 0.3963377774 * A + 0.2158037573 * B, 3);
    const m = Math.pow(L - 0.1055613458 * A - 0.0638541728 * B, 3);
    const s = Math.pow(L - 0.0894841775 * A - 1.291485548 * B, 3);
    return [
        4.0767416621 * l - 3.3077115913 * m + 0.2309699292 * s,
        -1.2684380046 * l + 2.6097574011 * m - 0.3413193965 * s,
        -0.0041960863 * l - 0.7034186147 * m + 1.707614701 * s,
    ];
}

const GAMUT_SLACK = 0.0005;
const inGamut = (rgb: RGB) => rgb.every((v) => v >= -GAMUT_SLACK && v <= 1 + GAMUT_SLACK);
const luminanceOf = ([r, g, b]: RGB) => 0.2126 * r + 0.7152 * g + 0.0722 * b;
const clamp = (v: number, lo: number, hi: number) => Math.min(hi, Math.max(lo, v));

/** The most chroma, up to c, that sRGB can show at this lightness and hue. */
function fitChroma(l: number, c: number, h: number): number {
    if (c <= 0 || inGamut(linearFromOklch(l, c, h))) return Math.max(0, c);
    let lo = 0;
    let hi = c;
    for (let i = 0; i < 18; i++) {
        const mid = (lo + hi) / 2;
        if (inGamut(linearFromOklch(l, mid, h))) lo = mid;
        else hi = mid;
    }
    return lo;
}

/** The colour of this hue and (at most this) chroma whose relative luminance is y. */
function atLuminance(y: number, c: number, h: number): Oklch {
    let lo = 0;
    let hi = 1;
    for (let i = 0; i < 26; i++) {
        const mid = (lo + hi) / 2;
        const rgb = linearFromOklch(mid, fitChroma(mid, c, h), h);
        if (luminanceOf(rgb.map((v) => clamp(v, 0, 1)) as RGB) < y) lo = mid;
        else hi = mid;
    }
    const l = (lo + hi) / 2;
    return { l, c: fitChroma(l, c, h), h, alpha: 1 };
}

const skyLuminance = (l: number, c: number, h: number) => luminanceOf(linearFromOklch(l, fitChroma(l, c, h), h));

/** Whether a value is a #rrggbb colour. */
export function isHexColour(value: unknown): value is string {
    return typeof value === "string" && HEX_RE.test(value);
}

/** The relative luminance (WCAG) of a colour, ignoring its opacity. */
export function relativeLuminance(colour: Oklch | string): number {
    const rgb = typeof colour === "string" ? linearFromHex(colour) : linearFromOklch(colour.l, colour.c, colour.h);
    return luminanceOf(rgb.map((v) => clamp(v, 0, 1)) as RGB);
}

/** The WCAG contrast ratio between two colours. */
export function contrastRatio(a: Oklch | string, b: Oklch | string): number {
    const [hi, lo] = [relativeLuminance(a), relativeLuminance(b)].sort((x, y) => y - x);
    return (hi + 0.05) / (lo + 0.05);
}

/** The stock sky scale, in the same shape a generated one has. */
export function stockScale(): AccentScale {
    const light = {} as Scale;
    const dark = {} as Scale;
    const solid = {} as Scale;
    for (const step of STEPS) {
        const [l, c, h] = SKY[step];
        light[step] = { l, c, h, alpha: 1 };
        const [dl, dc, dh, alpha] = SKY_DARK[step];
        dark[step] = { l: dl, c: dc, h: dh, alpha };
        const fill = SKY_SOLID[step];
        solid[step] = fill ? { l: fill[0], c: fill[1], h: fill[2], alpha: 1 } : light[step];
    }
    return { light, dark, solid };
}

/** The scale for a #rrggbb accent, or null when the value is not one. */
export function accentScale(hex: string | undefined | null): AccentScale | null {
    if (!isHexColour(hex)) return null;
    const accent = oklchFromLinear(linearFromHex(hex));
    const grey = accent.c < ACHROMATIC;
    const hue = grey ? 0 : accent.h;
    const strength = grey ? 0 : accent.c / REFERENCE_CHROMA;
    const stock600 = skyLuminance(...SKY[600]);
    const shift = clamp(relativeLuminance(hex) / stock600, DARKEST_SHIFT, 1);
    const shiftAt = (step: Step) => (step >= 600 ? shift : step === 500 ? Math.sqrt(shift) : 1);

    const light = {} as Scale;
    const dark = {} as Scale;
    const solid = {} as Scale;
    for (const step of STEPS) {
        const [l, c, h] = SKY[step];
        light[step] = atLuminance(skyLuminance(l, c, h) * shiftAt(step), c * strength, hue);

        const [dl, dc, , alpha] = SKY_DARK[step];
        dark[step] = { l: dl, c: fitChroma(dl, dc * strength, hue), h: hue, alpha };

        const fill = SKY_SOLID[step];
        solid[step] = fill ? atLuminance(skyLuminance(...fill) * shiftAt(step), fill[1] * strength, hue) : light[step];
    }
    return { light, dark, solid };
}

/** A fill white text reads on, drawn from an accent of any lightness; undefined when it is not a colour. */
export function accentFill(hex: string | undefined | null): string | undefined {
    const scale = accentScale(hex);
    return scale ? oklchCSS(scale.light[600]) : undefined;
}

const round = (v: number, places: number) => {
    const f = Math.pow(10, places);
    return Math.round(v * f) / f;
};

/** A colour as CSS, built from numbers alone. */
export function oklchCSS({ l, c, h, alpha }: Oklch): string {
    const body = `${round(l * 100, 2)}% ${round(c, 4)} ${round(h, 2)}`;
    return alpha < 1 ? `oklch(${body} / ${round(alpha, 3)})` : `oklch(${body})`;
}

/** A colour as #rrggbb, clipped to sRGB. */
export function oklchHex({ l, c, h }: Oklch): string {
    const channel = (v: number) =>
        Math.round(clamp(toGamma(clamp(v, 0, 1)), 0, 1) * 255)
            .toString(16)
            .padStart(2, "0");
    const [r, g, b] = linearFromOklch(l, fitChroma(l, c, h), h);
    return `#${channel(r)}${channel(g)}${channel(b)}`;
}
