import { useMutation, useQueryClient } from "@tanstack/react-query";
import disconnectIntegration from "@/lib/api/client/app/integrations/disconnectIntegration";

export default function useDisconnectIntegration() {
    const qc = useQueryClient();
    return useMutation({
        mutationFn: (id: string) => disconnectIntegration(id),
        onSuccess: () => {
            qc.invalidateQueries({ queryKey: ["integrations", "connections"] });
        },
    });
}
