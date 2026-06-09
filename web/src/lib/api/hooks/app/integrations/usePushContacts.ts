import { useMutation, useQueryClient } from "@tanstack/react-query";
import pushContacts from "@/lib/api/client/app/integrations/pushContacts";

// Pushes selected contacts into a connected CRM on demand. Invalidates the
// connection's detail + the connections list so health / last-synced reflect
// the run.
export function usePushContacts() {
    const qc = useQueryClient();
    return useMutation({
        mutationFn: pushContacts,
        onSuccess: (_data, vars) => {
            qc.invalidateQueries({ queryKey: ["integrations", "connection", vars.connectionId] });
            qc.invalidateQueries({ queryKey: ["integrations", "connections"] });
        },
    });
}
