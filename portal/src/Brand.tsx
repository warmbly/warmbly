// The agency's mark and footer, shared by the connect page and the report.

import { type ReactNode, useState } from "react";

import type { Brand } from "./theme";

/** BrandMark is the logo, or the name when there is no logo or it does not load, linked to the agency's website. */
export function BrandMark({ brand }: { brand: Brand }) {
    const [broken, setBroken] = useState(false);
    const mark =
        brand.logoURL && !broken ? (
            <img className="pt-logo" src={brand.logoURL} alt={brand.name} onError={() => setBroken(true)} />
        ) : brand.name ? (
            <span className="pt-brandname">{brand.name}</span>
        ) : null;
    if (!mark) return null;
    return brand.websiteURL ? (
        <a className="pt-mark" href={brand.websiteURL} target="_blank" rel="noopener noreferrer">
            {mark}
        </a>
    ) : (
        mark
    );
}

function Outside({ href, children }: { href: string; children: ReactNode }) {
    return (
        <a href={href} target="_blank" rel="noopener noreferrer">
            {children}
        </a>
    );
}

/** BrandFooter: the agency's support, terms and privacy links when it set them, then the powered-by line. */
export function BrandFooter({ brand, className, showName = false }: { brand: Brand; className?: string; showName?: boolean }) {
    const { theme, poweredBy } = brand;
    const help = theme.supportURL ? (
        <Outside href={theme.supportURL}>Contact support</Outside>
    ) : theme.supportEmail ? (
        <a href={`mailto:${theme.supportEmail}`}>{theme.supportEmail}</a>
    ) : null;

    const meta: { key: string; node: ReactNode }[] = [];
    if (showName && brand.name) meta.push({ key: "name", node: <span>{brand.name}</span> });
    if (theme.termsURL) meta.push({ key: "terms", node: <Outside href={theme.termsURL}>Terms</Outside> });
    if (theme.privacyURL) meta.push({ key: "privacy", node: <Outside href={theme.privacyURL}>Privacy</Outside> });
    if (poweredBy) {
        meta.push({
            key: "powered",
            node: poweredBy.url ? <Outside href={poweredBy.url}>Powered by {poweredBy.name}</Outside> : <span>Powered by {poweredBy.name}</span>,
        });
    }
    if (!help && meta.length === 0) return null;

    return (
        <footer className={className ? `pt-foot ${className}` : "pt-foot"}>
            {help && <p className="pt-foot-help">Need help? {help}</p>}
            {meta.length > 0 && (
                <p className="pt-foot-meta">
                    {meta.map((m, i) => (
                        <span key={m.key} className="pt-foot-item">
                            {i > 0 && <span aria-hidden="true"> · </span>}
                            {m.node}
                        </span>
                    ))}
                </p>
            )}
        </footer>
    );
}
