import type { PublicKeyCredentialCreationOptionsJSON } from "@simplewebauthn/browser";

// Response from POST /auth/passkey/register/begin — the go-webauthn
// CredentialCreation envelope. The browser consumes `.publicKey`.
export default interface PasskeyRegisterBegin {
    publicKey: PublicKeyCredentialCreationOptionsJSON;
    mediation?: string;
}
