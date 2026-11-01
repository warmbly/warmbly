import type Contact from "../contacts/Contact";

export type CompanyEnrichOperation =
    | "enrich_company" | "search_companies" | "similar_companies" | "search_people"
    | "lookup_person" | "workforce" | "enrich_company_properties" | "enrich_companies_batch"
    | "get_company" | "get_person" | "count_companies" | "count_similar";

export interface CompanyEnrichQuery {
    operation: CompanyEnrichOperation;
    body?: Record<string, unknown>;
    query?: Record<string, unknown>;
}

export interface CompanyEnrichResult {
    data: unknown;
    credits_consumed?: number;
}

export interface CompanyEnrichContactInput {
    contact_id: string;
    kind: "person" | "company";
    overwrite: false;
}

export interface CompanyEnrichContactResult extends CompanyEnrichResult {
    contact: Contact;
    updated_fields: string[];
}
