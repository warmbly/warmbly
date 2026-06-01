import { useMutation, useQueryClient } from "@tanstack/react-query";
import createConnectionEvent from "@/lib/api/client/app/integrations/createConnectionEvent";
import deleteConnectionEvent from "@/lib/api/client/app/integrations/deleteConnectionEvent";

export function useCreateConnectionEvent() {
    const qc = useQueryClient();
    return useMutation({
        mutationFn: createConnectionEvent,
        onSuccess: (_data, vars) => {
            qc.invalidateQueries({ queryKey: ["integrations", "connection", vars.connectionId] });
            qc.invalidateQueries({ queryKey: ["integrations", "connections"] });
        },
    });
}

export function useDeleteConnectionEvent() {
    const qc = useQueryClient();
    return useMutation({
        mutationFn: deleteConnectionEvent,
        onSuccess: (_data, vars) => {
            qc.invalidateQueries({ queryKey: ["integrations", "connection", vars.connectionId] });
        },
    });
}
