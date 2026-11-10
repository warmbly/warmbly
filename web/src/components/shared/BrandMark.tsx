import { Logo } from "@/components/svg";
import { DashboardImage } from "@/components/ui/dashboard-image";
import useBrand from "@/hooks/useBrand";
import { cn } from "@/lib/utils";
import { currentPortal } from "@/lib/portal";

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
                // An agency's logo is drawn for a light page, so on the sky it sits on a white tile.
                <span className="inline-flex items-center justify-center rounded-lg bg-white px-2 py-1.5 shadow-sm">
                    <DashboardImage src={portal.brand.logo_url} alt="" className="h-6 w-auto max-w-[120px] object-contain" />
                </span>
            ) : (
                <span
                    aria-hidden
                    className="size-7 rounded-md inline-flex items-center justify-center text-[13px] font-bold text-white bg-white/15 ring-1 ring-inset ring-white/25"
                >
                    {brand.name.slice(0, 1).toUpperCase()}
                </span>
            )}
            <span className="max-w-[240px] truncate font-extrabold text-[18px] tracking-tight text-white">{brand.name}</span>
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
