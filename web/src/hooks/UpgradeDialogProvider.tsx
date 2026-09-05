// UpgradeDialogProvider — mounts the full-screen plan chooser once for the
// whole app and exposes useUpgradeDialog().open({ feature, minPlan }) to any
// locked surface. Also settles a Stripe Checkout return (?checkout=success)
// wherever the user left from, so the unlocked feature is live in place.

import React from "react";
import { useQueryClient } from "@tanstack/react-query";
import { useSearchParams } from "react-router-dom";
import toast from "react-hot-toast";
import UpgradeDialog from "@/components/layout/UpgradeDialog";
import { UpgradeDialogContext, type UpgradeRequest } from "./context/upgrade";

export default function UpgradeDialogProvider({ children }: { children: React.ReactNode }) {
    const [request, setRequest] = React.useState<UpgradeRequest | null>(null);
    const [visible, setVisible] = React.useState(false);

    const open = React.useCallback((req: UpgradeRequest) => {
        setRequest(req);
        setVisible(true);
    }, []);
    // The request is kept while the exit animation plays out.
    const close = React.useCallback(() => setVisible(false), []);

    useCheckoutReturn();

    const value = React.useMemo(() => ({ open, close }), [open, close]);

    return (
        <UpgradeDialogContext.Provider value={value}>
            {children}
            {request && <UpgradeDialog open={visible} request={request} onClose={close} />}
        </UpgradeDialogContext.Provider>
    );
}

// Stripe sends the browser back to the page checkout started from with a
// `checkout` query param. Refresh the subscription, say so, and strip the
// param so a reload does not repeat the toast.
function useCheckoutReturn() {
    const [params, setParams] = useSearchParams();
    const queryClient = useQueryClient();
    const result = params.get("checkout");

    React.useEffect(() => {
        if (result !== "success" && result !== "cancel") return;
        if (result === "success") {
            queryClient.invalidateQueries({ queryKey: ["subscription"] });
            queryClient.invalidateQueries({ queryKey: ["organizations"] });
            toast.success("Your plan is active. Everything it includes is unlocked.");
        } else {
            toast("Checkout canceled. Your workspace is unchanged.");
        }
        const next = new URLSearchParams(params);
        next.delete("checkout");
        setParams(next, { replace: true });
        // params/setParams change identity on every navigation; only the
        // presence of the marker should trigger this.
        // eslint-disable-next-line react-hooks/exhaustive-deps
    }, [result]);
}
