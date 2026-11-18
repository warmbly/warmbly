import type { CompanyEnrichOperation, CompanyEnrichQuery } from "@/lib/api/models/app/integrations/CompanyEnrich";

export const OPERATIONS: { value: CompanyEnrichOperation; label: string; hint: string }[] = [
    { value: "enrich_company", label: "Enrich a company", hint: "Find a company profile by domain." },
    { value: "search_companies", label: "Search companies", hint: "Find companies using names, descriptions and filters." },
    { value: "similar_companies", label: "Find similar companies", hint: "Find lookalikes from up to ten company domains." },
    { value: "search_people", label: "Search people", hint: "Research people by name, role and company." },
    { value: "lookup_person", label: "Look up a person", hint: "Match a person using an email address." },
    { value: "workforce", label: "View company workforce", hint: "Explore workforce data for one company." },
    { value: "enrich_company_properties", label: "Match company details", hint: "Match a company name or social profile." },
    { value: "enrich_companies_batch", label: "Enrich several companies", hint: "Enrich between one and fifty company domains." },
    { value: "get_company", label: "Get company by ID", hint: "Retrieve a previously discovered company." },
    { value: "get_person", label: "Get person by ID", hint: "Retrieve a previously discovered person." },
    { value: "count_companies", label: "Count matching companies", hint: "Count companies before fetching a page." },
    { value: "count_similar", label: "Count similar companies", hint: "Count lookalikes for up to ten domains." },
];

export interface ResearchForm {
    operation: CompanyEnrichOperation;
    domain: string;
    domains: string;
    id: string;
    email: string;
    query: string;
    semanticQuery: string;
    positionQuery: string;
    countries: string;
    name: string;
    linkedinUrl: string;
    filters: string;
    page: number;
    pageSize: number;
    expand: boolean;
    waitForEnrichment: boolean;
}

export const INITIAL_FORM: ResearchForm = {
    operation: "enrich_company", domain: "", domains: "", id: "", email: "", query: "", semanticQuery: "",
    positionQuery: "", countries: "", name: "", linkedinUrl: "", filters: "", page: 1, pageSize: 10,
    expand: false, waitForEnrichment: false,
};

export const PAGE_OPERATIONS: CompanyEnrichOperation[] = ["search_companies", "similar_companies", "search_people"];
export const FILTER_OPERATIONS: CompanyEnrichOperation[] = [...PAGE_OPERATIONS, "count_companies", "count_similar"];
export const SIMILAR_OPERATIONS: CompanyEnrichOperation[] = ["similar_companies", "count_similar"];
export const PEOPLE_OPERATIONS: CompanyEnrichOperation[] = ["search_people", "lookup_person", "get_person"];
export const EXPAND_OPERATIONS: CompanyEnrichOperation[] = [
    "enrich_company", "search_companies", "similar_companies", "search_people", "lookup_person",
    "enrich_company_properties", "enrich_companies_batch", "get_company", "get_person",
];

function jsonObject(value: string): Record<string, unknown> {
    if (!value.trim()) return {};
    let parsed: unknown;
    try { parsed = JSON.parse(value); } catch { throw new Error("Additional filters must be valid JSON."); }
    if (!parsed || typeof parsed !== "object" || Array.isArray(parsed)) throw new Error("Additional filters must be a JSON object.");
    function checkNumbers(value: unknown) {
        if (typeof value === "number" && (!Number.isFinite(value) || (Number.isInteger(value) && !Number.isSafeInteger(value)))) throw new Error("JSON numbers must be finite and precise. Put large IDs in quotes.");
        if (value && typeof value === "object") Object.values(value).forEach(checkNumbers);
    }
    checkNumbers(parsed);
    return parsed as Record<string, unknown>;
}

const split = (value: string) => value.split(/[,\n]/).map((part) => part.trim()).filter(Boolean);
const uuid = /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i;

export function buildResearchQuery(form: ResearchForm): CompanyEnrichQuery {
    const operation = form.operation;
    if (!OPERATIONS.some((item) => item.value === operation)) throw new Error("Choose a supported operation.");
    const body = FILTER_OPERATIONS.includes(operation) || operation === "enrich_company_properties" ? jsonObject(form.filters) : {};
    const query: Record<string, unknown> = {};
    if (PAGE_OPERATIONS.includes(operation)) {
        if (!Number.isInteger(form.page) || form.page < 1 || !Number.isInteger(form.pageSize) || form.pageSize < 1 || form.pageSize > 100 || form.page * form.pageSize > 10_000) {
            throw new Error("Choose a page of 1–100 results within the first 10,000 results.");
        }
        body.page = form.page;
        body.pageSize = form.pageSize;
    }
    if (EXPAND_OPERATIONS.includes(operation) && form.expand) query.expand = [PEOPLE_OPERATIONS.includes(operation) ? "education" : "workforce"];
    if (["enrich_company", "similar_companies", "count_similar"].includes(operation) && form.waitForEnrichment) query.waitForEnrichment = true;
    if (["search_companies", "count_companies", "search_people"].includes(operation)) {
        if (form.query.trim()) body.query = form.query.trim();
        if (form.query.trim().length > 250) throw new Error("Search text must be 250 characters or fewer.");
        if (form.countries.trim()) body.countries = split(form.countries);
    }
    if (["search_companies", "count_companies"].includes(operation)) {
        if (form.semanticQuery.trim().length > 500) throw new Error("Company description must be 500 characters or fewer.");
        if (form.semanticQuery.trim()) body.semanticQuery = form.semanticQuery.trim();
    }
    if (operation === "search_people" && form.positionQuery.trim()) body.positionQuery = split(form.positionQuery);
    if (SIMILAR_OPERATIONS.includes(operation) || operation === "enrich_companies_batch") {
        const domains: unknown = form.domains.trim() ? split(form.domains) : body.domains;
        const limit = operation === "enrich_companies_batch" ? 50 : 10;
        if (!Array.isArray(domains) || domains.length < 1 || domains.length > limit || domains.some((domain) => typeof domain !== "string" || !domain.trim())) throw new Error(`Enter between one and ${limit} company domains.`);
        body.domains = domains;
    }
    if (operation === "enrich_company" || operation === "workforce") {
        if (form.domain.trim()) query.domain = form.domain.trim();
        if (operation === "workforce" && form.id.trim()) query.id = form.id.trim();
        if (operation === "enrich_company" && !query.domain) throw new Error("Enter a company domain.");
        if (operation === "workforce" && Number(!!query.domain) + Number(!!query.id) !== 1) throw new Error("Enter either a domain or a company ID, not both.");
    }
    if (operation === "get_company" || operation === "get_person") query.id = form.id.trim();
    if ((operation === "get_company" || operation === "workforce") && query.id && !uuid.test(String(query.id))) throw new Error("Enter a valid company UUID.");
    if (operation === "get_company" && !query.id) throw new Error("Enter a company ID.");
    if (operation === "get_person" && (!/^[1-9]\d*$/.test(String(query.id)) || BigInt(String(query.id)) > 9223372036854775807n)) throw new Error("Enter a positive person ID as a decimal string.");
    if (operation === "lookup_person") {
        if (!/^[^\s@]+@[^\s@]+\.[^\s@]+$/.test(form.email.trim())) throw new Error("Enter an email address.");
        body.email = form.email.trim();
    }
    if (operation === "enrich_company_properties") {
        if (form.name.trim()) body.name = form.name.trim();
        if (form.linkedinUrl.trim()) body.linkedinUrl = form.linkedinUrl.trim();
        const identifiers = ["name", "linkedinUrl", "linkedinId", "twitterUrl", "facebookUrl", "instagramUrl", "youTubeUrl"];
        if (!identifiers.some((key) => typeof body[key] === "string" && (body[key] as string).trim())) throw new Error("Enter a company name or social profile.");
    }
    return { operation, ...(Object.keys(body).length ? { body } : {}), ...(Object.keys(query).length ? { query } : {}) };
}
