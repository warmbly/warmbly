import { API_BASE_URL } from "@/lib/information";
import type PasskeyLoginBegin from "@/lib/api/models/auth/PasskeyLoginBegin";

export default async function passkeyLoginBegin(): Promise<PasskeyLoginBegin> {
    // Keep this as a direct fetch instead of the shared axios wrapper. Safari's
    // WebAuthn user-gesture detection is sensitive to async wrapper layers
    // before startAuthentication().
    const response = await fetch(`${API_BASE_URL}/auth/passkey/login/begin`, {
        method: "POST",
        headers: { "Content-Type": "application/json" },
    });

    if (!response.ok) {
        let message = "Couldn't start passkey sign-in.";
        try {
            const body = await response.json() as { message?: string; error?: string };
            message = body.message || body.error || message;
        } catch {
            /* keep fallback */
        }
        throw new Error(message);
    }

    return await response.json() as PasskeyLoginBegin;
}
