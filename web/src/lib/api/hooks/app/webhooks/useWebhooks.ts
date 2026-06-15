import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";

import listWebhooks from "@/lib/api/client/app/webhooks/listWebhooks";
import createWebhook from "@/lib/api/client/app/webhooks/createWebhook";
import updateWebhook from "@/lib/api/client/app/webhooks/updateWebhook";
import deleteWebhook from "@/lib/api/client/app/webhooks/deleteWebhook";
import rotateWebhookSecret from "@/lib/api/client/app/webhooks/rotateWebhookSecret";
import verifyWebhook from "@/lib/api/client/app/webhooks/verifyWebhook";
import listWebhookDeliveries from "@/lib/api/client/app/webhooks/listWebhookDeliveries";
import redeliverWebhookDelivery from "@/lib/api/client/app/webhooks/redeliverWebhookDelivery";
import listWebhookEventTypes from "@/lib/api/client/app/webhooks/listWebhookEventTypes";
import listWebhookDrops from "@/lib/api/client/app/webhooks/listWebhookDrops";
import type {
    WebhookDeliveriesQuery,
    WebhookEndpointInput,
} from "@/lib/api/models/app/webhooks/Webhook";

export function useWebhooks() {
    return useQuery({
        queryKey: ["webhooks", "list"],
        queryFn: () => listWebhooks(),
        staleTime: 5_000,
    });
}

export function useCreateWebhook() {
    const qc = useQueryClient();
    return useMutation({
        mutationFn: (data: WebhookEndpointInput) => createWebhook(data),
        onSuccess: () => void qc.invalidateQueries({ queryKey: ["webhooks"] }),
    });
}

export function useUpdateWebhook() {
    const qc = useQueryClient();
    return useMutation({
        mutationFn: ({ id, data }: { id: string; data: WebhookEndpointInput }) => updateWebhook(id, data),
        onSuccess: () => void qc.invalidateQueries({ queryKey: ["webhooks"] }),
    });
}

export function useDeleteWebhook() {
    const qc = useQueryClient();
    return useMutation({
        mutationFn: (id: string) => deleteWebhook(id),
        onSuccess: () => void qc.invalidateQueries({ queryKey: ["webhooks"] }),
    });
}

export function useRotateWebhookSecret() {
    return useMutation({
        mutationFn: (id: string) => rotateWebhookSecret(id),
    });
}

// Verify doubles as "send a test event"; the endpoint verifies on a 2xx so its
// state changes after the delivery lands — refresh the list + delivery log.
export function useVerifyWebhook() {
    const qc = useQueryClient();
    return useMutation({
        mutationFn: (id: string) => verifyWebhook(id),
        onSuccess: () => {
            void qc.invalidateQueries({ queryKey: ["webhooks", "list"] });
            void qc.invalidateQueries({ queryKey: ["webhooks", "deliveries"] });
        },
    });
}

export function useWebhookDeliveries(query: WebhookDeliveriesQuery = {}) {
    const { endpointId, status, eventType, limit } = query;
    return useQuery({
        queryKey: [
            "webhooks",
            "deliveries",
            endpointId ?? "all",
            { status: status || "", eventType: eventType || "", limit: limit ?? 0 },
        ],
        queryFn: () => listWebhookDeliveries(query),
        staleTime: 5_000,
    });
}

export function useRedeliverDelivery() {
    const qc = useQueryClient();
    return useMutation({
        mutationFn: (deliveryId: string) => redeliverWebhookDelivery(deliveryId),
        onSuccess: () => void qc.invalidateQueries({ queryKey: ["webhooks", "deliveries"] }),
    });
}

export function useWebhookEventCatalog() {
    return useQuery({
        queryKey: ["webhooks", "event-types"],
        queryFn: () => listWebhookEventTypes(),
        staleTime: 5 * 60_000,
    });
}

export function useWebhookDrops() {
    return useQuery({
        queryKey: ["webhooks", "drops"],
        queryFn: () => listWebhookDrops(),
        staleTime: 30_000,
    });
}
