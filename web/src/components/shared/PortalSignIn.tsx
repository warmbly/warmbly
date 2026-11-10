// The sign-in page of an agency's portal: its layout, background, heading and message around
// whatever form the page renders. The portal domain, the main address's page for a portal and the
// Branding preview all draw this one component.

import React from "react";

import { DashboardImage } from "@/components/ui/dashboard-image";
import type { PortalSignInBackground, PortalTheme, PublicBrand } from "@/lib/api/models/app/brand/Brand";
import { isHexColour } from "@/lib/accentScale";
import {
    brandImageURL,
    httpsURL,
    loadThemeFont,
    parsePortalTheme,
    scopedVariables,
    signInBackground,
    supportHref,
} from "@/lib/portalTheme";
import type { ResolvedTheme } from "@/lib/theme";
import { cn } from "@/lib/utils";

/** The heading a layout with a brand panel shows when the agency wrote none. */
function defaultSignInHeading(name: string): string {
    return name ? `Welcome to ${name}` : "Welcome";
}

export default function PortalSignIn({
    brand,
    children,
    scope,
    contained = false,
}: {
    brand: PublicBrand;
    children: React.ReactNode;
    /** Carries the theme on this subtree in one appearance: a page on an address that is not the brand's own, or a preview. */
    scope?: ResolvedTheme;
    /** Fills its parent, not the viewport. */
    contained?: boolean;
}) {
    const theme = React.useMemo(() => parsePortalTheme(brand.theme), [brand.theme]);
    const accent = isHexColour(brand.accent_color) ? brand.accent_color : undefined;
    const image = brandImageURL(brand.sign_in_image_url);
    const background = signInBackground(theme, image);
    const layout = theme.sign_in_layout ?? "";
    const style = React.useMemo(
        () => (scope ? (scopedVariables(accent, theme, scope) as React.CSSProperties) : undefined),
        [scope, accent, theme],
    );
    // The brand's own address loads its font when the portal resolves; anywhere else this page asks for it.
    React.useEffect(() => {
        if (scope) void loadThemeFont(theme.font);
    }, [scope, theme.font]);

    const copy = {
        heading: theme.sign_in_heading || (layout ? defaultSignInHeading(brand.name) : ""),
        message: theme.sign_in_message ?? "",
    };
    const parts = { brand, theme, background, image, copy, children };

    return (
        <div
            style={style}
            className={cn(
                "@container/signin relative isolate flex w-full flex-col bg-slate-50 text-slate-900",
                contained ? "h-full overflow-y-auto" : "min-h-dvh",
                scope && "font-[family-name:var(--wb-font)]",
                scope === "dark" && "dark",
                scope === "light" && "theme-light",
            )}
        >
            {layout === "split" ? <Split {...parts} /> : layout === "cover" ? <Cover {...parts} /> : <Centred {...parts} />}
        </div>
    );
}

interface Parts {
    brand: PublicBrand;
    theme: PortalTheme;
    background: PortalSignInBackground;
    image: string | undefined;
    copy: { heading: string; message: string };
    children: React.ReactNode;
}

// A card under the brand, with the background as a band behind its top edge.
function Centred({ brand, theme, background, image, copy, children }: Parts) {
    const painted = background !== "";
    return (
        <div className={cn("flex flex-1 flex-col", !painted && "justify-center py-8")}>
            <div className={cn("relative", painted && "pb-24 pt-10 @2xl/signin:pb-28 @2xl/signin:pt-16")}>
                <Paint background={background} image={image} />
                <div
                    className={cn(
                        "relative z-10 mx-auto flex w-full max-w-[560px] flex-col items-center px-5 text-center",
                        painted && "theme-solid text-white",
                    )}
                >
                    <Mark brand={brand} painted={painted} stacked />
                    <Copy copy={copy} className="mt-5" headingClassName="text-[22px] @2xl/signin:text-[26px]" />
                </div>
            </div>
            <div className={cn("relative z-10 flex flex-col items-center px-4", painted ? "-mt-16 flex-1 pb-8" : "mt-7")}>
                <Card>{children}</Card>
                <Footer brand={brand} theme={theme} className={painted ? "mt-auto pt-8" : "mt-7"} />
            </div>
        </div>
    );
}

// The brand in a panel beside the form; on a phone the panel is a band above it.
function Split({ brand, theme, background, image, copy, children }: Parts) {
    const painted = background !== "";
    return (
        <div className="grid flex-1 grid-cols-1 grid-rows-[auto_1fr] @5xl/signin:grid-cols-2 @5xl/signin:grid-rows-1">
            <div
                className={cn(
                    "relative flex flex-col overflow-hidden",
                    !painted && "border-b border-slate-200 bg-slate-100/70 @5xl/signin:border-b-0 @5xl/signin:border-r",
                )}
            >
                <Paint background={background} image={image} />
                <div
                    className={cn(
                        "relative z-10 flex flex-1 flex-col gap-6 px-6 py-7 @5xl/signin:px-12 @5xl/signin:py-11",
                        painted && "theme-solid text-white",
                    )}
                >
                    <Mark brand={brand} painted={painted} />
                    <Copy
                        copy={copy}
                        className="max-w-[480px] @5xl/signin:my-auto"
                        headingClassName="text-[24px] @5xl/signin:text-[40px] @5xl/signin:leading-[1.1]"
                        messageClassName="@5xl/signin:mt-4 @5xl/signin:text-[16px]"
                    />
                </div>
            </div>
            <div className="relative flex flex-col bg-white px-5 py-9 @5xl/signin:px-12">
                <div className="m-auto w-full max-w-[380px]">{children}</div>
                <Footer brand={brand} theme={theme} className="mt-9" />
            </div>
        </div>
    );
}

// The background fills the page and the form floats on it.
function Cover({ brand, theme, background, image, copy, children }: Parts) {
    const painted = background !== "";
    const support = supportHref(theme);
    return (
        <>
            <Paint background={background} image={image} />
            <div
                className={cn(
                    "relative z-10 flex items-center justify-between gap-4 px-5 py-5 @5xl/signin:px-10 @5xl/signin:py-7",
                    painted && "theme-solid text-white",
                )}
            >
                <Mark brand={brand} painted={painted} />
                {support && (
                    <a href={support} {...external(support)} className="shrink-0 text-[13px] font-medium opacity-75 transition-opacity hover:opacity-100">
                        Support
                    </a>
                )}
            </div>
            <div className="relative z-10 flex flex-1 items-center px-4 py-6 @5xl/signin:px-10">
                <div className="mx-auto grid w-full max-w-[1040px] items-center justify-items-center gap-8 @5xl/signin:grid-cols-[minmax(0,1fr)_420px] @5xl/signin:gap-16">
                    <Copy
                        copy={copy}
                        className={cn("max-w-[560px] text-center @5xl/signin:justify-self-start @5xl/signin:text-left", painted && "theme-solid text-white")}
                        headingClassName="text-[26px] @5xl/signin:text-[46px] @5xl/signin:leading-[1.08]"
                        messageClassName="@5xl/signin:mt-5 @5xl/signin:text-[17px]"
                    />
                    <Card className="shadow-[0_1px_2px_rgba(15,23,42,0.06),0_40px_90px_-30px_rgba(8,10,15,0.55)]">{children}</Card>
                </div>
            </div>
            <Footer brand={brand} theme={theme} painted={painted} hideSupport className="relative z-10 px-5 pb-6 pt-2" />
        </>
    );
}

function Paint({ background, image }: { background: PortalSignInBackground; image: string | undefined }) {
    if (!background) return null;
    return (
        <div
            aria-hidden
            className={cn(
                "absolute inset-0 overflow-hidden",
                background === "accent" && "wb-signin-accent",
                background === "dark" && "wb-signin-dark",
                background === "image" && "wb-signin-image bg-[#0b0d12]",
            )}
        >
            {background === "image" ? (
                <DashboardImage src={image} alt="" decoding="async" className="absolute inset-0 h-full w-full object-cover" />
            ) : (
                <div className="wb-signin-grid absolute inset-0" />
            )}
        </div>
    );
}

function Mark({ brand, painted, stacked = false }: { brand: PublicBrand; painted: boolean; stacked?: boolean }) {
    const website = httpsURL(brand.website_url);
    const logo = brandImageURL(brand.logo_url);
    const inner = (
        <>
            {logo ? (
                // A logo is drawn for a light page, so a painted background gives it a white tile.
                <span className={cn("inline-flex shrink-0 items-center justify-center", painted && "rounded-lg bg-white px-2 py-1.5 shadow-sm")}>
                    <DashboardImage src={logo} alt="" className={cn("w-auto object-contain", stacked ? "h-9 max-w-[180px]" : "h-7 max-w-[140px]")} />
                </span>
            ) : (
                <span
                    aria-hidden
                    className={cn(
                        "inline-flex shrink-0 items-center justify-center rounded-lg font-bold",
                        stacked ? "size-11 text-[18px]" : "size-8 text-[14px]",
                        painted ? "bg-white/15 text-white ring-1 ring-inset ring-white/25" : "bg-sky-600 text-white",
                    )}
                >
                    {(brand.name || "?").slice(0, 1).toUpperCase()}
                </span>
            )}
            <span className={cn("min-w-0 font-semibold tracking-tight", stacked ? "line-clamp-2 text-[15px] [overflow-wrap:anywhere]" : "truncate text-[17px]")}>
                {brand.name}
            </span>
        </>
    );
    const className = cn("flex min-w-0 max-w-full items-center", stacked ? "flex-col gap-2.5 text-center" : "gap-2.5");
    if (!website) return <div className={className}>{inner}</div>;
    return (
        <a href={website} target="_blank" rel="noopener noreferrer" className={className}>
            {inner}
        </a>
    );
}

function Copy({
    copy,
    className,
    headingClassName,
    messageClassName,
}: {
    copy: Parts["copy"];
    className?: string;
    headingClassName?: string;
    messageClassName?: string;
}) {
    if (!copy.heading && !copy.message) return null;
    return (
        <div className={cn("min-w-0 [overflow-wrap:anywhere]", className)}>
            {copy.heading && <h2 className={cn("text-balance font-semibold leading-tight tracking-tight", headingClassName)}>{copy.heading}</h2>}
            {copy.message && <p className={cn("mt-2.5 text-[14px] leading-relaxed opacity-75", messageClassName)}>{copy.message}</p>}
        </div>
    );
}

function Card({ children, className }: { children: React.ReactNode; className?: string }) {
    return (
        <div
            className={cn(
                "animate-card-float w-full max-w-[420px] rounded-2xl border border-slate-200 bg-white px-6 py-8 @md/signin:px-9 @md/signin:py-10",
                "shadow-[0_1px_2px_rgba(15,23,42,0.04),0_30px_70px_-32px_rgba(15,23,42,0.32)]",
                className,
            )}
        >
            {children}
        </div>
    );
}

const external = (href: string) => (href.startsWith("mailto:") ? {} : { target: "_blank", rel: "noopener noreferrer" });

function Footer({
    brand,
    theme,
    painted = false,
    hideSupport = false,
    className,
}: {
    brand: PublicBrand;
    theme: PortalTheme;
    painted?: boolean;
    hideSupport?: boolean;
    className?: string;
}) {
    const link = cn("transition-colors", painted ? "hover:text-white" : "hover:text-slate-700");
    const support = hideSupport ? "" : supportHref(theme);
    const poweredBy = brand.powered_by;
    return (
        <div
            className={cn(
                "flex w-full flex-wrap items-center justify-center gap-x-3.5 gap-y-1 text-[12px]",
                painted ? "theme-solid text-white/70" : "text-slate-400",
                className,
            )}
        >
            {support && (
                <a href={support} {...external(support)} className={link}>
                    Support
                </a>
            )}
            {theme.terms_url && (
                <a href={theme.terms_url} target="_blank" rel="noopener noreferrer" className={link}>
                    Terms
                </a>
            )}
            {theme.privacy_url && (
                <a href={theme.privacy_url} target="_blank" rel="noopener noreferrer" className={link}>
                    Privacy
                </a>
            )}
            {poweredBy &&
                (poweredBy.url ? (
                    <a href={poweredBy.url} target="_blank" rel="noopener noreferrer" className={link}>
                        Powered by {poweredBy.name}
                    </a>
                ) : (
                    <span>Powered by {poweredBy.name}</span>
                ))}
            <span className="max-w-full truncate">
                © {new Date().getFullYear()} {brand.name}
            </span>
        </div>
    );
}
