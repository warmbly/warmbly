// Theme preference, its resolution against the OS, and the switch itself.
// index.html applies the stored choice before the first paint with the same
// rules; keep STORAGE_KEY and the resolution there in step with this file.

export type Theme = "light" | "dark" | "system";
export type ResolvedTheme = "light" | "dark";

export const THEME_STORAGE_KEY = "theme";

// Browser chrome on phones (address bar, PWA title bar) matches the sidebar.
const CHROME_COLOR: Record<ResolvedTheme, string> = { light: "#f5f6f8", dark: "#070708" };

const DARK_QUERY = "(prefers-color-scheme: dark)";

export const isTheme = (v: unknown): v is Theme => v === "light" || v === "dark" || v === "system";

// Light until someone picks otherwise; "system" is an explicit choice.
export const DEFAULT_THEME: Theme = "light";

// A brand's own default for a viewer who has not chosen; index.html reads the same key before the first paint.
export const THEME_DEFAULT_STORAGE_KEY = "theme-default";

function rememberedDefault(): Theme {
    if (typeof window === "undefined") return DEFAULT_THEME;
    try {
        const v = localStorage.getItem(THEME_DEFAULT_STORAGE_KEY);
        return v === "light" || v === "dark" ? v : DEFAULT_THEME;
    } catch {
        return DEFAULT_THEME;
    }
}

// Starts from what index.html painted, so the store and the page agree before the brand is known.
let fallback: Theme = rememberedDefault();
const fallbackListeners = new Set<() => void>();

/** The theme the viewer picked themselves, or null when they never did. */
export function storedChoice(): Theme | null {
    if (typeof window === "undefined") return null;
    try {
        const v = localStorage.getItem(THEME_STORAGE_KEY);
        return isTheme(v) ? v : null;
    } catch {
        return null;
    }
}

/** The viewer's own choice, else the default in effect. */
export function readStoredTheme(): Theme {
    return storedChoice() ?? fallback;
}

/**
 * Sets the theme for viewers who have not chosen one (null puts light back). A stored choice always wins.
 * `remember` keeps it for the next visit's first paint, which only suits an address that always has this default.
 */
export function setDefaultTheme(theme: ResolvedTheme | null, remember = false) {
    const next: Theme = theme ?? DEFAULT_THEME;
    try {
        if (theme && remember) localStorage.setItem(THEME_DEFAULT_STORAGE_KEY, theme);
        else localStorage.removeItem(THEME_DEFAULT_STORAGE_KEY);
    } catch {
        // No storage: the default still applies to this tab.
    }
    const changed = next !== fallback;
    fallback = next;
    if (storedChoice()) return;
    // Also repaints a first paint made from a remembered default that no longer holds.
    applyTheme(resolveTheme(next));
    if (changed) fallbackListeners.forEach((cb) => cb());
}

/** Runs when the default theme changes for a viewer who follows it. */
export function onDefaultThemeChange(cb: () => void): () => void {
    fallbackListeners.add(cb);
    return () => fallbackListeners.delete(cb);
}

export function storeTheme(theme: Theme) {
    try {
        localStorage.setItem(THEME_STORAGE_KEY, theme);
    } catch {
        // Private mode or a full quota: the choice still applies to this tab.
    }
}

export function systemTheme(): ResolvedTheme {
    if (typeof window === "undefined" || !window.matchMedia) return "light";
    return window.matchMedia(DARK_QUERY).matches ? "dark" : "light";
}

export const resolveTheme = (theme: Theme): ResolvedTheme => (theme === "system" ? systemTheme() : theme);

export function onSystemThemeChange(cb: (t: ResolvedTheme) => void): () => void {
    if (typeof window === "undefined" || !window.matchMedia) return () => {};
    const mq = window.matchMedia(DARK_QUERY);
    const handler = () => cb(mq.matches ? "dark" : "light");
    mq.addEventListener("change", handler);
    return () => mq.removeEventListener("change", handler);
}

function paint(resolved: ResolvedTheme) {
    const root = document.documentElement;
    root.classList.toggle("dark", resolved === "dark");
    root.style.colorScheme = resolved;
    let meta = document.querySelector<HTMLMetaElement>('meta[name="theme-color"]');
    if (!meta) {
        meta = document.createElement("meta");
        meta.name = "theme-color";
        document.head.appendChild(meta);
    }
    meta.content = CHROME_COLOR[resolved];
}

const prefersReducedMotion = () =>
    typeof window !== "undefined" && !!window.matchMedia?.("(prefers-reduced-motion: reduce)").matches;

/** Where a switch was asked for, so the new theme can grow out of that point. */
export type ThemeOrigin = { x: number; y: number };

type ViewTransitionDocument = Document & {
    startViewTransition?: (cb: () => void) => { ready: Promise<void>; finished: Promise<void> };
};

/**
 * Paints a resolved theme. With an origin the new theme spreads from it as a
 * circle where View Transitions exist; otherwise the swap is instant, with every
 * transition held for a frame so hundreds of hover fades do not run at once.
 */
// The theme last asked for. A view transition paints a frame later, so the DOM
// alone would let a second call in the same tick repaint under the snapshot.
let target: ResolvedTheme | null = null;

export function applyTheme(resolved: ResolvedTheme, origin?: ThemeOrigin) {
    if (typeof document === "undefined") return;
    const root = document.documentElement;
    const current = target ?? (root.classList.contains("dark") ? "dark" : "light");
    if (current === resolved && root.style.colorScheme) return;
    target = resolved;

    const doc = document as ViewTransitionDocument;
    if (origin && doc.startViewTransition && !prefersReducedMotion()) {
        root.classList.add("theme-switching");
        const t = doc.startViewTransition(() => paint(resolved));
        t.ready
            .then(() => {
                const r = Math.hypot(
                    Math.max(origin.x, window.innerWidth - origin.x),
                    Math.max(origin.y, window.innerHeight - origin.y),
                );
                root.animate(
                    { clipPath: [`circle(0px at ${origin.x}px ${origin.y}px)`, `circle(${r}px at ${origin.x}px ${origin.y}px)`] },
                    { duration: 520, easing: "cubic-bezier(0.22, 1, 0.36, 1)", pseudoElement: "::view-transition-new(root)" },
                );
            })
            .catch(() => {});
        t.finished.finally(() => root.classList.remove("theme-switching"));
        return;
    }

    root.classList.add("theme-instant");
    paint(resolved);
    // Two frames: one for the new colours to compute, one to paint them.
    requestAnimationFrame(() => requestAnimationFrame(() => root.classList.remove("theme-instant")));
}
