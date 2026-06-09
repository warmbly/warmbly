import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import listFieldMappings from "@/lib/api/client/app/integrations/listFieldMappings";
import replaceFieldMappings from "@/lib/api/client/app/integrations/replaceFieldMappings";
import updateConnectionConfig from "@/lib/api/client/app/integrations/updateConnectionConfig";

export function useFieldMappings(connectionId: string) {
    return useQuery({
        queryKey: ["integrations", "field-mappings", connectionId],
        queryFn: () => listFieldMappings(connectionId),
        enabled: !!connectionId,
        staleTime: 10_000,
    });
}

export function useReplaceFieldMappings() {
    const qc = useQueryClient();
    return useMutation({
        mutationFn: replaceFieldMappings,
        onSuccess: (_data, vars) => {
            qc.invalidateQueries({ queryKey: ["integrations", "field-mappings", vars.connectionId] });
            qc.invalidateQueries({ queryKey: ["integrations", "connection", vars.connectionId] });
        },
    });
}

export function useUpdateConnectionConfig() {
    const qc = useQueryClient();
    return useMutation({
        mutationFn: updateConnectionConfig,
        onSuccess: (_data, vars) => {
            qc.invalidateQueries({ queryKey: ["integrations", "connection", vars.connectionId] });
            qc.invalidateQueries({ queryKey: ["integrations", "connections"] });
        },
    });
}
