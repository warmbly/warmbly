import { createContext, useContext } from "react";
import type { PlanID } from "@/lib/plans";

/** What a locked surface tells the upgrade dialog about itself. */
export interface UpgradeRequest {
    /** Feature name as shown in the sidebar / page title ("Unified inbox"). */
    feature: string;
    /** Minimum plan that unlocks it. Defaults to starter. */
    minPlan?: PlanID;
    /** One-sentence pitch shown under the title. */
    blurb?: string;
    /** Feature-specific bullets shown in the hero; falls back to the plan's. */
    bullets?: string[];
}

interface UpgradeDialogContextValue {
    open: (req: UpgradeRequest) => void;
    close: () => void;
}

export const UpgradeDialogContext = createContext<UpgradeDialogContextValue | undefined>(undefined);

export function useUpgradeDialog() {
    const c = useContext(UpgradeDialogContext);
    if (!c) {
        throw Error("UpgradeDialogProvider not found");
    }
    return c;
}
