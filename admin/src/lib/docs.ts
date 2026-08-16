// Docs links arrive from the API as site-relative paths with a trailing
// slash ("/development/configuration/#secrets"), never as absolute URLs.

export const DOCS_BASE = "https://docs.warmbly.com";

export function docsUrl(path: string): string {
    if (!path) return DOCS_BASE;
    if (/^https?:\/\//i.test(path)) return path;
    return `${DOCS_BASE}${path.startsWith("/") ? "" : "/"}${path}`;
}
