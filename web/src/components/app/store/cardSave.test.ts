import { describe, expect, it } from "vitest";
import type { Stripe, StripeElements } from "@stripe/stripe-js";
import { saveCard, type CardSaveApi } from "./cardSave";

function fakes(opts: { adoptFails?: boolean } = {}) {
    const calls: string[] = [];
    let releaseAdopt: () => void = () => {};
    const api: CardSaveApi = {
        createCardSetupIntent: async () => {
            calls.push("create");
            return { client_secret: "seti_1_secret" };
        },
        adoptCardSetup: (id) => {
            calls.push(`adopt:${id}`);
            if (opts.adoptFails) return Promise.reject(new Error("refused"));
            return new Promise<void>((resolve) => {
                releaseAdopt = resolve;
            });
        },
    };
    const stripe = {
        confirmSetup: async () => {
            calls.push("confirm");
            return { setupIntent: { id: "seti_1" } };
        },
    } as unknown as Pick<Stripe, "confirmSetup">;
    const elements = {
        submit: async () => {
            calls.push("submit");
            return {};
        },
    } as unknown as Pick<StripeElements, "submit">;
    return { calls, api, stripe, elements, release: () => releaseAdopt() };
}

describe("saveCard", () => {
    it("makes the confirmed card the default before reporting it saved, so the charge after uses it", async () => {
        const f = fakes();
        let settled = false;
        const saving = saveCard(f.stripe, f.elements, "https://app/return", f.api).then((r) => {
            settled = true;
            return r;
        });
        await new Promise((r) => setTimeout(r, 0));
        expect(f.calls).toEqual(["submit", "create", "confirm", "adopt:seti_1"]);
        // Nothing may charge while the default is still the old card.
        expect(settled).toBe(false);
        f.release();
        await expect(saving).resolves.toEqual({ ok: true });
    });

    it("reports a failure when the card could not be made the default, so nothing is charged", async () => {
        const f = fakes({ adoptFails: true });
        const r = await saveCard(f.stripe, f.elements, "https://app/return", f.api);
        expect(r.ok).toBe(false);
    });
});
