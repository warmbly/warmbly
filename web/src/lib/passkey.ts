// Thin wrapper over @simplewebauthn/browser that pairs the browser ceremonies
// with our Go (go-webauthn) backend. The server wraps options in a `publicKey`
// envelope, which the browser lib wants unwrapped — we do that here so callers
// never touch the envelope.
import {
    startAuthentication,
    startRegistration,
    browserSupportsWebAuthn,
    browserSupportsWebAuthnAutofill,
    platformAuthenticatorIsAvailable,
    WebAuthnAbortService,
    WebAuthnError,
} from "@simplewebauthn/browser";

import passkeyLoginBegin from "@/lib/api/client/auth/passkey/loginBegin";
import passkeyLoginFinish from "@/lib/api/client/auth/passkey/loginFinish";
import passkeyRegisterBegin from "@/lib/api/client/auth/passkey/registerBegin";
import passkeyRegisterFinish from "@/lib/api/client/auth/passkey/registerFinish";
import type Token from "@/lib/api/models/auth/Token";
import type Passkey from "@/lib/api/models/auth/Passkey";
import type PasskeyLoginBegin from "@/lib/api/models/auth/PasskeyLoginBegin";

// sessionStorage flag set after a password sign-in to trigger a one-time
// "set up a passkey" nudge once the dashboard loads (FIDO guidance: prompt
// enrollment after sign-in, not mid-flow).
export const SUGGEST_PASSKEY_FLAG = "warmbly:suggest-passkey";

export const passkeySupported = (): boolean => browserSupportsWebAuthn();
export const passkeyAutofillSupported = (): Promise<boolean> => browserSupportsWebAuthnAutofill();
export const platformPasskeyAvailable = (): Promise<boolean> => platformAuthenticatorIsAvailable();
export type PasskeyLoginChallenge = PasskeyLoginBegin;

/**
 * Thrown when a ceremony doesn't complete. `reason` lets callers react:
 * - "aborted": we cancelled it (e.g. conditional autofill superseded) — always silent.
 * - "not-allowed": the user dismissed the OS dialog OR no passkey exists on the
 *   device (the spec deliberately makes these indistinguishable). The explicit
 *   button uses this to nudge enrollment; the autofill path stays silent.
 */
export class PasskeyCancelled extends Error {
    reason: "aborted" | "not-allowed";
    constructor(reason: "aborted" | "not-allowed" = "aborted") {
        super("Passkey request cancelled");
        this.name = "PasskeyCancelled";
        this.reason = reason;
    }
}

function mapError(e: unknown): Error {
    if (e instanceof WebAuthnError) {
        switch (e.code) {
            // Control flow, not failures.
            case "ERROR_CEREMONY_ABORTED":
                return new PasskeyCancelled("aborted");
            case "ERROR_PASSTHROUGH_SEE_CAUSE_PROPERTY":
                return new PasskeyCancelled("not-allowed");
            case "ERROR_AUTHENTICATOR_PREVIOUSLY_REGISTERED":
                return new Error("This device already has a passkey for your account.");
            case "ERROR_AUTHENTICATOR_MISSING_DISCOVERABLE_CREDENTIAL_SUPPORT":
            case "ERROR_AUTHENTICATOR_MISSING_USER_VERIFICATION_SUPPORT":
                return new Error("This device can't create a passkey that meets our requirements.");
            default:
                return new Error("Your device couldn't complete the passkey request.");
        }
    }
    if (e instanceof Error) return e;
    return new Error("Something went wrong with the passkey request.");
}

/**
 * Start a server-side login challenge. Safari can lose WebAuthn's required
 * user activation if this network request happens after the click, so explicit
 * button flows should prefetch it before calling startAuthentication().
 */
export async function beginPasskeyLogin(): Promise<PasskeyLoginChallenge> {
    return await passkeyLoginBegin();
}

/**
 * Complete a discoverable passkey sign-in with an existing challenge. With
 * `conditional: true` it drives the browser autofill UI (no modal) and stays
 * pending until the user picks a passkey from the field's dropdown.
 */
export async function finishPasskeyLogin(
    challenge: PasskeyLoginChallenge,
    opts?: { conditional?: boolean },
): Promise<Token> {
    try {
        const credential = await startAuthentication({
            optionsJSON: challenge.options.publicKey,
            useBrowserAutofill: opts?.conditional ?? false,
        });
        return await passkeyLoginFinish({ session: challenge.session, credential });
    } catch (e) {
        throw mapError(e);
    }
}

/** Run a discoverable passkey sign-in from scratch. */
export async function passkeyLogin(opts?: { conditional?: boolean }): Promise<Token> {
    return await finishPasskeyLogin(await beginPasskeyLogin(), opts);
}

/** Enroll a new passkey for the signed-in user. */
export async function registerPasskey(name?: string): Promise<Passkey> {
    try {
        const options = await passkeyRegisterBegin();
        const credential = await startRegistration({ optionsJSON: options.publicKey });
        return await passkeyRegisterFinish({ name, credential });
    } catch (e) {
        throw mapError(e);
    }
}

/** Tear down a pending conditional-UI request (e.g. on route change). */
export function cancelPasskeyCeremony(): void {
    try {
        WebAuthnAbortService.cancelCeremony();
    } catch {
        /* no active ceremony */
    }
}

export { WebAuthnError };
