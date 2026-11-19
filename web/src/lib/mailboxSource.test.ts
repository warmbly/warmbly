import { describe, expect, it } from "vitest";
import { mailboxSource } from "./mailboxSource";

describe("mailboxSource", () => {
    it("names a mailbox bought in the store before how it signs in", () => {
        const s = mailboxSource({ provider: "gmail", mail_host: "google_workspace", auth_method: "delegated", vendor: undefined, purchased: true });
        expect(s).toMatchObject({ kind: "purchased", label: "Purchased", logo: "google" });
    });

    it("keeps the grant chip for a delegated mailbox that was not bought here", () => {
        expect(mailboxSource({ provider: "outlook", mail_host: "microsoft365", auth_method: "delegated", vendor: undefined }).kind).toBe("grant");
    });
});
