// Opens the product tour once for every member who has not finished or skipped
// it, and whenever the profile menu asks. The tour itself is its own chunk, so
// the dashboard pays nothing for it until it plays.

import { Suspense, lazy, useEffect } from "react";
import { AnimatePresence } from "framer-motion";
import useUser from "@/lib/api/hooks/auth/useUser";
import { useAppStore } from "@/stores";
import { billingPreview } from "./tourUi";

const ProductTour = lazy(() => import("./ProductTour"));

export default function ProductTourHost() {
    const open = useAppStore((s) => s.productTourOpen);
    const setOpen = useAppStore((s) => s.setProductTourOpen);
    const { data: user } = useUser();
    // Waits for the workspace, so the tour never plays over the redirect to /select-org.
    const orgId = useAppStore((s) => s.currentOrganization?.id ?? "");
    // Strictly null: a backend that predates the field leaves it out, and must not replay the tour every visit.
    const pending = !!user?.onboarding_completed_at && user.product_tour_completed_at === null;

    useEffect(() => {
        if ((pending || billingPreview() !== null) && orgId) setOpen(true);
    }, [pending, orgId, setOpen]);

    return (
        <Suspense fallback={null}>
            <AnimatePresence>{open && <ProductTour key="tour" />}</AnimatePresence>
        </Suspense>
    );
}
