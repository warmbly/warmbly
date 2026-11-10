// Lead finder endpoints. Every call is scoped to the current workspace.

import type {
    AddLeadsToContactsRequest,
    AddLeadsToContactsResult,
    GeocodeResult,
    LatLng,
    Lead,
    LeadEnrichRequest,
    LeadEnrichmentRun,
    LeadExportFormat,
    LeadExportRows,
    LeadFacets,
    LeadFilter,
    LeadFinderConfig,
    LeadPage,
    LeadPatch,
    LeadPerson,
    LeadPersonInput,
    LeadPoint,
    LeadSearch,
    LeadSearchRequest,
    LeadSearchResult,
    LeadSelection,
    LeadSort,
} from "@/lib/api/models/app/leads/Lead";
import Request from "../../Request";

export async function getLeadFinderConfig(): Promise<LeadFinderConfig> {
    return await Request<LeadFinderConfig>({ method: "GET", url: "/leads/config", authorization: true });
}

export async function geocodeLocation(q: string, near?: LatLng, signal?: AbortSignal): Promise<GeocodeResult[]> {
    const params = new URLSearchParams({ q });
    if (near) {
        params.set("lat", near.lat.toFixed(4));
        params.set("lng", near.lng.toFixed(4));
    }
    const lang = typeof navigator !== "undefined" ? navigator.language.slice(0, 2) : "";
    if (lang) params.set("lang", lang);
    const body = await Request<{ data: GeocodeResult[] }>({
        method: "GET",
        url: `/leads/geocode?${params.toString()}`,
        authorization: true,
        signal,
    });
    return body?.data ?? [];
}

export async function searchLeads(req: LeadSearchRequest): Promise<LeadSearchResult> {
    return await Request<LeadSearchResult>({ method: "POST", url: "/leads/search", data: req, authorization: true });
}

export async function queryLeads(input: {
    filter: LeadFilter;
    sort: LeadSort;
    limit?: number;
    cursor?: string;
}): Promise<LeadPage> {
    return await Request<LeadPage>({ method: "POST", url: "/leads/query", data: input, authorization: true });
}

export async function getLeadPoints(filter: LeadFilter): Promise<{ data: LeadPoint[]; truncated: boolean }> {
    return await Request<{ data: LeadPoint[]; truncated: boolean }>({
        method: "POST",
        url: "/leads/points",
        data: { filter },
        authorization: true,
    });
}

export async function getLeadFacets(filter: LeadFilter): Promise<LeadFacets> {
    return await Request<LeadFacets>({ method: "POST", url: "/leads/facets", data: { filter }, authorization: true });
}

export async function getLead(id: string): Promise<Lead> {
    return await Request<Lead>({ method: "GET", url: `/leads/${id}`, authorization: true });
}

export async function updateLead(id: string, patch: LeadPatch): Promise<Lead> {
    return await Request<Lead>({ method: "PATCH", url: `/leads/${id}`, data: patch, authorization: true });
}

export async function updateLeads(input: LeadSelection & LeadPatch): Promise<{ updated: number }> {
    return await Request<{ updated: number }>({ method: "PATCH", url: "/leads", data: input, authorization: true });
}

export async function deleteLeads(sel: LeadSelection): Promise<{ deleted: number }> {
    return await Request<{ deleted: number }>({ method: "DELETE", url: "/leads", data: sel, authorization: true });
}

export async function enrichLeads(input: LeadEnrichRequest): Promise<LeadEnrichmentRun> {
    return await Request<LeadEnrichmentRun>({ method: "POST", url: "/leads/enrich", data: input, authorization: true });
}

export async function listLeadEnrichments(): Promise<LeadEnrichmentRun[]> {
    const body = await Request<{ data: LeadEnrichmentRun[] }>({ method: "GET", url: "/leads/enrichments", authorization: true });
    return body?.data ?? [];
}

export async function addLeadsToContacts(req: AddLeadsToContactsRequest): Promise<AddLeadsToContactsResult> {
    return await Request<AddLeadsToContactsResult>({ method: "POST", url: "/leads/contacts", data: req, authorization: true });
}

export async function exportLeads(input: LeadSelection & { format: LeadExportFormat; columns?: string[]; rows?: LeadExportRows }): Promise<Blob> {
    return await Request<Blob>({
        method: "POST",
        url: "/leads/export",
        data: input,
        authorization: true,
        responseType: "blob",
    });
}

export async function listLeadSearches(): Promise<LeadSearch[]> {
    const body = await Request<{ data: LeadSearch[] }>({ method: "GET", url: "/leads/searches", authorization: true });
    return body?.data ?? [];
}

export async function updateLeadSearch(id: string, patch: { name?: string; saved?: boolean }): Promise<LeadSearch> {
    return await Request<LeadSearch>({ method: "PATCH", url: `/leads/searches/${id}`, data: patch, authorization: true });
}

export async function deleteLeadSearch(id: string): Promise<void> {
    await Request<void>({ method: "DELETE", url: `/leads/searches/${id}`, authorization: true });
}

export async function addLeadPerson(leadId: string, input: LeadPersonInput): Promise<LeadPerson> {
    return await Request<LeadPerson>({ method: "POST", url: `/leads/${leadId}/people`, data: input, authorization: true });
}

export async function updateLeadPerson(leadId: string, personId: string, patch: LeadPersonInput): Promise<LeadPerson> {
    return await Request<LeadPerson>({ method: "PATCH", url: `/leads/${leadId}/people/${personId}`, data: patch, authorization: true });
}

export async function deleteLeadPerson(leadId: string, personId: string): Promise<void> {
    await Request<void>({ method: "DELETE", url: `/leads/${leadId}/people/${personId}`, authorization: true });
}

export async function findLeadPersonEmail(leadId: string, personId: string): Promise<LeadPerson> {
    return await Request<LeadPerson>({ method: "POST", url: `/leads/${leadId}/people/${personId}/find-email`, authorization: true });
}
