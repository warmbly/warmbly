import { useQuery } from "@tanstack/react-query";
import listIntegrationCatalog from "@/lib/api/client/app/integrations/listCatalog";

export default function useIntegrationCatalog() {
    return useQuery({
        queryKey: ["integrations", "catalog"],
        queryFn: listIntegrationCatalog,
        // Catalog is effectively static — refresh once an hour at most.
        staleTime: 60 * 60_000,
    });
}
