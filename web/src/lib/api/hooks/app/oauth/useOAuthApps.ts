import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";

import listOAuthApps from "@/lib/api/client/app/oauth/listOAuthApps";
import createOAuthApp from "@/lib/api/client/app/oauth/createOAuthApp";
import updateOAuthApp from "@/lib/api/client/app/oauth/updateOAuthApp";
import deleteOAuthApp from "@/lib/api/client/app/oauth/deleteOAuthApp";
import rotateOAuthAppSecret from "@/lib/api/client/app/oauth/rotateOAuthAppSecret";
import uploadOAuthAppLogo from "@/lib/api/client/app/oauth/uploadOAuthAppLogo";
import type { OAuthApplicationInput } from "@/lib/api/models/app/oauth/OAuthApp";

export function useOAuthApps() {
    return useQuery({
        queryKey: ["oauth-apps", "list"],
        queryFn: () => listOAuthApps(),
        staleTime: 5_000,
    });
}

export function useCreateOAuthApp() {
    const qc = useQueryClient();
    return useMutation({
        mutationFn: (data: OAuthApplicationInput) => createOAuthApp(data),
        onSuccess: () => void qc.invalidateQueries({ queryKey: ["oauth-apps"] }),
    });
}

export function useUpdateOAuthApp() {
    const qc = useQueryClient();
    return useMutation({
        mutationFn: ({ id, data }: { id: string; data: OAuthApplicationInput }) => updateOAuthApp(id, data),
        onSuccess: () => void qc.invalidateQueries({ queryKey: ["oauth-apps"] }),
    });
}

export function useDeleteOAuthApp() {
    const qc = useQueryClient();
    return useMutation({
        mutationFn: (id: string) => deleteOAuthApp(id),
        onSuccess: () => void qc.invalidateQueries({ queryKey: ["oauth-apps"] }),
    });
}

export function useRotateOAuthAppSecret() {
    return useMutation({
        mutationFn: (id: string) => rotateOAuthAppSecret(id),
    });
}

export function useUploadOAuthAppLogo() {
    return useMutation({
        mutationFn: (blob: Blob) => uploadOAuthAppLogo(blob),
    });
}
