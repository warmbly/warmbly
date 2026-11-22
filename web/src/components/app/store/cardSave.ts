// Saving a card typed into the page: confirm a SetupIntent with Stripe.js, then make that card the workspace's
// default before anything is charged, so a replaced card is never the one billed.
import type { Stripe, StripeElements } from "@stripe/stripe-js";
import { adoptCardSetup, createCardSetupIntent } from "@/lib/api/client/app/subscription/balance";
import { storeErrorMessage } from "@/lib/buy/errors";

export type CardSaveResult = { ok: true } | { ok: false; message: string | null };

export interface CardSaveApi {
    createCardSetupIntent: () => Promise<{ client_secret: string }>;
    adoptCardSetup: (setupIntentId: string) => Promise<void>;
}

const defaultApi: CardSaveApi = { createCardSetupIntent, adoptCardSetup };

export async function saveCard(
    stripe: Pick<Stripe, "confirmSetup">,
    elements: Pick<StripeElements, "submit">,
    returnUrl: string,
    api: CardSaveApi = defaultApi,
): Promise<CardSaveResult> {
    // The field marks its own problems; nothing more to say here.
    const submitted = await elements.submit();
    if (submitted.error) return { ok: false, message: null };
    let secret: string;
    try {
        secret = (await api.createCardSetupIntent()).client_secret;
    } catch (e) {
        return { ok: false, message: storeErrorMessage(e) };
    }
    const { error, setupIntent } = await stripe.confirmSetup({
        elements: elements as StripeElements,
        clientSecret: secret,
        redirect: "if_required",
        confirmParams: { return_url: returnUrl },
    });
    if (error) {
        // Stripe writes card refusals for the person paying; anything else gets our own sentence.
        const forPayer = error.type === "card_error" || error.type === "validation_error";
        return { ok: false, message: forPayer && error.message ? error.message : "The card could not be saved, so nothing was charged. Try again in a moment." };
    }
    if (!setupIntent?.id) return { ok: false, message: "The card could not be saved, so nothing was charged. Try again in a moment." };
    try {
        await api.adoptCardSetup(setupIntent.id);
    } catch (e) {
        return { ok: false, message: storeErrorMessage(e) };
    }
    return { ok: true };
}
