import { useLayoutEffect } from "react";

import type { PublicBrand } from "@/lib/api/models/app/brand/Brand";
import { currentPortal } from "@/lib/portal";
import { applyBrandTheme } from "@/lib/portalTheme";

/**
 * Puts a client brand's theme on the dashboard while it applies and takes it off after. An agency's
 * portal domain already carries its theme from the moment the portal resolved, so nothing is done there.
 */
export function useBrandTheme(brand: PublicBrand | null) {
    const onPortal = currentPortal().portal;
    const key = brand ? JSON.stringify([brand.accent_color ?? "", brand.theme ?? {}]) : "";
    // Before the paint, so the chrome never shows one frame in the stock colours.
    useLayoutEffect(() => {
        if (onPortal || !key) return;
        const [accent_color, theme] = JSON.parse(key) as [string, PublicBrand["theme"]];
        applyBrandTheme({ accent_color, theme });
        return () => applyBrandTheme(null);
    }, [onPortal, key]);
}
