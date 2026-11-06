import { describe, expect, it } from "vitest";

import safeNext, { postAuthNext } from "./safeNext";

describe("safeNext", () => {
    it("follows a path on this origin", () => {
        expect(safeNext("/cli?code=ABCD-EFGH", "/home")).toBe("/cli?code=ABCD-EFGH");
        expect(safeNext("/invite/abc", "/home")).toBe("/invite/abc");
    });

    it("refuses anything that can name another host", () => {
        for (const bad of ["//evil.example", "/\\evil.example", "/\\/evil.example", "https://evil.example", "evil", "/a\\b", "/\t/evil.example", "/x\ny", ""]) {
            expect(safeNext(bad, "/home")).toBe("/home");
        }
        expect(safeNext(null, "/home")).toBe("/home");
    });
});

describe("postAuthNext", () => {
    it("does not reopen a consumed invitation after either signup path", () => {
        for (const next of ["/invite?token=old", "/invite/?token=old", "/invite#accepted", "/auth/../invite?token=old"]) {
            expect(postAuthNext(next, true)).toBe("/app/emails");
        }
    });

    it("returns existing users to their still-pending invitation", () => {
        expect(postAuthNext("/invite?token=pending")).toBe("/invite?token=pending");
    });

    it("never sends a finished onboarding back to itself", () => {
        for (const next of ["/onboarding", "/onboarding/?next=/onboarding", "/auth/../onboarding", "/onboarding?next=https://evil.example"]) {
            expect(postAuthNext(next)).toBe("/app/emails");
        }
        expect(postAuthNext("/app/settings/billing?pool=1")).toBe("/app/settings/billing?pool=1");
    });

    it("keeps the destination onboarding was carrying when sign-in resumes it", () => {
        const billing = "/app/settings/billing?pool=1";
        expect(postAuthNext(`/onboarding?next=${encodeURIComponent(billing)}`)).toBe(billing);
        expect(postAuthNext(`/onboarding?next=${encodeURIComponent(`/onboarding?next=${encodeURIComponent(billing)}`)}`)).toBe(billing);
    });

    it("preserves other safe destinations and rejects external redirects", () => {
        expect(postAuthNext("/cli?code=ABCD-EFGH", true)).toBe("/cli?code=ABCD-EFGH");
        expect(postAuthNext("/app/emails", true)).toBe("/app/emails");
        expect(postAuthNext("/invites", true)).toBe("/invites");
        for (const next of [null, "//evil.example", "https://evil.example", "/\\evil.example"]) {
            expect(postAuthNext(next, true)).toBe("/app/emails");
        }
    });
});
