import { describe, expect, it } from "vitest";
import { verificationSourceLabel, verificationTitle } from "./VerificationBadge";

describe("verification provider attribution", () => {
    it.each(["CleanMyList", "MailVeri", "MillionVerifier"])("names %s on provider verdicts", (name) => {
        expect(verificationTitle({
            verification_status: "valid",
            verification_source: "provider",
            verification_provider: name.toLowerCase(),
        })).toBe(`Deliverable · verified with ${name}`);
    });

    it("keeps the generic label for a verifier it has no name for", () => {
        expect(verificationTitle({
            verification_status: "valid",
            verification_source: "provider",
            verification_provider: "some-service",
        })).toBe("Deliverable · verified with a verification service");
    });

    it("says when a re-check is waiting on the current verdict", () => {
        expect(verificationTitle({
            verification_status: "invalid",
            verification_source: "provider",
            verification_provider: "millionverifier",
            verification_requested_at: new Date("2026-09-22T10:00:00Z"),
        })).toBe("Undeliverable · verified with MillionVerifier · re-check queued");
    });

    it("names the built-in check and an imported vocabulary", () => {
        expect(verificationSourceLabel("probe", "builtin")).toBe("checked by Warmbly's built-in check");
        expect(verificationSourceLabel("imported", "zerobounce", "ZeroBounce")).toBe("imported from ZeroBounce");
        expect(verificationSourceLabel("", "")).toBe("");
    });
});
