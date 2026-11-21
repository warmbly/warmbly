// A search in plain words, deterministic (no AI): "VPs and Directors at 11-200-person companies in Germany who changed jobs".
import type { FinderCatalog, FinderQuery, FinderValue } from "@/lib/api/models/app/social/Social";
import { filtersByKey } from "./state";

const SENIORITY_PLURAL: Record<string, string> = {
    "OWNER/PARTNER": "Owners and Partners",
    OWNER: "Owners",
    PARTNER: "Partners",
    CXO: "CXOs",
    VICE_PRESIDENT: "VPs",
    VP: "VPs",
    DIRECTOR: "Directors",
    EXPERIENCED_MANAGER: "Experienced Managers",
    ENTRY_LEVEL_MANAGER: "Entry-level Managers",
    MANAGER: "Managers",
    STRATEGIC: "Strategic people",
    SENIOR: "Senior people",
    ENTRY_LEVEL: "Entry-level people",
    ENTRY: "Entry-level people",
    IN_TRAINING: "Trainees",
    TRAINING: "Trainees",
    UNPAID: "Unpaid people",
};

const DEGREES: Record<string, string> = { "1": "1st", "2": "2nd", "3": "3rd+" };

const SIGNALS: Record<string, string> = {
    changed_jobs: "changed jobs",
    posted_on_linkedin: "posted on LinkedIn recently",
    viewed_your_profile: "viewed your profile",
    following_your_company: "follow your company",
    past_colleague: "are past colleagues",
    shared_experiences: "share experiences with you",
    open_to_volunteering: "are open to volunteering",
};

const COMPANY_SIGNALS: Record<string, string> = {
    job_opportunities: "that are hiring",
    saved_accounts: "that you saved",
};

const SENIORITY = ["seniority"];
const FUNCTION = ["function", "job_functions"];
const TITLE = ["current_title", "job_titles", "title"];
const HEADCOUNT = ["company_headcount", "company_size", "headcount"];
const COMPANY_TYPE = ["company_type"];
const COMPANY = ["current_company", "current_companies", "company"];
const INDUSTRY = ["industry", "industries"];
const LOCATION = ["geography", "locations"];
const HQ = ["company_hq", "hq_location"];
const CONNECTION = ["connection", "network"];
const YEARS: Record<string, string> = {
    years_in_position: "in their role",
    years_in_company: "at their company",
    years_of_experience: "of experience",
};

/** "a", "a and b", "a, b and c", "a, b, c and 2 more" */
export function joinList(items: string[], word = "and", max = 3): string {
    const xs = items.filter(Boolean);
    if (xs.length <= 1) return xs[0] ?? "";
    if (xs.length > max) return `${xs.slice(0, max).join(", ")} ${word} ${xs.length - max} more`;
    return `${xs.slice(0, -1).join(", ")} ${word} ${xs[xs.length - 1]}`;
}

const label = (v: FinderValue) => (v.text || v.id || "").trim();
const quoted = (s: string) => `“${s}”`;

/** "11-200" from headcount buckets "11-50" and "51-200"; disjoint ranges are listed with "or". */
export function mergeRanges(ids: string[]): string {
    const rs = ids
        .map((id) => {
            const [a, b] = id.split("-");
            const lo = Number(a || 0);
            const hi = b === undefined || b === "" ? Infinity : Number(b);
            return Number.isFinite(lo) && !Number.isNaN(hi) ? { lo, hi } : null;
        })
        .filter((r): r is { lo: number; hi: number } => !!r)
        .sort((x, y) => x.lo - y.lo);
    const merged: { lo: number; hi: number }[] = [];
    for (const r of rs) {
        const last = merged.at(-1);
        if (last && r.lo <= last.hi + 1) last.hi = Math.max(last.hi, r.hi);
        else merged.push({ ...r });
    }
    const n = (x: number) => x.toLocaleString("en-US");
    return joinList(
        merged.map((r) => (r.hi === Infinity ? `${n(r.lo)}+` : `${n(r.lo)}-${n(r.hi)}`)),
        "or",
        4,
    );
}

/** The search in one sentence: who, where they work, where they are, what they did lately, and what is excluded. */
export function describeSearch(q: FinderQuery, catalog?: FinderCatalog): string {
    if (q.url && !Object.keys(q.filters ?? {}).length && !q.keywords) return "The pasted LinkedIn search, run exactly as LinkedIn has it";
    const filters = q.filters ?? {};
    const byKey = filtersByKey(catalog);
    const used = new Set<string>();
    const inc = (keys: string[]): FinderValue[] => {
        const out: FinderValue[] = [];
        for (const k of keys) {
            const vs = filters[k];
            if (!vs?.length) continue;
            used.add(k);
            out.push(...vs.filter((v) => !v.exclude));
        }
        return out;
    };
    const companies = q.source === "sales_navigator_accounts";

    const seniority = inc(SENIORITY);
    let who = companies ? "Companies" : seniority.length ? joinList(seniority.map((v) => SENIORITY_PLURAL[v.id ?? ""] ?? `${label(v)}s`)) : "People";
    const fn = inc(FUNCTION);
    if (fn.length && !companies) who += ` in ${joinList(fn.map(label))}`;
    const titles = inc(TITLE);
    if (titles.length) who += ` titled ${joinList(titles.map((v) => quoted(label(v))), "or")}`;
    if (q.keywords?.trim()) who += ` matching ${quoted(q.keywords.trim())}`;
    const parts = [who];

    const hc = inc(HEADCOUNT);
    const types = inc(COMPANY_TYPE);
    const industries = inc(INDUSTRY);
    const named = inc(COMPANY);
    if (companies) {
        if (industries.length) parts.push(`in ${joinList(industries.map(label), "or")}`);
        if (hc.length) parts.push(`with ${mergeRanges(hc.map((v) => v.id ?? ""))} employees`);
    } else if (named.length) {
        parts.push(`at ${joinList(named.map(label), "or")}`);
        if (hc.length) parts.push(`with ${mergeRanges(hc.map((v) => v.id ?? ""))} employees`);
        if (types.length) parts.push(`(${joinList(types.map(label), "or")})`);
        if (industries.length) parts.push(`in ${joinList(industries.map(label), "or")}`);
    } else if (hc.length || types.length || industries.length) {
        const size = hc.length ? `${mergeRanges(hc.map((v) => v.id ?? ""))}-person ` : "";
        const kind = types.length ? `${joinList(types.map(label), "or")} ` : "";
        const ind = industries.length ? `${joinList(industries.map(label), "or")} ` : "";
        parts.push(`at ${size}${ind}${kind}companies`.replace(/\s+/g, " "));
    }
    const hq = inc(HQ);
    if (hq.length) parts.push(`headquartered in ${joinList(hq.map(label), "or")}`);

    const loc = inc(LOCATION);
    if (loc.length) parts.push(`in ${joinList(loc.map(label), "or")}`);
    const deg = inc(CONNECTION);
    if (deg.length) {
        const names = deg.filter((v) => DEGREES[v.id ?? ""]).map((v) => DEGREES[v.id ?? ""]);
        const group = deg.some((v) => v.id === "GROUP");
        const net = names.length ? `in your ${joinList(names, "or")}-degree network` : "";
        parts.push([net, group ? (net ? "or fellow group members" : "who are fellow group members") : ""].filter(Boolean).join(" "));
    }
    for (const [key, suffix] of Object.entries(YEARS)) {
        const vs = inc([key]);
        if (vs.length) parts.push(`with ${joinList(vs.map((v) => label(v).toLowerCase()), "or")} ${suffix}`);
    }

    const signals: string[] = [];
    const companySignals: string[] = [];
    for (const [key, vs] of Object.entries(filters)) {
        if (!vs.length) continue;
        if (SIGNALS[key] && !companies) {
            used.add(key);
            signals.push(SIGNALS[key]);
        } else if (COMPANY_SIGNALS[key] && companies) {
            used.add(key);
            companySignals.push(COMPANY_SIGNALS[key]);
        }
    }
    if (signals.length) parts.push(`who ${joinList(signals, "and", 4)}`);
    if (companySignals.length) parts.push(joinList(companySignals, "and"));

    let sentence = parts.join(" ");

    const excluded: string[] = [];
    for (const vs of Object.values(filters)) for (const v of vs) if (v.exclude) excluded.push(label(v));
    if (excluded.length) sentence += `, excluding ${joinList(excluded, "and", 4)}`;

    const rest = Object.entries(filters)
        .filter(([k, vs]) => vs.some((v) => !v.exclude) && !used.has(k))
        .map(([k]) => (byKey[k]?.label ?? k.replace(/_/g, " ")).toLowerCase());
    if (rest.length) sentence += `; also filtered by ${joinList(rest, "and", 3)}`;
    return sentence;
}
