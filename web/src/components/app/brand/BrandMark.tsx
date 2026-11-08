// BrandMark — the mark at the top of the dashboard: Warmbly's, or for a client
// member of a branded workspace, that workspace's logo and name in its accent.

import { Logo } from "@/components/svg";
import { DashboardImage } from "@/components/ui/dashboard-image";
import { useClientBrand } from "@/hooks/useClientBrand";
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
    const accent = brand.accent_color || undefined;
    return (
        <>
            {brand.logo_url ? (
                <DashboardImage src={brand.logo_url} alt={brand.name || "Logo"} className="h-7 max-w-[120px] object-contain shrink-0" />
            ) : (
                <span
                    aria-hidden
                    className="size-7 rounded-md shrink-0 inline-flex items-center justify-center text-[13px] font-bold text-white bg-slate-900"
                    style={accent ? { background: accent } : undefined}
                >
                    {(brand.name || "?").slice(0, 1).toUpperCase()}
                </span>
            )}
            {brand.name && (
                <span className={cn("font-semibold tracking-tight text-slate-900 truncate max-w-[150px]", wordmarkClassName)} style={accent ? { color: accent } : undefined}>
                    {brand.name}
                </span>
            )}
        </>
    );
}
