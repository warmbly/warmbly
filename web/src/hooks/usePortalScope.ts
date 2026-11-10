import type { PublicBrand } from "@/lib/api/models/app/brand/Brand";
import { currentPortal } from "@/lib/portal";
import { parsePortalTheme } from "@/lib/portalTheme";
import { storedChoice, type ResolvedTheme } from "@/lib/theme";
import { useAppStore } from "@/stores";

/**
 * How a page drawn in an agency's brand carries its theme: undefined on the agency's own portal
 * domain, where the theme is already on the whole page, else the appearance to draw it in here.
 * The viewer's own choice wins over the brand's default.
 */
export function usePortalScope(brand: PublicBrand | undefined): ResolvedTheme | undefined {
    const resolved = useAppStore((s) => s.resolvedTheme);
    if (!brand || currentPortal().portal) return undefined;
    if (storedChoice()) return resolved;
    return parsePortalTheme(brand.theme).appearance || resolved;
}
