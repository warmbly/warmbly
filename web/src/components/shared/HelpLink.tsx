import type React from "react";

import { helpLink, helpLinkProps } from "@/lib/helpLink";

/**
 * A link to documentation. On an agency's portal domain a link to the service's own pages leads to
 * the agency's support link and reads `portalLabel`; when the agency set none it is not drawn at all.
 */
export default function HelpLink({
    href,
    children,
    portalLabel,
    ...rest
}: Omit<React.ComponentProps<"a">, "href" | "target" | "rel"> & {
    href: string;
    /** What the link says on a portal, where it no longer opens the page its label names. */
    portalLabel?: React.ReactNode;
}) {
    const to = helpLink(href);
    if (!to) return null;
    return (
        <a {...rest} {...helpLinkProps(to)}>
            {to !== href && portalLabel !== undefined ? portalLabel : children}
        </a>
    );
}
