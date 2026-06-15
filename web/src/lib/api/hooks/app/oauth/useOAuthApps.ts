import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";

import listOAuthApps from "@/lib/api/client/app/oauth/listOAuthApps";
import createOAuthApp from "@/lib/api/client/app/oauth/createOAuthApp";
import updateOAuthApp from "@/lib/api/client/app/oauth/updateOAuthApp";
import deleteOAuthApp from "@/lib/api/client/app/oauth/deleteOAuthApp";
import rotateOAuthAppSecret from "@/lib/api/client/app/oauth/rotateOAuthAppSecret";
import uploadOAuthAppLogo from "@/lib/api/client/app/oauth/uploadOAuthAppLogo";
import getOAuthAppWebhookSecret from "@/lib/api/client/app/oauth/getOAuthAppWebhookSecret";
import rotateOAuthAppWebhookSecret from "@/lib/api/client/app/oauth/rotateOAuthAppWebhookSecret";
import listOAuthAppWebhookEndpoints from "@/lib/api/client/app/oauth/listOAuthAppWebhookEndpoints";
import listOAuthAppWebhookDeliveries from "@/lib/api/client/app/oauth/listOAuthAppWebhookDeliveries";
import type { OAuthApplicationInput } from "@/lib/api/models/app/oauth/OAuthApp";
import type { WebhookDeliveriesQuery } from "@/lib/api/models/app/webhooks/Webhook";

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

// Reveals the app's webhook signing secret; only fetched when `enabled` (the
// secret only exists once a webhook URL has been set).
export function useOAuthAppWebhookSecret(id: string, enabled: boolean) {
    return useQuery({
        queryKey: ["oauth-apps", id, "webhook-secret"],
        queryFn: () => getOAuthAppWebhookSecret(id),
        enabled,
        staleTime: 60_000,
    });
}

export function useRotateOAuthAppWebhookSecret() {
    const qc = useQueryClient();
    return useMutation({
        mutationFn: (id: string) => rotateOAuthAppWebhookSecret(id),
        onSuccess: (_data, id) =>
            void qc.invalidateQueries({ queryKey: ["oauth-apps", id, "webhook-secret"] }),
    });
}

export function useOAuthAppWebhookEndpoints(id: string, enabled: boolean) {
    return useQuery({
        queryKey: ["oauth-apps", id, "webhook-endpoints"],
        queryFn: () => listOAuthAppWebhookEndpoints(id),
        enabled,
        staleTime: 5_000,
    });
}

export function useOAuthAppWebhookDeliveries(
    id: string,
    query: Omit<WebhookDeliveriesQuery, "endpointId"> & { enabled?: boolean } = {},
) {
    const { status, eventType, limit, enabled = true } = query;
    return useQuery({
        queryKey: [
            "oauth-apps",
            id,
            "webhook-deliveries",
            { status: status || "", eventType: eventType || "", limit: limit ?? 0 },
        ],
        queryFn: () => listOAuthAppWebhookDeliveries(id, query),
        enabled,
        staleTime: 5_000,
    });
}
