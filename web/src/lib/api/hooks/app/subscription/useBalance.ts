import { queryOptions, useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import toast from "react-hot-toast/headless";
import {
    createBalanceCheckout,
    createPaymentMethodSetup,
    getBalance,
    listBalanceInvoices,
    topUpBalance,
    updateAutoReload,
} from "@/lib/api/client/app/subscription/balance";
import type { AppError } from "@/lib/api/client/normalizeError";
import type { AutoReloadPatch, Balance } from "@/lib/api/models/app/subscription/Credits";
import buildError from "@/lib/helper/buildError";
import { fmtUsd } from "@/lib/money";

// Under ['subscription','credits'] so every spend, top-up and realtime credit event refreshes it.
export const balanceQuery = queryOptions({
    queryKey: ["subscription", "credits", "balance"],
    queryFn: ({ signal }) => getBalance(signal),
    staleTime: 30_000,
});

// Outside ['subscription'] so a stream of AI charges does not re-list Stripe invoices; money added refreshes it.
export const balanceInvoicesQuery = queryOptions({
    queryKey: ["balance-invoices"],
    queryFn: ({ signal }) => listBalanceInvoices(signal),
    staleTime: 60_000,
});

export function useBalance(enabled = true) {
    return useQuery({ ...balanceQuery, enabled });
}

export function useBalanceInvoices(enabled = true) {
    return useQuery({ ...balanceInvoicesQuery, enabled });
}

/** The settings to turn on once the customer is back from saving a card. */
export const RESUME_AUTO_RELOAD_KEY = "warmbly.balance.resume-auto-reload";

async function goToCheckout(amount: number) {
    const { checkout_url } = await createBalanceCheckout(amount);
    window.location.assign(checkout_url);
}

/** Stripe Checkout for an amount; it saves the card for next time. */
export function useBalanceCheckout() {
    return useMutation({
        mutationFn: goToCheckout,
        onError: (e: AppError) => toast.error(buildError(e)),
    });
}

/** Stripe Checkout in setup mode: saves or replaces the card. */
export function useAddPaymentMethod() {
    return useMutation({
        mutationFn: async () => {
            const { checkout_url } = await createPaymentMethodSetup();
            window.location.assign(checkout_url);
        },
        onError: (e: AppError) => toast.error(buildError(e)),
    });
}

// The card was not charged here; Checkout can still take the payment.
const CHECKOUT_FALLBACK = new Set(["card_declined", "authentication_required", "payment_failed", "payment_method_required"]);

/**
 * Charges the saved card now. A refusal carries on in Stripe Checkout with the
 * same amount, so the person still gets to pay. Resolves false when it did.
 */
export function useTopUp() {
    const qc = useQueryClient();
    return useMutation({
        mutationFn: async (amount: number) => {
            try {
                return await topUpBalance(amount);
            } catch (e) {
                const err = e as AppError;
                if (err?.code && CHECKOUT_FALLBACK.has(err.code)) {
                    toast.loading(
                        err.code === "authentication_required"
                            ? "Your bank wants to confirm this payment. Opening Stripe Checkout…"
                            : "Opening Stripe Checkout…",
                        { id: "balance-checkout" },
                    );
                    await goToCheckout(amount);
                    return null;
                }
                throw e;
            }
        },
        onSuccess: (r) => {
            if (!r) return;
            qc.setQueryData<Balance>(balanceQuery.queryKey, (prev) =>
                prev ? { ...prev, balance_usd: r.balance_usd, charge_in_progress: r.charge_in_progress } : prev,
            );
            void qc.invalidateQueries({ queryKey: ["subscription", "credits"] });
            void qc.invalidateQueries({ queryKey: balanceInvoicesQuery.queryKey });
            if (r.status === "paid") toast.success(`${fmtUsd(r.amount_usd)} added to the balance`);
            else toast("Your payment is still going through. The balance updates when it lands.");
        },
        onError: (e: AppError) => {
            toast.dismiss("balance-checkout");
            toast.error(buildError(e));
        },
    });
}

/** Saves automatic reload. Optimistic: the cache changes first and rolls back on error. */
export function useUpdateAutoReload() {
    const qc = useQueryClient();
    return useMutation({
        mutationFn: (patch: AutoReloadPatch) => updateAutoReload(patch),
        onMutate: async (patch) => {
            await qc.cancelQueries({ queryKey: balanceQuery.queryKey });
            const prev = qc.getQueryData<Balance>(balanceQuery.queryKey);
            if (prev?.auto_reload) {
                const ar = { ...prev.auto_reload, ...patch };
                if (patch.enabled) ar.failure_code = null;
                qc.setQueryData<Balance>(balanceQuery.queryKey, { ...prev, auto_reload: ar });
            }
            return { prev };
        },
        onError: (_e, _patch, ctx) => {
            if (ctx?.prev) qc.setQueryData(balanceQuery.queryKey, ctx.prev);
        },
        onSuccess: (next) => {
            qc.setQueryData(balanceQuery.queryKey, next);
        },
        onSettled: () => {
            void qc.invalidateQueries({ queryKey: ["subscription", "credits", "settings"] });
        },
    });
}
