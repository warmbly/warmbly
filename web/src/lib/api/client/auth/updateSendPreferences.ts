import Request from "../Request";

export interface SendPreferencesResponse {
    undo_send_seconds: number;
}

// Persists the undo-send window (5..120 seconds). The current value rides
// the /auth/me user object as undo_send_seconds.
export default async function updateSendPreferences(
    undoSendSeconds: number,
): Promise<SendPreferencesResponse> {
    return await Request<SendPreferencesResponse>({
        method: "PUT",
        url: "/auth/me/send-preferences",
        data: { undo_send_seconds: undoSendSeconds },
        authorization: true,
    });
}
