// Button and label classes the tour's panels share.

export const PRIMARY =
    "h-10 px-4 rounded-full inline-flex items-center justify-center gap-1.5 bg-slate-900 text-[13.5px] font-medium text-white transition-colors hover:bg-slate-800 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-sky-400 focus-visible:ring-offset-2";
export const SECONDARY =
    "h-10 px-4 rounded-full inline-flex items-center justify-center gap-1.5 bg-white text-[13.5px] font-medium text-slate-800 ring-1 ring-slate-200 transition-colors hover:bg-slate-50 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-sky-400";
export const EYEBROW = "text-[10.5px] uppercase tracking-[0.14em] text-slate-400 font-medium";

/** Dev only: `?tour=billing` plays the tour as the owner of an unpaid, billed workspace, `?tour=billing-paid` as one on Grow. Checkout is stubbed. */
export function billingPreview(): "free" | "paid" | null {
    if (!import.meta.env.DEV) return null;
    const v = new URLSearchParams(window.location.search).get("tour");
    return v === "billing" ? "free" : v === "billing-paid" ? "paid" : null;
}

/** Where a self-hosted member signs up for a hosted plan; the same links the pricing page uses. */
export function hostedPlanURL(id: string): string {
    return id === "enterprise" ? "https://warmbly.com/contact/?topic=sales" : `https://app.warmbly.com/auth/register?plan=${id}`;
}

export const CLOUD_DOCS = "https://docs.warmbly.com/guides/warmbly-cloud/";
