import type { IntegrationCatalogEntry } from "@/lib/api/models/app/integrations/Integration";
import Request from "../../Request";

export default async function listIntegrationCatalog(): Promise<{ catalog: IntegrationCatalogEntry[] }> {
    return await Request<{ catalog: IntegrationCatalogEntry[] }>({
        method: "GET",
        url: "/integrations/catalog",
        authorization: true,
    });
}
