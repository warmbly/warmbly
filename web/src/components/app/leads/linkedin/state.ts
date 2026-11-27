// LinkedIn finder state, kept pure: the query being built, the selection, the end of the result list and formatting.
import type { AiChange, FinderCatalog, FinderFilter, FinderHit, FinderQuery, FinderSource, FinderUsage, FinderValue, SocialAccount, SearchPage } from "@/lib/api/models/app/social/Social";
import { usageFor } from "@/components/app/social/parts";

export type FiltersState = Record<string, FinderValue[]>;

export interface FinderState {
    source: FinderSource;
    keywords: string;
    filters: FiltersState;
    /** Set when a pasted search runs exactly as LinkedIn has it. */
    url?: string;
}

export type FinderAction =
    | { type: "source"; source: FinderSource }
    | { type: "keywords"; keywords: string }
    | { type: "add"; filter: FinderFilter; value: FinderValue }
    | { type: "remove"; key: string; value: FinderValue }
    | { type: "toggleExclude"; filter: FinderFilter; value: FinderValue }
    | { type: "option"; filter: FinderFilter; id: string; label: string; mode: "include" | "exclude" | "off" }
    | { type: "toggle"; filter: FinderFilter; on: boolean }
    | { type: "range"; filter: FinderFilter; value: string | null }
    | { type: "clear"; key: string }
    | { type: "clearAll" }
    | { type: "load"; query: FinderQuery }
    | { type: "useFilters" }
    | { type: "aiChange"; change: AiChange };

export const MAX_VALUES = 25;

export function initialState(source: FinderSource = "sales_navigator_leads"): FinderState {
    return { source, keywords: "", filters: {} };
}

/** A value's identity inside a filter: its id, else its text, case-insensitive. */
export function valueKey(v: FinderValue): string {
    return (v.id || v.text || "").trim().toLowerCase();
}

function withFilter(s: FinderState, key: string, values: FinderValue[]): FinderState {
    const filters = { ...s.filters };
    if (values.length === 0) delete filters[key];
    else filters[key] = values;
    return { ...s, filters, url: undefined };
}

export function finderReducer(s: FinderState, a: FinderAction): FinderState {
    switch (a.type) {
        case "source":
            // Filters belong to one source's panel; switching starts fresh and keeps the keywords.
            return a.source === s.source ? s : { source: a.source, keywords: s.keywords, filters: {} };
        case "keywords":
            return { ...s, keywords: a.keywords, url: undefined };
        case "add": {
            const cur = s.filters[a.filter.key] ?? [];
            const v: FinderValue = { ...a.value, text: a.value.text?.trim(), id: a.value.id?.trim() || undefined };
            if (!valueKey(v)) return s;
            if (v.exclude && !a.filter.exclude) v.exclude = false;
            if (a.filter.single) return withFilter(s, a.filter.key, [v]);
            const i = cur.findIndex((x) => valueKey(x) === valueKey(v));
            if (i >= 0) {
                // Adding a value again moves it to the side asked for.
                const next = [...cur];
                next[i] = { ...next[i], exclude: v.exclude || undefined };
                return withFilter(s, a.filter.key, next);
            }
            if (cur.length >= MAX_VALUES) return s;
            return withFilter(s, a.filter.key, [...cur, { ...v, exclude: v.exclude || undefined }]);
        }
        case "remove":
            return withFilter(s, a.key, (s.filters[a.key] ?? []).filter((x) => valueKey(x) !== valueKey(a.value)));
        case "toggleExclude": {
            if (!a.filter.exclude) return s;
            const cur = s.filters[a.filter.key] ?? [];
            return withFilter(
                s,
                a.filter.key,
                cur.map((x) => (valueKey(x) === valueKey(a.value) ? { ...x, exclude: x.exclude ? undefined : true } : x)),
            );
        }
        case "option": {
            const rest = (s.filters[a.filter.key] ?? []).filter((x) => x.id !== a.id);
            if (a.mode === "off") return withFilter(s, a.filter.key, rest);
            const exclude = a.mode === "exclude" && a.filter.exclude ? true : undefined;
            const v: FinderValue = { id: a.id, text: a.label, ...(exclude ? { exclude } : {}) };
            return withFilter(s, a.filter.key, a.filter.single ? [v] : [...rest, v]);
        }
        case "toggle":
            return withFilter(s, a.filter.key, a.on ? [{ id: "true", text: a.filter.label }] : []);
        case "range":
            return withFilter(s, a.filter.key, a.value ? [{ id: a.value, text: a.value }] : []);
        case "clear":
            return withFilter(s, a.key, []);
        case "clearAll":
            return { source: s.source, keywords: "", filters: {} };
        case "load":
            return { source: a.query.source, keywords: a.query.keywords ?? "", filters: { ...(a.query.filters ?? {}) }, url: a.query.url || undefined };
        case "useFilters":
            return { ...s, url: undefined };
        case "aiChange":
            return applyAiChange(s, a.change);
    }
}

/** One change the AI proposed, applied to the search. */
export function applyAiChange(s: FinderState, c: AiChange): FinderState {
    if (c.filter === "keywords") {
        const kw = c.action === "set" || c.action === "add" ? (c.values[0]?.text ?? c.values[0]?.id ?? "") : "";
        return { ...s, keywords: kw, url: undefined };
    }
    const cur = s.filters[c.filter] ?? [];
    const clean = (v: FinderValue): FinderValue => ({ ...(v.id ? { id: v.id } : {}), ...(v.text ? { text: v.text } : {}), ...(v.exclude ? { exclude: true } : {}) });
    switch (c.action) {
        case "set":
            return withFilter(s, c.filter, c.values.map(clean));
        case "add": {
            const next = [...cur];
            for (const v of c.values.map(clean)) {
                const i = next.findIndex((x) => valueKey(x) === valueKey(v));
                if (i >= 0) next[i] = v;
                else next.push(v);
            }
            return withFilter(s, c.filter, next);
        }
        case "remove": {
            const drop = new Set(c.values.map(valueKey));
            return withFilter(s, c.filter, cur.filter((x) => !drop.has(valueKey(x))));
        }
        case "clear":
            return withFilter(s, c.filter, []);
    }
}

/** Whether the search already has what the change asks for. */
export function aiChangeApplied(s: FinderState, c: AiChange): boolean {
    const next = applyAiChange(s, c);
    return next.keywords === s.keywords && JSON.stringify(toQuery(next).filters ?? {}) === JSON.stringify(toQuery(s).filters ?? {});
}

/** The API query of a state, empty filters dropped. */
export function toQuery(s: FinderState): FinderQuery {
    const filters: FiltersState = {};
    for (const [k, vs] of Object.entries(s.filters)) if (vs.length) filters[k] = vs;
    const q: FinderQuery = { source: s.source };
    if (s.keywords.trim()) q.keywords = s.keywords.trim();
    if (Object.keys(filters).length) q.filters = filters;
    if (s.url) q.url = s.url;
    return q;
}

/** Whether two queries search the same thing, filter and value order aside. */
export function sameQuery(a: FinderQuery, b: FinderQuery): boolean {
    const norm = (q: FinderQuery) =>
        JSON.stringify([
            q.source,
            q.keywords?.trim() ?? "",
            q.url ?? "",
            Object.entries(q.filters ?? {})
                .filter(([, vs]) => vs.length)
                .map(([k, vs]) => [k, vs.map(valueKey).sort()] as const)
                .sort(([x], [y]) => x.localeCompare(y)),
        ]);
    return norm(a) === norm(b);
}

export function isEmpty(s: FinderState): boolean {
    return !s.url && !s.keywords.trim() && Object.values(s.filters).every((v) => v.length === 0);
}

export function activeFilterCount(s: FinderState): number {
    return Object.values(s.filters).filter((v) => v.length > 0).length;
}

export const isSupported = (f: FinderFilter) => f.supported !== false;

export function filtersByKey(c: FinderCatalog | undefined): Record<string, FinderFilter> {
    const out: Record<string, FinderFilter> = {};
    for (const g of c?.groups ?? []) for (const f of g.filters) out[f.key] = f;
    return out;
}

// ---------------------------------------------------------------- Boolean keywords

export interface KeywordCheck {
    ok: boolean;
    message?: string;
    operators: number;
}

/** LinkedIn's Boolean rules: capital operators, closed quotes and parentheses. */
export function checkKeywords(s: string, salesNavigator: boolean): KeywordCheck {
    let depth = 0;
    let quoted = false;
    for (const ch of s) {
        if (ch === '"') quoted = !quoted;
        else if (!quoted && ch === "(") depth++;
        else if (!quoted && ch === ")") {
            depth--;
            if (depth < 0) return { ok: false, message: "A closing parenthesis has no opening one.", operators: 0 };
        }
    }
    const parts = s.split('"');
    const outside = parts.filter((_, i) => i % 2 === 0).join(" ");
    // A quoted phrase stays one term, so `"a b" OR "c d"` has terms on both sides of OR.
    const terms = parts.map((p, i) => (i % 2 === 1 ? " \u0000phrase " : p)).join(" ");
    const words = terms.replace(/[()]/g, " ").split(/\s+/).filter(Boolean);
    const operators = words.filter((w) => w === "AND" || w === "OR" || w === "NOT").length;
    if (quoted) return { ok: false, message: "A quote is not closed.", operators };
    if (depth !== 0) return { ok: false, message: "A parenthesis is not closed.", operators };
    if (/[{}[\]<>*]/.test(outside)) return { ok: false, message: "LinkedIn does not support { } [ ] < > or * in keyword searches.", operators };
    if (words.length && ["AND", "OR"].some((op) => words[0] === op || words[words.length - 1] === op))
        return { ok: false, message: "Keywords cannot start or end with AND or OR.", operators };
    if (salesNavigator && operators > 15) return { ok: false, message: "Sales Navigator allows at most 15 Boolean operators.", operators };
    const lower = words.find((w) => w === "and" || w === "or" || w === "not");
    if (lower) return { ok: true, message: `"${lower}" is read as a word. Type ${lower.toUpperCase()} in capitals to use it as an operator.`, operators };
    return { ok: true, operators };
}

// ---------------------------------------------------------------- accounts

/** "Any account": the server picks the licensed account with the most search budget left. */
export const AUTO_ACCOUNT = "auto";

export interface FinderAccount {
    id: string;
    display_name: string;
    avatar_url: string;
    status: string;
    licences: string[];
    sources: FinderSource[];
    usage: { used: number; limit: number };
}

/** The sources an account's licences can search: LinkedIn people for any, plus Sales Navigator and Recruiter. */
export function accountSources(a: SocialAccount): FinderSource[] {
    const out: FinderSource[] = [];
    if (a.licences.includes("sales_navigator")) out.push("sales_navigator_leads", "sales_navigator_accounts");
    out.push("classic_people");
    if (a.licences.includes("recruiter") || a.licences.includes("recruiter_lite")) out.push("recruiter_people");
    return out;
}

export function toFinderAccounts(accounts: SocialAccount[] | undefined): FinderAccount[] | undefined {
    return accounts
        ?.filter((a) => a.status !== "removed" && a.channel === "linkedin")
        .map((a) => {
            const u = usageFor(a, "search");
            return { id: a.id, display_name: a.display_name, avatar_url: a.avatar_url, status: a.status, licences: a.licences, sources: accountSources(a), usage: { used: u.today, limit: u.daily_limit } };
        });
}

export function accountCan(a: FinderAccount | undefined, s: FinderSource): boolean {
    return !!a && a.status === "active" && a.sources.includes(s);
}

export function eligibleAccounts(accounts: FinderAccount[] | undefined, s: FinderSource): FinderAccount[] {
    return (accounts ?? []).filter((a) => accountCan(a, s));
}

/** "Sales Navigator", "Recruiter" or "LinkedIn": who can run a source. */
export function poolName(s: FinderSource): string {
    if (s.startsWith("sales_navigator")) return "Sales Navigator";
    if (s === "recruiter_people") return "Recruiter";
    return "LinkedIn";
}

export interface PoolSummary {
    count: number;
    left: number;
    limit: number;
}

/** Today's search budget of every account that can search the source. */
export function poolSummary(accounts: FinderAccount[] | undefined, s: FinderSource): PoolSummary {
    const ok = eligibleAccounts(accounts, s);
    return {
        count: ok.length,
        left: ok.reduce((n, a) => n + Math.max(a.usage.limit - a.usage.used, 0), 0),
        limit: ok.reduce((n, a) => n + a.usage.limit, 0),
    };
}

/** A picked account stays while it can search the source; else "any account" for two or more, else the one. */
export function resolveAccount(accounts: FinderAccount[] | undefined, s: FinderSource, current: string): string {
    const ok = eligibleAccounts(accounts, s);
    if (current && current !== AUTO_ACCOUNT && ok.some((a) => a.id === current)) return current;
    if (ok.length >= 2) return AUTO_ACCOUNT;
    if (ok[0]) return ok[0].id;
    if (current && current !== AUTO_ACCOUNT && accounts?.some((a) => a.id === current)) return current;
    return accounts?.find((a) => a.status === "active")?.id ?? "";
}

/** The account id the API takes: null for "any account". */
export const apiAccount = (id: string): string | null => (!id || id === AUTO_ACCOUNT ? null : id);

// ---------------------------------------------------------------- selection

export type Selection = Set<string>;

export function toggleKey(sel: Selection, key: string): Selection {
    const keys = new Set(sel);
    if (keys.has(key)) keys.delete(key);
    else keys.add(key);
    return keys;
}

/** All of them when any is unselected, else none of them. */
export function togglePage(sel: Selection, pageKeys: string[]): Selection {
    const keys = new Set(sel);
    const allOn = pageKeys.length > 0 && pageKeys.every((k) => keys.has(k));
    for (const k of pageKeys) {
        if (allOn) keys.delete(k);
        else keys.add(k);
    }
    return keys;
}

/** A row that can be imported: it has a LinkedIn identity. */
export function isSelectable(h: FinderHit): boolean {
    return !!(h.person?.provider_id || h.person?.public_id || h.company);
}

export function firstNKeys(hits: FinderHit[], n: number): string[] {
    return hits.slice(0, Math.max(n, 0)).filter(isSelectable).map((h) => h.key);
}

// ---------------------------------------------------------------- infinite results

/** LinkedIn people pages always hold 10 rows. */
export const CLASSIC_PAGE = 10;

export type ListTail =
    | { kind: "more" }
    | { kind: "loading" }
    | { kind: "error"; error: unknown }
    | { kind: "budget"; usage: FinderUsage }
    | { kind: "cap"; cap: number; total: number | null }
    | { kind: "end"; loaded: number };

export function isBudgetError(error: unknown): boolean {
    return !!error && typeof error === "object" && "code" in error && (error as { code?: string }).code === "search_limit_reached";
}

export function minPageCost(source: FinderSource): number {
    return source === "classic_people" ? CLASSIC_PAGE : 1;
}

export function listTail(args: { last: SearchPage | undefined; loaded: number; hasNext: boolean; fetchingNext: boolean; nextError: unknown; source: FinderSource }): ListTail {
    const { last, loaded, hasNext, fetchingNext, nextError, source } = args;
    if (fetchingNext || !last) return { kind: "loading" };
    if (hasNext) {
        if (nextError) return isBudgetError(nextError) ? { kind: "budget", usage: last.usage } : { kind: "error", error: nextError };
        if (last.usage.remaining < minPageCost(source)) return { kind: "budget", usage: last.usage };
        return { kind: "more" };
    }
    if (last.end_reason === "cap") return { kind: "cap", cap: last.cap, total: last.total ?? null };
    return { kind: "end", loaded };
}

/** The most rows "Select the first N" can reach: the cap, the count and what today's budget can still load. */
export function selectFirstMax(args: { loaded: number; cap: number; total: number | null; remaining: number; hasNext: boolean; source: FinderSource }): number {
    const { loaded, cap, total, remaining, hasNext, source } = args;
    if (!hasNext) return loaded;
    const budget = source === "classic_people" ? Math.floor(remaining / CLASSIC_PAGE) * CLASSIC_PAGE : remaining;
    return Math.max(loaded, Math.min(cap, total ?? cap, loaded + Math.max(budget, 0)));
}

const fmt = (n: number) => n.toLocaleString("en-US");

/** "Showing 120 of about 2,300 results", exact below 100. */
export function showingLabel(loaded: number, total: number | null | undefined): string {
    if (total == null) return loaded === 1 ? "Showing 1 result" : `Showing ${fmt(loaded)} results`;
    if (total === 0) return "No results";
    if (total < 100 || loaded >= total) return `Showing ${fmt(loaded)} of ${fmt(total)} result${total === 1 ? "" : "s"}`;
    return `Showing ${fmt(loaded)} of about ${fmt(total)} results`;
}

/** A rough time to load `rows` more, at one paced call every ~4 s. */
export function loadEta(rows: number, pageSize: number): string {
    const seconds = Math.ceil(rows / Math.max(pageSize, 1)) * 4;
    if (seconds < 60) return `about ${Math.max(seconds, 5)} s`;
    return `about ${Math.round(seconds / 60)} min`;
}

// ---------------------------------------------------------------- formatting

/** "2 yrs 3 mos" since a date. */
export function tenure(since: string | undefined, now: Date = new Date()): string | null {
    if (!since) return null;
    const d = new Date(since);
    if (Number.isNaN(d.getTime())) return null;
    let months = (now.getFullYear() - d.getFullYear()) * 12 + now.getMonth() - d.getMonth();
    if (now.getDate() < d.getDate()) months--;
    months = Math.max(months, 0);
    const y = Math.floor(months / 12);
    const m = months % 12;
    if (y === 0 && m === 0) return "Less than a month";
    const parts = [];
    if (y) parts.push(`${y} yr${y > 1 ? "s" : ""}`);
    if (m) parts.push(`${m} mo${m > 1 ? "s" : ""}`);
    return parts.join(" ");
}

export function degreeLabel(d: string | undefined): string | null {
    switch (d) {
        case "1st":
        case "2nd":
            return d;
        case "3rd":
            return "3rd+";
        case "out_of_network":
            return "Out of network";
    }
    return null;
}

export const SOURCE_LABELS: Record<FinderSource, string> = {
    sales_navigator_leads: "Sales Navigator leads",
    sales_navigator_accounts: "Sales Navigator accounts",
    classic_people: "LinkedIn people",
    recruiter_people: "Recruiter",
};
