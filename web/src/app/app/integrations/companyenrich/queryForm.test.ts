import { describe, expect, it } from "vitest";
import { buildResearchQuery, INITIAL_FORM, type ResearchForm } from "./queryForm";

describe("CompanyEnrich research requests", () => {
    it.each([
        ["enrich_company", { domain: "example.com" }, undefined, { domain: "example.com" }],
        ["search_companies", { query: "Example", semanticQuery: "Security software" }, { page: 1, pageSize: 10, query: "Example", semanticQuery: "Security software" }, undefined],
        ["similar_companies", { domains: "a.com, b.com" }, { page: 1, pageSize: 10, domains: ["a.com", "b.com"] }, undefined],
        ["search_people", { query: "Jane", positionQuery: "Founder, CEO" }, { page: 1, pageSize: 10, query: "Jane", positionQuery: ["Founder", "CEO"] }, undefined],
        ["lookup_person", { email: "jane@example.com" }, { email: "jane@example.com" }, undefined],
        ["workforce", { domain: "example.com" }, undefined, { domain: "example.com" }],
        ["enrich_company_properties", { name: "Example", linkedinUrl: "https://linkedin.com/company/example" }, { name: "Example", linkedinUrl: "https://linkedin.com/company/example" }, undefined],
        ["enrich_companies_batch", { domains: "a.com\nb.com" }, { domains: ["a.com", "b.com"] }, undefined],
        ["get_company", { id: "00000000-0000-4000-8000-000000000001" }, undefined, { id: "00000000-0000-4000-8000-000000000001" }],
        ["get_person", { id: "9223372036854775807" }, undefined, { id: "9223372036854775807" }],
        ["count_companies", { query: "Example" }, { query: "Example" }, undefined],
        ["count_similar", { domains: "a.com" }, { domains: ["a.com"] }, undefined],
    ] as const)("constructs %s with its documented body and query", (operation, fields, body, query) => {
        const input = buildResearchQuery({ ...INITIAL_FORM, ...fields, operation });
        expect(input).toEqual({ operation, ...(body ? { body } : {}), ...(query ? { query } : {}) });
    });

    it("preserves future filters and expansions, with explicit inputs taking precedence", () => {
        expect(buildResearchQuery({ ...INITIAL_FORM, operation: "similar_companies", domains: "example.com", expand: true, waitForEnrichment: true, filters: '{"domains":["old.com"],"pageSize":100,"exclude":{"countries":["US"]},"future":{"value":false}}' })).toEqual({
            operation: "similar_companies", body: { domains: ["example.com"], page: 1, pageSize: 10, exclude: { countries: ["US"] }, future: { value: false } }, query: { expand: ["workforce"], waitForEnrichment: true },
        });
    });

    it("accepts JSON-only lookalike seeds when the dedicated field is empty", () => {
        expect(buildResearchQuery({ ...INITIAL_FORM, operation: "count_similar", filters: '{"domains":["example.com"]}' })).toEqual({ operation: "count_similar", body: { domains: ["example.com"] } });
        expect(() => buildResearchQuery({ ...INITIAL_FORM, operation: "count_similar", filters: '{"domains":[false]}' })).toThrow();
    });

    it.each([
        { operation: "enrich_company" },
        { operation: "workforce", domain: "example.com", id: "00000000-0000-4000-8000-000000000001" },
        { operation: "get_person", id: "9223372036854775808" },
        { operation: "get_person", id: "1.2" },
        { operation: "get_company", id: "not-a-uuid" },
        { operation: "lookup_person", email: "not-an-email" },
        { operation: "enrich_company_properties", filters: "{}" },
        { operation: "similar_companies", domains: Array(11).fill("example.com").join(",") },
        { operation: "enrich_companies_batch", domains: Array(51).fill("example.com").join(",") },
        { operation: "search_companies", page: 101, pageSize: 100 },
        { operation: "search_companies", page: 1.1 },
        { operation: "search_companies", filters: "[]" },
        { operation: "search_companies", filters: "{" },
        { operation: "search_companies", filters: '{"future":1e400}' },
        { operation: "search_companies", filters: '{"future":9223372036854775807}' },
        { operation: "search_companies", query: "q".repeat(251) },
        { operation: "search_companies", semanticQuery: "q".repeat(501) },
    ] satisfies Partial<ResearchForm>[])("rejects malformed or unbounded requests: %j", (fields) => {
        expect(() => buildResearchQuery({ ...INITIAL_FORM, ...fields })).toThrow();
    });
});
