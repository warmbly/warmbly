import type { CompanyEnrichResult } from "@/lib/api/models/app/integrations/CompanyEnrich";

function record(value: unknown): Record<string, unknown> | null {
    return value !== null && typeof value === "object" && !Array.isArray(value) ? value as Record<string, unknown> : null;
}

function text(value: unknown): string {
    if (value === null || value === undefined || value === "") return "—";
    if (typeof value === "string" || typeof value === "number" || typeof value === "boolean") return String(value);
    if (Array.isArray(value)) return value.map(text).join(", ");
    const item = record(value);
    return item ? text(item.name ?? item.title ?? item.label ?? item.domain ?? "View full response") : "—";
}

const label = (key: string) => key.replace(/_/g, " ").replace(/([a-z])([A-Z])/g, "$1 $2");

export default function Results({ result }: { result: CompanyEnrichResult }) {
    const data = result.data;
    const page = record(data);
    const rows: unknown[] = Array.isArray(data) ? data : Array.isArray(page?.items) ? page.items
        : page?.person || page?.company ? [page.person, page.company].filter(Boolean) : data === null ? [] : [data];
    const metadata = page ? Object.entries(page).filter(([key]) => ["page", "pageSize", "totalItems", "totalPages", "total", "hasMore"].includes(key)) : [];
    const scores = record(record(page?.metadata)?.scores) ?? record(page?.scores);

    return <section className="space-y-3" aria-label="CompanyEnrich results">
        <div className="flex flex-wrap items-center gap-3 text-[12px] text-slate-500">
            {result.credits_consumed !== undefined && <span>{result.credits_consumed} credits consumed</span>}
            {metadata.map(([key, value]) => <span key={key}>{label(key)}: <strong className="font-medium text-slate-700">{text(value)}</strong></span>)}
        </div>
        {typeof data === "number" ? <div className="rounded-md border border-sky-100 bg-sky-50 p-5"><div className="text-3xl font-semibold text-sky-900">{data.toLocaleString()}</div><p className="mt-1 text-[12.5px] text-sky-700">Matching companies</p></div>
            : rows.length === 0 ? <p className="rounded-md border border-slate-200 p-5 text-[12.5px] text-slate-500">No results returned for this request.</p>
            : <div className="grid gap-3 md:grid-cols-2">{rows.map((row, index) => {
                const item = record(row);
                if (!item) return <div key={index} className="rounded-md border border-slate-200 p-3 text-[12.5px]">{text(row)}</div>;
                const title = item.name ?? item.fullName ?? item.full_name ?? ([item.first_name ?? item.firstName, item.last_name ?? item.lastName].filter(Boolean).join(" ") || item.domain || `Result ${index + 1}`);
                const preferred = ["domain", "email", "headline", "position", "company", "industry", "employees", "revenue", "location", "country", "id", "score"];
                let details = preferred.filter((key) => item[key] !== undefined).map((key) => [key, item[key]] as const);
                if (!details.length) details = Object.entries(item).slice(0, 12);
                const score = item.id !== undefined ? scores?.[String(item.id)] : undefined;
                return <article key={index} className="min-w-0 rounded-md border border-slate-200 bg-white p-4">
                    <h3 className="break-words text-[14px] font-semibold text-slate-900">{text(title)}</h3>
                    {item.description && typeof item.description === "string" ? <p className="mt-1 line-clamp-3 text-[12px] text-slate-500">{item.description}</p> : null}
                    <dl className="mt-3 space-y-1.5 text-[12px]">{details.map(([key, value]) => <div key={key} className="grid grid-cols-[90px_minmax(0,1fr)] gap-2"><dt className="capitalize text-slate-400">{label(key)}</dt><dd className="break-words text-slate-700">{text(value)}</dd></div>)}
                        {score !== undefined && <div className="flex gap-2"><dt className="text-slate-400">Similarity score</dt><dd>{text(score)}</dd></div>}
                    </dl>
                </article>;
            })}</div>}
        <details className="rounded-md border border-slate-200 bg-slate-50 p-3">
            <summary className="cursor-pointer text-[12px] font-medium text-slate-600">Full response, including all fields and scores</summary>
            <pre className="mt-3 max-h-96 overflow-auto whitespace-pre-wrap break-all text-[11px] text-slate-600">{JSON.stringify(data, null, 2)}</pre>
        </details>
    </section>;
}
