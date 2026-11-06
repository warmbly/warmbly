import React from "react";
import { Loader2Icon, SearchIcon } from "lucide-react";
import { TextInput, NumberInput, FieldError, Label } from "@/components/ui/field";
import Checkbox from "@/components/ui/checkbox";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { useCompanyEnrichQuery } from "@/lib/api/hooks/app/integrations/useCompanyEnrich";
import type { CompanyEnrichOperation, CompanyEnrichResult } from "@/lib/api/models/app/integrations/CompanyEnrich";
import { primaryBtn } from "../salesforce/_components/shared";
import { errMsg } from "../salesforce/_components/util";
import { buildResearchQuery, EXPAND_OPERATIONS, FILTER_OPERATIONS, INITIAL_FORM, OPERATIONS, PAGE_OPERATIONS, PEOPLE_OPERATIONS, SIMILAR_OPERATIONS, type ResearchForm } from "./queryForm";
import Results from "./Results";

export default function ResearchPanel({ connectionId, enabled }: { connectionId: string; enabled: boolean }) {
    const [form, setForm] = React.useState<ResearchForm>(INITIAL_FORM);
    const [result, setResult] = React.useState<{ response: CompanyEnrichResult; label: string } | null>(null);
    const [error, setError] = React.useState("");
    const generation = React.useRef(0);
    const inFlight = React.useRef(false);
    const request = useCompanyEnrichQuery(connectionId);
    const op = form.operation;
    const operation = OPERATIONS.find((entry) => entry.value === op)!;

    React.useEffect(() => () => { generation.current += 1; }, []);

    function update<K extends keyof ResearchForm>(key: K, value: ResearchForm[K]) {
        generation.current += 1;
        setForm((current) => key === "operation" ? { ...INITIAL_FORM, operation: value as CompanyEnrichOperation } : { ...current, [key]: value });
        setResult(null); setError("");
    }

    async function run(event: React.FormEvent) {
        event.preventDefault();
        if (!enabled || inFlight.current) return;
        let input;
        try { input = buildResearchQuery(form); } catch (reason) { setError(errMsg(reason, "Check the request fields.")); return; }
        const stamp = generation.current;
        inFlight.current = true;
        setResult(null); setError("");
        try {
            const response = await request.mutateAsync(input);
            if (stamp === generation.current) setResult({ response, label: operation.label });
        } catch (reason) {
            if (stamp === generation.current) setError(errMsg(reason, "CompanyEnrich could not complete the request."));
        } finally { inFlight.current = false; }
    }

    const field = (key: keyof ResearchForm, title: string, placeholder = "", maxLength?: number) => <Label className="mb-0 block space-y-1.5 normal-case tracking-normal"><span className="text-[12px] font-medium text-slate-700">{title}</span><TextInput value={String(form[key])} onChange={(value) => update(key, value)} placeholder={placeholder} maxLength={maxLength} className="w-full" /></Label>;
    const filters = FILTER_OPERATIONS.includes(op) || op === "enrich_company_properties";
    return <div className="space-y-6">
        <form onSubmit={(event) => void run(event)} className="rounded-md border border-slate-200 bg-white p-4 sm:p-5">
            <fieldset disabled={!enabled || request.isPending} className="space-y-4 disabled:opacity-60">
                <div>
                    <label htmlFor="companyenrich-operation" className="mb-1.5 block text-[12px] font-medium text-slate-700">Research action</label>
                    <Select value={op} onValueChange={(value) => update("operation", value as CompanyEnrichOperation)} disabled={!enabled || request.isPending}>
                        <SelectTrigger id="companyenrich-operation" className="w-full rounded-md border-slate-200 text-[12.5px] sm:max-w-sm"><SelectValue /></SelectTrigger>
                        <SelectContent>{OPERATIONS.map((entry) => <SelectItem key={entry.value} value={entry.value} className="text-[12.5px]">{entry.label}</SelectItem>)}</SelectContent>
                    </Select>
                    <p className="mt-2 text-[12px] text-slate-500">{operation.hint}</p>
                </div>
                <div className="grid gap-4 sm:grid-cols-2">
                    {["enrich_company", "workforce"].includes(op) && field("domain", "Company domain", "example.com")}
                    {["get_company", "get_person", "workforce"].includes(op) && field("id", op === "get_person" ? "Person ID" : "Company ID", op === "get_person" ? "Decimal ID, kept as text" : "Company UUID")}
                    {(SIMILAR_OPERATIONS.includes(op) || op === "enrich_companies_batch") && field("domains", "Company domains", "example.com, another.com")}
                    {op === "lookup_person" && field("email", "Email address", "person@example.com")}
                    {["search_companies", "count_companies", "search_people"].includes(op) && field("query", op === "search_people" ? "Person name" : "Company name or domain", "", 250)}
                    {["search_companies", "count_companies"].includes(op) && field("semanticQuery", "Company description", "Cloud security software companies", 500)}
                    {op === "search_people" && field("positionQuery", "Job titles", "Founder, Head of Sales")}
                    {["search_companies", "count_companies", "search_people"].includes(op) && field("countries", "Country codes", "US, GB")}
                    {op === "enrich_company_properties" && <>{field("name", "Company name", "Example company")}{field("linkedinUrl", "LinkedIn company URL", "https://www.linkedin.com/company/...")}</>}
                    {PAGE_OPERATIONS.includes(op) && <>
                        <Label className="mb-0 space-y-1.5 normal-case tracking-normal"><span className="text-[12px] font-medium text-slate-700">Page</span><NumberInput value={form.page} onChange={(value) => update("page", value)} min={1} max={Math.floor(10_000 / form.pageSize)} className="w-full" /></Label>
                        <Label className="mb-0 space-y-1.5 normal-case tracking-normal"><span className="text-[12px] font-medium text-slate-700">Results per page</span><NumberInput value={form.pageSize} onChange={(value) => update("pageSize", value)} min={1} max={100} className="w-full" /></Label>
                    </>}
                </div>
                {op === "workforce" && <p className="text-[11.5px] text-slate-500">Use either the company domain or ID.</p>}
                {EXPAND_OPERATIONS.includes(op) && <label className="flex items-center gap-2 text-[12px] text-slate-600"><Checkbox tone="slate" checked={form.expand} onChange={(event) => update("expand", event.target.checked)} />Include {PEOPLE_OPERATIONS.includes(op) ? "education" : "workforce"} details (may cost extra credits)</label>}
                {["enrich_company", "similar_companies", "count_similar"].includes(op) && <label className="flex items-center gap-2 text-[12px] text-slate-600"><Checkbox tone="slate" checked={form.waitForEnrichment} onChange={(event) => update("waitForEnrichment", event.target.checked)} />Wait for enrichment in this request</label>}
                {filters && <details className="rounded-md border border-slate-200 p-3"><summary className="cursor-pointer text-[12px] font-medium text-slate-600">Additional filters and properties</summary><label className="mt-3 block"><span className="mb-1 block text-[11.5px] text-slate-500">Documented JSON fields. Filled-in fields above take precedence; page controls always set the requested page.</span><textarea aria-label="Additional filters JSON" value={form.filters} onChange={(event) => update("filters", event.target.value)} placeholder={'{"countries":["US"]}'} rows={4} className="w-full rounded-md border border-slate-200 p-2 font-mono text-[12px] outline-none focus:border-sky-400 focus:ring-2 focus:ring-sky-100" /></label><a href="https://docs.companyenrich.com/reference" target="_blank" rel="noopener noreferrer" className="text-[11.5px] text-sky-700 underline">CompanyEnrich filter reference</a></details>}
                <div className="rounded-md border border-amber-200 bg-amber-50 p-3">
                    <p className="text-[12px] text-amber-900">Running a request uses your CompanyEnrich credits.</p>
                    <p className="mt-1 text-[11.5px] text-amber-700">One request only. Results stay here for review. Every new page needs another request; no contacts are imported or enrolled.</p>
                </div>
                <button type="submit" className={primaryBtn} disabled={!enabled || request.isPending}>{request.isPending ? <Loader2Icon className="size-3.5 animate-spin" /> : <SearchIcon className="size-3.5" />}{request.isPending ? "Requesting…" : "Run one request"}</button>
            </fieldset>
            <FieldError message={error} />
        </form>
        {result && <div className="space-y-3"><h2 className="text-[14px] font-semibold text-slate-800">{result.label} results</h2><Results result={result.response} /></div>}
    </div>;
}
