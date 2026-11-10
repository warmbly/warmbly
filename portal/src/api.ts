// The same-origin JSON API the portal service (portal/worker) exposes for these pages.

/** The tab outlived its render token; only a reload mints a new one. */
export class StalePageError extends Error {
    constructor() {
        super("stale page");
        this.name = "StalePageError";
    }
}

// The service stamps the render token into this meta tag; every API call must carry it.
const renderToken = document.querySelector('meta[name="wf-token"]')?.getAttribute("content") ?? "";

export function apiHeaders(): Record<string, string> {
    return { Accept: "application/json", "X-Warmbly-Render": renderToken };
}
