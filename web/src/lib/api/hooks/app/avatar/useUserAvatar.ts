import { useMutation, useQueryClient } from "@tanstack/react-query";
import {
    deleteUserAvatar,
    uploadUserAvatar,
} from "@/lib/api/client/app/avatar/uploadUserAvatar";

export function useUploadUserAvatar() {
    const qc = useQueryClient();
    return useMutation({
        mutationFn: (blob: Blob) => uploadUserAvatar(blob),
        onSuccess: () => {
            qc.invalidateQueries({ queryKey: ["auth", "me"] });
        },
    });
}

export function useDeleteUserAvatar() {
    const qc = useQueryClient();
    return useMutation({
        mutationFn: () => deleteUserAvatar(),
        onSuccess: () => {
            qc.invalidateQueries({ queryKey: ["auth", "me"] });
        },
    });
}
