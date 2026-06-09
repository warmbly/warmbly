import { useMutation } from "@tanstack/react-query";
import getWebhookSecret from "@/lib/api/client/app/integrations/getWebhookSecret";
import testConnection from "@/lib/api/client/app/integrations/testConnection";

// Reveal (and lazily generate) the connection's outbound-webhook signing secret.
export function useRevealWebhookSecret() {
    return useMutation({ mutationFn: getWebhookSecret });
}

// Fire a synthetic event through the connection's configured automations.
export function useTestConnection() {
    return useMutation({ mutationFn: testConnection });
}
