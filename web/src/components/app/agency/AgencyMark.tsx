// An agency's mark: its brand logo when it set one, else its initials.

import { DashboardImage } from "@/components/ui/dashboard-image";
import { useAgencyBrand } from "@/lib/api/hooks/app/brand";
import { accentFill } from "@/lib/accentScale";
import { cn } from "@/lib/utils";

function initials(name: string) {
    return (
        name
            .split(" ")
            .filter(Boolean)
            .map((w) => w[0])
            .join("")
            .toUpperCase()
            .slice(0, 2) || "?"
    );
}

const SIZES = {
    sm: "size-5 rounded-[5px] text-[9px]",
    md: "size-7 rounded-md text-[11px]",
} as const;

export function AgencyMark({
    name,
    logoUrl,
    accent,
    size = "md",
    className,
}: {
    name: string;
    logoUrl?: string;
    accent?: string;
    size?: keyof typeof SIZES;
    className?: string;
}) {
    return (
        <span
            aria-hidden
            className={cn(
                "shrink-0 inline-flex items-center justify-center overflow-hidden font-bold text-white leading-none tracking-tight ring-1 ring-slate-200",
                SIZES[size],
                logoUrl ? "bg-white dark:bg-transparent p-0.5" : "bg-slate-900",
                className,
            )}
            // White initials sit on it, so a pale accent is drawn at a shade they read on.
            style={!logoUrl && accentFill(accent) ? { background: accentFill(accent) } : undefined}
        >
            {logoUrl ? <DashboardImage src={logoUrl} alt="" className="w-full h-full object-contain" /> : initials(name)}
        </span>
    );
}

/** The mark for one agency, reading its brand for the logo. */
export function AgencyBrandMark({ agencyId, name, size, className }: { agencyId: string; name: string; size?: keyof typeof SIZES; className?: string }) {
    const brand = useAgencyBrand(agencyId).data?.brand;
    return <AgencyMark name={name} logoUrl={brand?.logo_url || undefined} accent={brand?.accent_color || undefined} size={size} className={className} />;
}
