import { useMutation, useQueryClient } from "@tanstack/react-query";
import connectIntegration, { type ConnectInput } from "@/lib/api/client/app/integrations/connectIntegration";

export default function useConnectIntegration() {
    const qc = useQueryClient();
    return useMutation({
        mutationFn: (input: ConnectInput) => connectIntegration(input),
        onSuccess: () => {
            qc.invalidateQueries({ queryKey: ["integrations", "connections"] });
        },
    });
}
