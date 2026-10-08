import { afterEach, describe, expect, it, vi } from "vitest";
import { startAuthentication, startRegistration, WebAuthnError } from "@simplewebauthn/browser";
import registerBegin from "@/lib/api/client/auth/passkey/registerBegin";
import registerFinish from "@/lib/api/client/auth/passkey/registerFinish";
import loginFinish from "@/lib/api/client/auth/passkey/loginFinish";
import { finishPasskeyLogin, passkeyChallengeUnavailable, PasskeyCancelled, registerPasskey } from "./passkey";

vi.mock("@simplewebauthn/browser", async (importOriginal) => ({
    ...await importOriginal<typeof import("@simplewebauthn/browser")>(),
    startAuthentication: vi.fn(),
    startRegistration: vi.fn(),
}));
vi.mock("@/lib/api/client/auth/passkey/registerBegin", () => ({ default: vi.fn() }));
vi.mock("@/lib/api/client/auth/passkey/registerFinish", () => ({ default: vi.fn() }));
vi.mock("@/lib/api/client/auth/passkey/loginFinish", () => ({ default: vi.fn() }));

afterEach(() => {
    document.body.innerHTML = "";
    vi.clearAllMocks();
});

describe("passkeyChallengeUnavailable", () => {
    it("treats a throttled address as something waiting fixes", () => {
        // The sign-in page asks for a challenge on load, so a shared address
        // can be refused one without anything being wrong.
        const throttled = Object.assign(new Error("Too many passkey sign-in requests from this address."), {
            status: 429,
            code: "rate_limit_exceeded",
        });

        expect(passkeyChallengeUnavailable(throttled)).toBe(true);
    });

    it("treats a request that never got an answer the same way", () => {
        expect(passkeyChallengeUnavailable(new TypeError("Failed to fetch"))).toBe(true);
    });

    it("still reports a server that answered with a fault", () => {
        const broken = Object.assign(new Error("Something went wrong."), { status: 500 });

        expect(passkeyChallengeUnavailable(broken)).toBe(false);
    });

    it("still reports a refusal that is not a throttle", () => {
        const refused = Object.assign(new Error("Passkeys are not enabled here."), { status: 404 });

        expect(passkeyChallengeUnavailable(refused)).toBe(false);
    });
});

describe("passkey ceremony errors", () => {
    const options = {
        publicKey: {
            challenge: "dGVzdA",
            rp: { name: "Warmbly", id: "localhost" },
            user: { id: "dXNlcg", name: "test@example.com", displayName: "Test" },
            pubKeyCredParams: [{ type: "public-key" as const, alg: -7 }],
        },
    };

    it.each([
        new DOMException("This authenticator already has a credential", "InvalidStateError"),
        new WebAuthnError({ message: "The authenticator was previously registered", code: "ERROR_AUTHENTICATOR_PREVIOUSLY_REGISTERED", cause: new DOMException("Already registered", "InvalidStateError") }),
        new WebAuthnError({ message: "Browser failure", code: "ERROR_PASSTHROUGH_SEE_CAUSE_PROPERTY", cause: new DOMException("Already registered", "InvalidStateError") }),
    ])("explains duplicate enrollment without finishing registration", async (error) => {
        vi.mocked(registerBegin).mockResolvedValue(options);
        vi.mocked(startRegistration).mockRejectedValue(error);

        await expect(registerPasskey()).rejects.toMatchObject({
            name: "PasskeyAlreadyRegistered",
            message: "This device already has a passkey for your account.",
        });
        expect(registerFinish).not.toHaveBeenCalled();
    });

    it("still treats a pending login ceremony as cancellation", async () => {
        document.body.innerHTML = '<input autocomplete="username webauthn" />';
        vi.mocked(startAuthentication).mockRejectedValue(new DOMException("Already pending", "InvalidStateError"));
        const challenge = { session: "test", options: { publicKey: { challenge: "dGVzdA", rpId: "localhost" } } };

        await expect(finishPasskeyLogin(challenge, { conditional: true })).rejects.toBeInstanceOf(PasskeyCancelled);
        expect(loginFinish).not.toHaveBeenCalled();
    });

    it("still treats dismissed registration as cancellation", async () => {
        vi.mocked(registerBegin).mockResolvedValue(options);
        vi.mocked(startRegistration).mockRejectedValue(new DOMException("Dismissed", "NotAllowedError"));

        await expect(registerPasskey()).rejects.toBeInstanceOf(PasskeyCancelled);
        expect(registerFinish).not.toHaveBeenCalled();
    });

    it("preserves genuine registration security failures", async () => {
        const error = new DOMException("Invalid relying party", "SecurityError");
        vi.mocked(registerBegin).mockResolvedValue(options);
        vi.mocked(startRegistration).mockRejectedValue(error);

        await expect(registerPasskey()).rejects.toBe(error);
        expect(registerFinish).not.toHaveBeenCalled();
    });
});
