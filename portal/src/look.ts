// Puts a brand's look on the document: data attributes styles.css maps to fixed fonts, corners and palettes,
// and the colour variables derived from the accent. No string from the API is written into CSS here.

import { useLayoutEffect } from "react";

import { isEmbedded } from "./embed";
import { type Brand, DEFAULT_LOOK, type FontChoice, type Look, lookOf, parseLook, toneVars } from "./theme";

// The two webfonts ship with the bundle and load only for a brand that chose one.
const FONT_FILES: Partial<Record<FontChoice, () => Promise<unknown>>> = {
    geometric: () => import("@fontsource-variable/manrope/wght.css"),
    serif: () => import("@fontsource-variable/source-serif-4/wght.css"),
};

const STORE_KEY = "warmbly-portal:look";

// pageTag names this page without keeping its link token (FNV-1a over the path).
function pageTag(): string {
    let h = 0x811c9dc5;
    for (const ch of window.location.pathname) h = Math.imul(h ^ ch.charCodeAt(0), 0x01000193);
    return (h >>> 0).toString(16);
}

export function applyLook(look: Look) {
    const root = document.documentElement;
    root.dataset.appearance = look.appearance;
    root.dataset.corners = look.corners;
    root.dataset.font = look.font;
    for (const [name, value] of Object.entries(toneVars(look.accent))) root.style.setProperty(name, value);
    // A font that fails to load leaves the fallback stack in place.
    void FONT_FILES[look.font]?.().catch(() => undefined);
}

function rememberLook(look: Look) {
    try {
        localStorage.setItem(STORE_KEY, JSON.stringify({ page: pageTag(), look }));
    } catch {
        // no storage (private mode, a partitioned frame): the next visit starts from the default
    }
}

function recallLook(): Look {
    try {
        const held = JSON.parse(localStorage.getItem(STORE_KEY) ?? "null") as { page?: unknown; look?: unknown } | null;
        if (held?.page === pageTag()) return parseLook(held.look);
    } catch {
        // unreadable: fall through
    }
    return DEFAULT_LOOK;
}

/** startLook runs before the first paint: the look this page had last time, so a returning visitor sees no flip. */
export function startLook() {
    if (isEmbedded()) {
        document.documentElement.dataset.embed = "1";
        document.body.classList.add("embed");
    }
    applyLook(recallLook());
}

function setTabIcon(url: string | undefined) {
    if (!url) return;
    let link = document.querySelector<HTMLLinkElement>('link[rel~="icon"]');
    if (!link) {
        link = document.createElement("link");
        link.rel = "icon";
        document.head.appendChild(link);
    }
    link.href = url;
}

/** useBrandLook applies a loaded brand: its look, and its icon (else its logo) on the browser tab. */
export function useBrandLook(brand: Brand | undefined) {
    useLayoutEffect(() => {
        if (!brand) return;
        const look = lookOf(brand);
        applyLook(look);
        rememberLook(look);
        setTabIcon(brand.iconURL ?? brand.logoURL);
    }, [brand]);
}
