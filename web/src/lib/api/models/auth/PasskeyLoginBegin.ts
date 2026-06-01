import type { PublicKeyCredentialRequestOptionsJSON } from "@simplewebauthn/browser";

// Response from POST /auth/passkey/login/begin. `options` is the go-webauthn
// CredentialAssertion envelope; the browser consumes `options.publicKey`.
export default interface PasskeyLoginBegin {
    session: string;
    options: {
        publicKey: PublicKeyCredentialRequestOptionsJSON;
        mediation?: string;
    };
}
