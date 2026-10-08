import { describe, expect, it } from "vitest";
import { isNoise } from "./posthog";

const exception = (type: string, value: string) => ({
    $exception_list: [{ type, value, mechanism: { handled: false, synthetic: false } }],
});

describe("isNoise", () => {
    it("drops posthog-js's own request timeout", () => {
        expect(isNoise(exception("AbortError", "PostHog request timed out after 3000ms"))).toBe(true);
    });

    it("drops the flattened form of the same timeout", () => {
        expect(isNoise({
            $exception_types: ["AbortError"],
            $exception_values: ["PostHog request timed out after 3000ms"],
        })).toBe(true);
    });

    it("keeps an app AbortError", () => {
        expect(isNoise(exception("AbortError", "signal is aborted without reason"))).toBe(false);
    });

    it("keeps a non-abort error that mentions the timeout", () => {
        expect(isNoise(exception("Error", "PostHog request timed out after 3000ms"))).toBe(false);
    });

    it("drops a passkey ceremony cancelled by leaving the page", () => {
        expect(isNoise(exception("PasskeyCancelled", "Passkey request cancelled"))).toBe(true);
    });

    it.each([
        ["DOMException", "The authenticator was previously registered: InvalidStateError"],
        ["DOMException", "InvalidStateError: The authenticator was previously registered"],
        ["InvalidStateError", "The authenticator was previously registered"],
        ["PasskeyAlreadyRegistered", "This device already has a passkey for your account."],
    ])("drops expected duplicate passkey registration (%s: %s)", (type, value) => {
        expect(isNoise(exception(type, value))).toBe(true);
        expect(isNoise({ $exception_type: type, $exception_message: value })).toBe(true);
        expect(isNoise({ $exception_types: [type], $exception_values: [value] })).toBe(true);
    });

    it.each([
        ["DOMException", "InvalidStateError: An operation is already pending"],
        ["DOMException", "NotFoundError: The object can not be found here."],
        ["DOMException", "SecurityError: Invalid relying party"],
        ["Error", "The authenticator was previously registered: InvalidStateError"],
        ["DOMException", "The authenticator was previously registered: InvalidStateError. Verification failed"],
    ])("keeps other failures (%s: %s)", (type, value) => {
        expect(isNoise(exception(type, value))).toBe(false);
        expect(isNoise({ $exception_type: type, $exception_message: value })).toBe(false);
        expect(isNoise({ $exception_types: [type], $exception_values: [value] })).toBe(false);
    });

    it("keeps genuine failures with a duplicate registration cause", () => {
        const entries = [
            ...exception("Error", "Verification failed").$exception_list,
            ...exception("DOMException", "The authenticator was previously registered: InvalidStateError").$exception_list,
        ];
        expect(isNoise({ $exception_list: entries })).toBe(false);
        expect(isNoise({ $exception_types: entries.map((e) => e.type), $exception_values: entries.map((e) => e.value) })).toBe(false);
    });
});
