// Stripe.js for the checkout's card field, fetched only when that field mounts, so no other page loads it.
import type { Stripe } from "@stripe/stripe-js";
import { loadStripe } from "@stripe/stripe-js/pure";

// A script that has not answered by then counts as blocked, and the checkout falls back to Stripe's page.
const LOAD_TIMEOUT_MS = 10_000;

const loads = new Map<string, Promise<Stripe | null>>();

/** Stripe.js for a publishable key, or null when it cannot load (blocked, offline, refused by an extension). */
export function stripeFor(publishableKey: string): Promise<Stripe | null> {
    const cached = loads.get(publishableKey);
    if (cached) return cached;
    const load = Promise.race([
        loadStripe(publishableKey).catch(() => null),
        new Promise<null>((resolve) => window.setTimeout(() => resolve(null), LOAD_TIMEOUT_MS)),
    ]);
    loads.set(publishableKey, load);
    // A failed load is tried again on the next visit to the page.
    void load.then((s) => {
        if (!s) loads.delete(publishableKey);
    });
    return load;
}
