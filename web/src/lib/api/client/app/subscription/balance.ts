import type {
    AutoReloadPatch,
    Balance,
    BalanceInvoice,
    TopUpResult,
} from "@/lib/api/models/app/subscription/Credits";
import Request from "../../Request";

export async function getBalance(signal?: AbortSignal): Promise<Balance> {
    return await Request<Balance>({ method: "GET", url: "/subscription/balance", authorization: true, signal });
}

// Charges the saved card now. 402 means the card refused: carry on in Checkout.
export async function topUpBalance(amount_usd: number): Promise<TopUpResult> {
    return await Request<TopUpResult>({ method: "POST", url: "/subscription/balance/top-up", data: { amount_usd }, authorization: true });
}

export async function createBalanceCheckout(amount_usd: number): Promise<{ checkout_url: string }> {
    return await Request<{ checkout_url: string }>({ method: "POST", url: "/subscription/balance/checkout", data: { amount_usd }, authorization: true });
}

// Stripe Checkout in setup mode, to save or replace the card.
export async function createPaymentMethodSetup(): Promise<{ checkout_url: string }> {
    return await Request<{ checkout_url: string }>({ method: "POST", url: "/subscription/balance/payment-method", authorization: true });
}

export async function updateAutoReload(patch: AutoReloadPatch): Promise<Balance> {
    return await Request<Balance>({ method: "PATCH", url: "/subscription/balance/auto-reload", data: patch, authorization: true });
}

export async function listBalanceInvoices(signal?: AbortSignal): Promise<{ data: BalanceInvoice[] }> {
    return await Request<{ data: BalanceInvoice[] }>({ method: "GET", url: "/subscription/balance/invoices", authorization: true, signal });
}
