import Request from "../../Request";
import reviveDates from "@/lib/helper/reviveDates";
import type {
    CompanyEnrichContactInput, CompanyEnrichContactResult, CompanyEnrichQuery, CompanyEnrichResult,
} from "@/lib/api/models/app/integrations/CompanyEnrich";

export function queryCompanyEnrich(connectionId: string, input: CompanyEnrichQuery): Promise<CompanyEnrichResult> {
    return Request({
        method: "POST", url: `/integrations/companyenrich/${encodeURIComponent(connectionId)}/query`,
        data: input, authorization: true, replayAfterAuth: false, preserveJson: true,
    });
}

export async function enrichCompanyEnrichContact(connectionId: string, input: CompanyEnrichContactInput): Promise<CompanyEnrichContactResult> {
    const response = await Request<CompanyEnrichContactResult>({
        method: "POST", url: `/integrations/companyenrich/${encodeURIComponent(connectionId)}/enrich-contact`,
        data: input, authorization: true, replayAfterAuth: false, preserveJson: true,
    });
    // Normalize Warmbly's contact dates without modifying the provider payload.
    return { ...response, contact: reviveDates(response.contact) };
}
