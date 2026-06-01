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
    base64URLStringToBuffer,
    bufferToBase64URLString,
    type AuthenticationResponseJSON,
    type PublicKeyCredentialRequestOptionsJSON,
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

export function safariNeedsExplicitPasskeyGesture(): boolean {
    if (typeof navigator === "undefined") return false;
    const ua = navigator.userAgent;
    return /Safari/i.test(ua) && !/Chrome|Chromium|CriOS|FxiOS|Edg/i.test(ua);
}

/**
 * Thrown when a ceremony doesn't complete. `reason` lets callers react:
 * - "aborted": we cancelled it (e.g. conditional autofill superseded) — always silent.
 * - "not-allowed": the user dismissed the OS dialog OR no passkey exists on the
 *   device (the spec deliberately makes these indistinguishable). The explicit
 *   button uses this to nudge enrollment; the autofill path stays silent.
 */
export class PasskeyCancelled extends Error {
    reason: "aborted" | "not-allowed" | "timeout";
    constructor(reason: "aborted" | "not-allowed" | "timeout" = "aborted") {
        super("Passkey request cancelled");
        this.name = "PasskeyCancelled";
        this.reason = reason;
    }
}

function mapError(e: unknown): Error {
    if (e instanceof PasskeyCancelled) return e;
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
    if (e instanceof DOMException) {
        switch (e.name) {
            case "AbortError":
                return new PasskeyCancelled("aborted");
            case "NotAllowedError":
                return new PasskeyCancelled("not-allowed");
            default:
                return new Error(e.message || "Your device couldn't complete the passkey request.");
        }
    }
    if (e instanceof Error) return e;
    return new Error("Something went wrong with the passkey request.");
}

function toPublicKeyCredentialRequestOptions(
    options: PublicKeyCredentialRequestOptionsJSON,
    timeoutMs?: number,
): PublicKeyCredentialRequestOptions {
    return {
        ...options,
        challenge: base64URLStringToBuffer(options.challenge),
        timeout: timeoutMs ?? options.timeout,
        allowCredentials: options.allowCredentials?.length
            ? options.allowCredentials.map((credential) => ({
                ...credential,
                id: base64URLStringToBuffer(credential.id),
                transports: credential.transports as AuthenticatorTransport[] | undefined,
            }))
            : undefined,
    };
}

function toAuthenticatorAttachment(attachment: string | null): AuthenticatorAttachment | undefined {
    return attachment === "platform" || attachment === "cross-platform" ? attachment : undefined;
}

async function startExplicitAuthentication(
    optionsJSON: PublicKeyCredentialRequestOptionsJSON,
    timeoutMs: number,
): Promise<AuthenticationResponseJSON> {
    if (!browserSupportsWebAuthn()) {
        throw new Error("WebAuthn is not supported in this browser");
    }

    const credential = await navigator.credentials.get({
        mediation: "required",
        publicKey: toPublicKeyCredentialRequestOptions(optionsJSON, timeoutMs),
        signal: WebAuthnAbortService.createNewAbortSignal(),
    }) as PublicKeyCredential | null;

    if (!credential) {
        throw new Error("Authentication was not completed");
    }

    const response = credential.response as AuthenticatorAssertionResponse;
    const userHandle = response.userHandle ? bufferToBase64URLString(response.userHandle) : undefined;

    return {
        id: credential.id,
        rawId: bufferToBase64URLString(credential.rawId),
        response: {
            authenticatorData: bufferToBase64URLString(response.authenticatorData),
            clientDataJSON: bufferToBase64URLString(response.clientDataJSON),
            signature: bufferToBase64URLString(response.signature),
            userHandle,
        },
        type: "public-key",
        clientExtensionResults: credential.getClientExtensionResults(),
        authenticatorAttachment: toAuthenticatorAttachment(credential.authenticatorAttachment),
    };
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
    const timeoutMs = opts?.conditional ? undefined : 12000;
    let timeout: ReturnType<typeof setTimeout> | undefined;

    try {
        const authentication = opts?.conditional
            ? startAuthentication({
                optionsJSON: challenge.options.publicKey,
                useBrowserAutofill: true,
            })
            : startExplicitAuthentication(challenge.options.publicKey, timeoutMs ?? 12000);

        const credential = timeoutMs
            ? await Promise.race([
                authentication,
                new Promise<never>((_, reject) => {
                    timeout = setTimeout(() => {
                        cancelPasskeyCeremony();
                        reject(new PasskeyCancelled("timeout"));
                    }, timeoutMs);
                }),
            ])
            : await authentication;

        return await passkeyLoginFinish({ session: challenge.session, credential });
    } catch (e) {
        throw mapError(e);
    } finally {
        if (timeout) clearTimeout(timeout);
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
