import { currentPortal } from "./portal";
import { supportHref } from "./portalTheme";

/** Whether a link opens one of the service's own pages (its docs, site or community), as opposed to a third party's. */
export function isServicePage(url: string): boolean {
    try {
        return /(^|\.)warmbly\.com$/i.test(new URL(url).hostname);
    } catch {
        return true;
    }
}

/**
 * Where a help or documentation link leads. On an agency's portal domain a link to the service's own
 * pages becomes the agency's support link, and empty when it set none: the caller then draws no link,
 * because the page it would open names the service the portal is white-labelled from.
 */
export function helpLink(docs: string): string {
    const portal = currentPortal();
    return portal.portal && isServicePage(docs) ? supportHref(portal.brand.theme) : docs;
}

/** The agency's support link on its portal domain; empty anywhere else, or when it set none. */
export function portalSupportLink(): string {
    const portal = currentPortal();
    return portal.portal ? supportHref(portal.brand.theme) : "";
}

/** Anchor attributes for a help link: a web page opens in a new tab, an email address does not. */
export function helpLinkProps(href: string): { href: string; target?: string; rel?: string } {
    return href.startsWith("mailto:") ? { href } : { href, target: "_blank", rel: "noopener noreferrer" };
}
