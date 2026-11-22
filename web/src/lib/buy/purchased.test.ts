import { describe, expect, it } from "vitest";
import { storeErrorMessage } from "./errors";
import { PURCHASED_LOCKED, purchaseState, storeMailboxEnded } from "./purchased";

describe("purchaseState", () => {
    it("tells a connected mailbox, a live purchase and an ended one apart", () => {
        expect(purchaseState({}).kind).toBe("none");
        expect(purchaseState({ purchased: true }).kind).toBe("live");
        expect(purchaseState({ purchased: true, purchase_cancelled_at: null }).kind).toBe("live");
        const ended = purchaseState({ purchased: true, purchase_cancelled_at: "2026-12-16T10:00:00Z" });
        expect(ended.kind).toBe("cancelled");
        expect(ended.kind === "cancelled" && ended.on).toMatch(/2026/);
    });

    it("reads cancelled and failed as ended, cancelling as still live", () => {
        expect(storeMailboxEnded("cancelled")).toBe(true);
        expect(storeMailboxEnded("failed")).toBe(true);
        expect(storeMailboxEnded("cancelling")).toBe(false);
        expect(storeMailboxEnded("active")).toBe(false);
    });

    it("explains the purchased_mailbox refusal in Warmbly's words", () => {
        expect(storeErrorMessage({ status: 403, code: "purchased_mailbox", message: "raw", error: "Forbidden" })).toBe(PURCHASED_LOCKED);
    });
});
