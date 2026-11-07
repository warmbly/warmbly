export const mailPreviewCsp = "default-src 'none'; script-src 'none'; object-src 'none'; base-uri 'none'; form-action 'none'; frame-ancestors 'self'; style-src 'unsafe-inline'; img-src data: https: http:; font-src data: https:; media-src https:";

// The lead finder map's default tiles, styles, sprites and glyphs all come from here.
export const mapTileOrigin = "https://tiles.openfreemap.org";

export function dashboardCsp({ scripts = [], styles = [], nonce = "", connections = [], analytics = [], images = [] }: {
    scripts?: string[];
    styles?: string[];
    nonce?: string;
    connections?: string[];
    analytics?: string[];
    images?: string[];
} = {}) {
    const authorization = nonce ? [`'nonce-${nonce}'`] : [];
    return [
        "default-src 'none'",
        `script-src 'self' https://challenges.cloudflare.com ${[...authorization, ...scripts, ...analytics].join(" ")}`,
        "script-src-attr 'none'",
        `style-src 'self' ${[...authorization, ...styles].join(" ")}`,
        "style-src-attr 'unsafe-inline'",
        `connect-src 'self' ${mapTileOrigin} ${[...connections, ...analytics].join(" ")}`,
        `img-src 'self' data: blob: ${images.join(" ")}`,
        "font-src 'self'",
        "frame-src 'self' https://challenges.cloudflare.com",
        "worker-src 'self' blob:",
        "media-src 'self' blob:",
        "manifest-src 'self'",
        "object-src 'none'",
        "base-uri 'none'",
        "form-action 'self'",
        "frame-ancestors 'none'",
    ].map((directive) => directive.trim()).join("; ");
}

export const dashboardSecurityHeaders = {
    "X-Content-Type-Options": "nosniff",
    "X-Frame-Options": "DENY",
    "Referrer-Policy": "strict-origin-when-cross-origin",
    "Permissions-Policy": "camera=(), microphone=(), geolocation=(), interest-cohort=()",
    "Content-Security-Policy": dashboardCsp(),
} as const;
