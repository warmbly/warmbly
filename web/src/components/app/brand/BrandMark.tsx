// BrandMark — the mark at the top of the dashboard: Warmbly's, or for a client
// member of a branded workspace, that workspace's logo and name in its accent.

import { Logo } from "@/components/svg";
import { DashboardImage } from "@/components/ui/dashboard-image";
import { useClientBrand } from "@/hooks/useClientBrand";
import { isHexColour } from "@/lib/accentScale";
import { cn } from "@/lib/utils";

export default function BrandMark({
    logoClassName,
    wordmarkClassName,
}: {
    logoClassName: string;
    /** Classes for the name beside the mark; it decides when the name shows. */
    wordmarkClassName: string;
}) {
    const brand = useClientBrand();
    if (!brand) {
        return (
            <>
                <Logo className={logoClassName} />
                <span style={{ fontFamily: "var(--font-display)" }} className={cn("font-extrabold tracking-tight text-slate-900", wordmarkClassName)}>
                    Warmbly
                </span>
            </>
        );
    }
    // The brand's theme is on the page (AppShell), so sky is its accent at a shade that reads.
    const accent = isHexColour(brand.accent_color);
    return (
        <>
            {brand.logo_url ? (
                <DashboardImage src={brand.logo_url} alt={brand.name || "Logo"} className="h-7 max-w-[120px] object-contain shrink-0" />
            ) : (
                <span
                    aria-hidden
                    className={cn(
                        "size-7 rounded-md shrink-0 inline-flex items-center justify-center text-[13px] font-bold text-white",
                        accent ? "bg-sky-600" : "bg-slate-900",
                    )}
                >
                    {(brand.name || "?").slice(0, 1).toUpperCase()}
                </span>
            )}
            {brand.name && (
                <span className={cn("font-semibold tracking-tight truncate max-w-[150px]", accent ? "text-sky-700" : "text-slate-900", wordmarkClassName)}>
                    {brand.name}
                </span>
            )}
        </>
    );
}
