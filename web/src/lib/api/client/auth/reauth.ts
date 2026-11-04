import Request from "../Request";

interface ReauthBody {
    password?: string;
    code?: string;
}

interface ReauthResponse {
    valid_for_seconds: number;
}

// Re-prove the account holder behind the current session. The backend stamps
// the session, and the actions that require a fresh check accept it for the
// window it returns.
export default function reauth(body: ReauthBody) {
    return Request<ReauthResponse>({
        method: "post",
        url: "/auth/reauth",
        data: body,
        authorization: true,
        // Never retried through the reauth prompt: this IS the prompt, and a
        // failure here must reach the confirmation dialog.
        skipReauthPrompt: true,
    });
}
