export function isModuleLoadError(error: unknown): boolean {
    return error instanceof TypeError && isModuleLoadMessage(error.message);
}

export function isModuleLoadMessage(message: string): boolean {
    if (/^(Failed to fetch dynamically imported module|error loading dynamically imported module)(:|$)/i.test(message)
        || message === "Importing a module script failed.") return true;

    const mime = message.match(/^['"]text\/html['"] is not a valid JavaScript MIME type for module script ['"]([^'"]+)['"]\.?$/);
    if (!mime || typeof window === "undefined") return false;
    try {
        const url = new URL(mime[1], window.location.href);
        return url.origin === window.location.origin && /^\/assets\/[^/]+\.js$/.test(url.pathname);
    } catch {
        return false;
    }
}

// A second failure this soon after a reload is not a stale tab, so it renders and reports instead of looping.
const RELOAD_KEY = "warmbly:stale-chunk-reload-at";
const RELOAD_WINDOW_MS = 60_000;
let reloading = false;

// canReloadForStaleChunk says whether a module-load failure now would be answered with a reload.
export function canReloadForStaleChunk(): boolean {
    if (reloading) return true;
    try {
        const last = Number(window.sessionStorage.getItem(RELOAD_KEY));
        const age = Date.now() - last;
        return !(last > 0 && age >= 0 && age < RELOAD_WINDOW_MS);
    } catch {
        return false;
    }
}

// reloadForStaleChunk reloads once when a deploy removed the chunk this tab asked for, and says whether it did.
export function reloadForStaleChunk(error: unknown): boolean {
    if (!isModuleLoadError(error)) return false;
    if (reloading) return true;
    if (!canReloadForStaleChunk()) return false;
    try {
        window.sessionStorage.setItem(RELOAD_KEY, String(Date.now()));
    } catch {
        return false;
    }
    reloading = true;
    window.location.reload();
    return true;
}
