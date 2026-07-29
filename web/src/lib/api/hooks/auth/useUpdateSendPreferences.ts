import { useMutation, useQueryClient } from "@tanstack/react-query";
import updateSendPreferences from "../../client/auth/updateSendPreferences";
import type User from "@/lib/api/models/auth/User";

// Saves the undo-send window and patches the cached /auth/me user in place
// instead of refetching: a stale server-side cache may briefly serve the
// user without the field, which would wipe the value we just saved.
export default function useUpdateSendPreferences() {
    const qc = useQueryClient();
    return useMutation({
        mutationFn: (undoSendSeconds: number) => updateSendPreferences(undoSendSeconds),
        onSuccess: (res) => {
            qc.setQueryData<User | null>(["auth", "me"], (old) =>
                old ? { ...old, undo_send_seconds: res.undo_send_seconds } : old,
            );
        },
    });
}
