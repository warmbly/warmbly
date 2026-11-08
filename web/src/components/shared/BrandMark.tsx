import { Logo } from "@/components/svg";
import useBrand from "@/hooks/useBrand";
import { cn } from "@/lib/utils";
import { currentPortal, safeAccent } from "@/lib/portal";

/**
 * The wordmark on the sky: sign-in, the CLI authorization page, the cloud
 * connect page.
 *
 * It links to the deployment's website only when there is one. A self-hosted
 * instance that configured no EMAIL_BRAND_WEBSITE_URL renders plain text
 * instead, because the alternative was sending that operator's users to the
 * platform's marketing site from their own login screen.
 */
export default function BrandMark({ className }: { className: string }) {
    const brand = useBrand();
    const portal = currentPortal();
    const inner = (
        <>
            {!portal.portal ? (
                <Logo className="w-7 text-white" />
            ) : portal.brand.logo_url ? (
                <img src={portal.brand.logo_url} alt="" className="h-7 w-auto max-w-[120px] rounded object-contain" />
            ) : (
                <span
                    aria-hidden
                    className="size-7 rounded-md inline-flex items-center justify-center text-[13px] font-bold text-white bg-slate-900"
                    style={safeAccent(portal.brand.accent_color) ? { background: safeAccent(portal.brand.accent_color) } : undefined}
                >
                    {brand.name.slice(0, 1).toUpperCase()}
                </span>
            )}
            {/* A portal's sign-in sits on a light page, not the sky. */}
            <span className={cn("font-extrabold text-[18px] tracking-tight", portal.portal ? "text-slate-900" : "text-white")}>{brand.name}</span>
        </>
    );
    // Always drawn on the sky, so it keeps its white in either theme.
    if (!brand.website_url) return <div className={cn("theme-solid", className)}>{inner}</div>;
    return (
        <a href={brand.website_url} className={cn("theme-solid", className)}>
            {inner}
        </a>
    );
}
