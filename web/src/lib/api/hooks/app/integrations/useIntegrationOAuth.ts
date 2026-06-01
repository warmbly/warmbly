import { useMutation, useQueryClient } from "@tanstack/react-query";
import startIntegrationOAuth from "@/lib/api/client/app/integrations/startIntegrationOAuth";
import finishIntegrationOAuth from "@/lib/api/client/app/integrations/finishIntegrationOAuth";
import reauthIntegration from "@/lib/api/client/app/integrations/reauthIntegration";

// useStartIntegrationOAuth returns the provider authorization URL + state.
export function useStartIntegrationOAuth() {
    return useMutation({ mutationFn: startIntegrationOAuth });
}

// useFinishIntegrationOAuth completes the handshake and refreshes connections.
export function useFinishIntegrationOAuth() {
    const qc = useQueryClient();
    return useMutation({
        mutationFn: finishIntegrationOAuth,
        onSuccess: () => {
            qc.invalidateQueries({ queryKey: ["integrations", "connections"] });
            qc.invalidateQueries({ queryKey: ["integrations", "catalog"] });
        },
    });
}

// useReauthIntegration starts a fresh handshake for an existing connection.
export function useReauthIntegration() {
    return useMutation({ mutationFn: reauthIntegration });
}
