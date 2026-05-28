import type { DMARCReport } from "@/lib/api/models/app/integrations/Integration";
import Request from "../../Request";

export default async function listDMARCReports(domain?: string): Promise<{ reports: DMARCReport[] }> {
    const qs = domain ? `?domain=${encodeURIComponent(domain)}` : "";
    return await Request<{ reports: DMARCReport[] }>({
        method: "GET",
        url: `/integrations/dmarc/reports${qs}`,
        authorization: true,
    });
}
