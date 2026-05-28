import { useQuery } from "@tanstack/react-query";
import listIntegrationConnections from "@/lib/api/client/app/integrations/listConnections";

export default function useIntegrationConnections() {
    return useQuery({
        queryKey: ["integrations", "connections"],
        queryFn: listIntegrationConnections,
        staleTime: 10_000,
    });
}
