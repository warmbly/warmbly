// An agency's portal domain: the same dashboard served on a host the agency owns, in its brand.

import type { PublicBrand } from "@/lib/api/models/app/brand/Brand";
import { applyBrandTheme, brandImageURL, parsePortalTheme, themeFontReady } from "./portalTheme";

export type PortalInfo =
    | { portal: false }
    | {
          portal: true;
          host: string;
          agency_id: string;
          brand: PublicBrand;
          main_url: string;
          captcha_frame: boolean;
          /** Where the agency's clients sign in: on this portal, or on the main address. */
          sign_in: PortalSignIn;
      };

export type PortalSignIn = "portal" | "warmbly";

export type Portal = Extract<PortalInfo, { portal: true }>;

export const NO_PORTAL: PortalInfo = { portal: false };

function originOf(url: string): string {
    try {
        const u = new URL(url);
        return u.protocol === "http:" || u.protocol === "https:" ? u.origin : "";
    } catch {
        return "";
    }
}

/** Whether this page could be a portal: the main address is known and this is not it. */
export function mayBePortal(appUrl: string, locationOrigin: string): boolean {
    const main = originOf(appUrl);
    return !!main && !!locationOrigin && main !== locationOrigin;
}

/** Accepts only a well-formed portal answer; anything else is no portal. */
export function parsePortalInfo(raw: unknown): PortalInfo {
    if (!raw || typeof raw !== "object") return NO_PORTAL;
    const r = raw as Record<string, unknown>;
    const brand = r.brand as Record<string, unknown> | undefined;
    if (r.portal !== true || typeof r.agency_id !== "string" || !r.agency_id || !brand || typeof brand.name !== "string") {
        return NO_PORTAL;
    }
    const str = (v: unknown) => (typeof v === "string" && v ? v : undefined);
    const poweredBy = brand.powered_by as Record<string, unknown> | undefined;
    return {
        portal: true,
        host: typeof r.host === "string" ? r.host : "",
        agency_id: r.agency_id,
        brand: {
            name: brand.name,
            logo_url: str(brand.logo_url),
            icon_url: brandImageURL(brand.icon_url),
            sign_in_image_url: brandImageURL(brand.sign_in_image_url),
            accent_color: str(brand.accent_color),
            website_url: str(brand.website_url),
            theme: parsePortalTheme(brand.theme),
            powered_by: poweredBy && typeof poweredBy.name === "string" && poweredBy.name
                ? { name: poweredBy.name, url: str(poweredBy.url) }
                : undefined,
        },
        main_url: originOf(typeof r.main_url === "string" ? r.main_url : ""),
        captcha_frame: r.captcha_frame === true,
        sign_in: r.sign_in === "warmbly" ? "warmbly" : "portal",
    };
}

/** Whether this portal sends its sign-in to the main address, and that address is known. */
export function signsInOnMain(portal: PortalInfo): boolean {
    return portal.portal && portal.sign_in === "warmbly" && !!portal.main_url;
}

/** The main address's branded sign-in for this portal host. */
export function mainSignInURL(portal: PortalInfo, portalHost: string): string {
    if (!portal.portal || !portal.main_url || !portalHost) return "";
    return `${portal.main_url}/portal/sign-in?host=${encodeURIComponent(portalHost)}`;
}

/** A host as the main address's sign-in page accepts it: a bare lowercase hostname, else empty. */
export function portalHostParam(raw: string | null | undefined): string {
    const host = (raw ?? "").trim().toLowerCase();
    return /^(?=.{1,253}$)([a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?\.)+[a-z]{2,63}$/.test(host) ? host : "";
}

/** The one-time code a handoff carries in the address fragment, else empty. */
export function handoffCode(hash: string): string {
    const params = new URLSearchParams(hash.startsWith("#") ? hash.slice(1) : hash);
    const code = params.get("code") ?? "";
    return /^[A-Za-z0-9_-]{16,128}$/.test(code) ? code : "";
}

/** A redirect the API returned for a handoff, only when it lands on the portal host that was asked for. */
export function safeHandoffRedirect(url: string, host: string): string {
    try {
        const u = new URL(url);
        return (u.protocol === "https:" || u.protocol === "http:") && u.hostname === host && u.pathname === "/portal/return" ? u.href : "";
    } catch {
        return "";
    }
}

/** The workspaces reachable on this portal: only the owning agency's clients. */
export function portalWorkspaces<O extends { agency?: { id: string } | null }>(orgs: O[], portal: PortalInfo): O[] {
    if (!portal.portal) return orgs;
    return orgs.filter((o) => o.agency?.id === portal.agency_id);
}

/** The agencies whose view this portal offers: only the one that owns it. */
export function portalAgencies<A extends { id: string }>(agencies: A[], portal: PortalInfo): A[] {
    if (!portal.portal) return agencies;
    return agencies.filter((a) => a.id === portal.agency_id);
}

/** The name the tab title, header and auth pages carry. */
export function portalBrandName(portal: PortalInfo, fallback: string): string {
    return portal.portal && portal.brand.name ? portal.brand.name : fallback;
}

/** The main address without its scheme, for "use the main address" notes. */
export function mainHost(portal: PortalInfo): string {
    if (!portal.portal || !portal.main_url) return "the main address";
    try {
        return new URL(portal.main_url).host;
    } catch {
        return "the main address";
    }
}

/** Which captcha a page draws: the normal widget, the API-origin bridge frame, or a notice to use the main address. */
export function captchaMode(portal: PortalInfo): "widget" | "frame" | "unavailable" {
    if (!portal.portal) return "widget";
    return portal.captcha_frame ? "frame" : "unavailable";
}

export interface CaptchaMessage {
    token: string;
    error: boolean;
}

/** A captcha bridge message, only from the bridge frame this page opened on the API origin. */
export function readCaptchaMessage(
    event: { origin: string; source: unknown; data: unknown },
    apiOrigin: string,
    frame: unknown,
): CaptchaMessage | null {
    if (!apiOrigin || !frame || event.origin !== apiOrigin || event.source !== frame) return null;
    const d = event.data as Record<string, unknown> | null;
    if (!d || typeof d !== "object" || d.type !== "warmbly:captcha" || typeof d.token !== "string") return null;
    return { token: d.token, error: d.error === true };
}

/** The bridge page's address for this page's origin. */
export function captchaFrameURL(apiBase: string, locationOrigin: string): string {
    return `${apiBase}/auth/captcha-frame?origin=${encodeURIComponent(locationOrigin)}`;
}

let resolved: PortalInfo = NO_PORTAL;
let pending: Promise<PortalInfo> | null = null;

/** Asks the API once per page whether this host is a verified portal; fails open to none. */
export function resolvePortal(appUrl: string, apiBase: string): Promise<PortalInfo> {
    if (pending) return pending;
    const origin = typeof window === "undefined" ? "" : window.location.origin;
    if (!mayBePortal(appUrl, origin) || !apiBase) {
        pending = Promise.resolve(NO_PORTAL);
        return pending;
    }
    const controller = new AbortController();
    const timer = setTimeout(() => controller.abort(), 5000);
    pending = fetch(`${apiBase}/auth/portal?host=${encodeURIComponent(window.location.host)}`, {
        signal: controller.signal,
        credentials: "omit",
    })
        .then((res) => (res.ok ? res.json() : null))
        .then(parsePortalInfo, () => NO_PORTAL)
        .then(async (info) => {
            resolved = info;
            applyPortalChrome(info);
            // A portal set in a webfont waits briefly for it, so the first paint is already in that face.
            if (info.portal) await themeFontReady(info.brand.theme?.font, 800);
            return info;
        })
        .finally(() => clearTimeout(timer));
    return pending;
}

/** A #rrggbb accent, or empty for anything else. */
export function safeAccent(value: string | undefined): string {
    return value && /^#[0-9a-fA-F]{6}$/.test(value) ? value : "";
}

/** Puts the agency's theme on the page and points the tab icon at its icon, else its logo. */
function applyPortalChrome(info: PortalInfo) {
    if (typeof document === "undefined") return;
    if (!info.portal) {
        // An address that was a portal on an earlier visit drops the default appearance it remembered.
        applyBrandTheme(null, true);
        return;
    }
    applyBrandTheme(info.brand, true);
    const icon = info.brand.icon_url || info.brand.logo_url;
    if (!icon) return;
    document.querySelectorAll<HTMLLinkElement>('link[rel~="icon"], link[rel="apple-touch-icon"]').forEach((link) => {
        link.removeAttribute("sizes");
        link.removeAttribute("type");
        link.href = icon;
    });
}

/** The portal this page resolved; NO_PORTAL until resolvePortal settles. */
export function currentPortal(): PortalInfo {
    return resolved;
}
