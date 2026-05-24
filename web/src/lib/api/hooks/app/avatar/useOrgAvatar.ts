import { useMutation, useQueryClient } from "@tanstack/react-query";
import {
    deleteOrganizationAvatar,
    uploadOrganizationAvatar,
} from "@/lib/api/client/app/avatar/uploadUserAvatar";

export function useUploadOrgAvatar() {
    const qc = useQueryClient();
    return useMutation({
        mutationFn: (blob: Blob) => uploadOrganizationAvatar(blob),
        onSuccess: () => {
            qc.invalidateQueries({ queryKey: ["organizations"] });
            qc.invalidateQueries({ queryKey: ["organizations", "current"] });
        },
    });
}

export function useDeleteOrgAvatar() {
    const qc = useQueryClient();
    return useMutation({
        mutationFn: () => deleteOrganizationAvatar(),
        onSuccess: () => {
            qc.invalidateQueries({ queryKey: ["organizations"] });
            qc.invalidateQueries({ queryKey: ["organizations", "current"] });
        },
    });
}
