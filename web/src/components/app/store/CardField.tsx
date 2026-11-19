// The checkout's card field: Stripe's Payment Element, drawn in the page. Saving confirms a SetupIntent
// (3-D Secure included) so the card stays on the workspace for the automatic monthly charges.
import React from "react";
import type { Stripe, StripeElements } from "@stripe/stripe-js";
import { CreditCardIcon } from "lucide-react";
import { stripeFor } from "@/lib/buy/stripe";
import { saveCard, type CardSaveResult } from "./cardSave";

export interface CardFieldHandle {
    /** Saves the typed card; returns where a redirect-only bank would come back to. */
    save: (returnUrl: string) => Promise<CardSaveResult>;
}

export interface CardFieldState {
    status: "loading" | "ready" | "failed";
    complete: boolean;
}

// The field matches the dashboard: slate borders, sky focus, 6px corners, the app's type size.
const APPEARANCE = {
    theme: "stripe" as const,
    variables: {
        colorPrimary: "#0284c7",
        colorText: "#0f172a",
        colorTextSecondary: "#64748b",
        colorTextPlaceholder: "#94a3b8",
        colorDanger: "#dc2626",
        fontFamily: "-apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, sans-serif",
        fontSizeBase: "13px",
        borderRadius: "6px",
        spacingUnit: "3px",
    },
    rules: {
        ".Input": { border: "1px solid #e2e8f0", boxShadow: "none", padding: "8px 10px" },
        ".Input:focus": { border: "1px solid #38bdf8", boxShadow: "0 0 0 2px #e0f2fe" },
        ".Label": { fontSize: "11px", color: "#64748b" },
    },
};

export default function CardField({
    publishableKey,
    onState,
    ref,
}: {
    publishableKey: string;
    onState: (s: CardFieldState) => void;
    ref?: React.Ref<CardFieldHandle>;
}) {
    const host = React.useRef<HTMLDivElement>(null);
    const stripe = React.useRef<Stripe | null>(null);
    const elements = React.useRef<StripeElements | null>(null);
    const [status, setStatus] = React.useState<CardFieldState["status"]>("loading");
    const report = React.useRef(onState);
    report.current = onState;

    React.useEffect(() => {
        let gone = false;
        let complete = false;
        const tell = (s: CardFieldState["status"]) => {
            if (gone) return;
            setStatus(s);
            report.current({ status: s, complete });
        };
        tell("loading");
        void stripeFor(publishableKey).then((s) => {
            if (gone) return;
            if (!s || !host.current) return tell("failed");
            stripe.current = s;
            try {
                const els = s.elements({ mode: "setup", currency: "usd", allowedPaymentMethodTypes: ["card"], appearance: APPEARANCE });
                elements.current = els;
                const field = els.create("payment", { layout: "tabs", wallets: { applePay: "never", googlePay: "never" } });
                field.on("ready", () => tell("ready"));
                field.on("loaderror", () => tell("failed"));
                field.on("change", (e) => {
                    complete = e.complete;
                    if (!gone) report.current({ status: "ready", complete });
                });
                field.mount(host.current);
            } catch {
                tell("failed");
            }
        });
        return () => {
            gone = true;
            elements.current?.getElement("payment")?.destroy();
            elements.current = null;
        };
    }, [publishableKey]);

    React.useImperativeHandle(
        ref,
        () => ({
            save: async (returnUrl) => {
                const s = stripe.current;
                const els = elements.current;
                if (!s || !els) return { ok: false, message: "The card field did not load. Reload the page and try again." };
                return saveCard(s, els, returnUrl);
            },
        }),
        [],
    );

    return (
        <div>
            {status === "loading" && (
                <div aria-busy="true" className="space-y-2">
                    <div className="h-9 rounded-md skeleton-shimmer" />
                    <div className="grid grid-cols-2 gap-2">
                        <div className="h-9 rounded-md skeleton-shimmer" />
                        <div className="h-9 rounded-md skeleton-shimmer" />
                    </div>
                </div>
            )}
            {status === "failed" && (
                <p className="flex items-start gap-1.5 text-[11.5px] text-slate-500 leading-relaxed">
                    <CreditCardIcon className="w-3.5 h-3.5 mt-px text-slate-400 shrink-0" />
                    The card field could not load here. Pay opens Stripe&apos;s page to add the card, then brings you back.
                </p>
            )}
            <div ref={host} className={status === "loading" ? "h-0 overflow-hidden" : undefined} />
        </div>
    );
}
