// How loaded results are shown, kept pure: Warmbly's own view filters, the three views, columns, grouping and smart selections.
import type { FinderHit } from "@/lib/api/models/app/social/Social";
import { isSelectable } from "./state";

// ---------------------------------------------------------------- views and columns

export type ResultView = "table" | "cards" | "companies";
export type TableColumn = "title" | "company" | "location" | "tenure" | "signals" | "reach" | "outreach" | "industry";

export const VIEWS: { value: ResultView; label: string }[] = [
    { value: "table", label: "Table" },
    { value: "cards", label: "Cards" },
    { value: "companies", label: "By company" },
];

export const ALL_COLUMNS: TableColumn[] = ["title", "company", "location", "tenure", "signals", "reach", "outreach", "industry"];
export const DEFAULT_COLUMNS: TableColumn[] = ["title", "company", "location", "signals", "reach", "outreach"];
export const COLUMN_LABELS: Record<TableColumn, string> = {
    title: "Title",
    company: "Company",
    location: "Location",
    tenure: "In role",
    signals: "Signals",
    reach: "Reach",
    outreach: "Outreach",
    industry: "Industry",
};

export function isView(v: unknown): v is ResultView {
    return v === "table" || v === "cards" || v === "companies";
}

/** Known columns in catalogue order, the defaults when none are left. */
export function normalizeColumns(cols: readonly string[] | undefined): TableColumn[] {
    if (!cols) return DEFAULT_COLUMNS;
    const set = new Set(cols);
    const out = ALL_COLUMNS.filter((c) => set.has(c));
    return out.length ? out : DEFAULT_COLUMNS;
}

/** The narrowest each column stays readable at; the person column takes 190, the checkbox and padding 76. */
export const COLUMN_MIN: Record<TableColumn, number> = { title: 130, company: 120, location: 110, tenure: 84, signals: 92, reach: 88, outreach: 112, industry: 120 };
const FIT_FIXED = 76 + 190;
const FIT_GAP = 12;
const DROP_ORDER: TableColumn[] = ["industry", "tenure", "location", "signals", "outreach", "company", "reach", "title"];

/** The chosen columns that fit `width`: the least useful go first. A width of 0 keeps them all. */
export function fitColumns(columns: TableColumn[], width: number): { shown: TableColumn[]; dropped: TableColumn[] } {
    if (width <= 0) return { shown: columns, dropped: [] };
    const keep = new Set(columns);
    const need = () => FIT_FIXED + [...keep].reduce((n, c) => n + COLUMN_MIN[c] + FIT_GAP, 0);
    for (const c of DROP_ORDER) {
        if (need() <= width || keep.size <= 1) break;
        keep.delete(c);
    }
    return { shown: columns.filter((c) => keep.has(c)), dropped: columns.filter((c) => !keep.has(c)) };
}

// ---------------------------------------------------------------- sidebar

export const SIDEBAR = { min: 380, max: 720, default: 480, rail: 48, minResults: 420 } as const;

/** The filter sidebar's width: the user's choice, narrower when the results would get too thin. */
export function sidebarWidth(stored: number, available: number): number {
    const w = Math.min(Math.max(Number.isFinite(stored) ? stored : SIDEBAR.default, SIDEBAR.min), SIDEBAR.max);
    if (available <= 0) return w;
    return Math.max(Math.min(w, available - SIDEBAR.minResults), Math.min(SIDEBAR.min, Math.floor(available / 2)));
}

// ---------------------------------------------------------------- outreach

export type OutreachStatus = "none" | "invite_pending" | "connected" | "messaged" | "replied";

export const OUTREACH_LABELS: Record<OutreachStatus, string> = {
    none: "Not contacted",
    invite_pending: "Invite pending",
    connected: "Connected",
    messaged: "Messaged",
    replied: "Replied",
};

export function outreachStatus(h: FinderHit): OutreachStatus {
    const s = h.warmbly?.outreach;
    return s && s in OUTREACH_LABELS ? (s as OutreachStatus) : "none";
}

/** Sent an invite or a message from any of the workspace's LinkedIn accounts. */
export function isContacted(h: FinderHit): boolean {
    const s = outreachStatus(h);
    return s === "invite_pending" || s === "messaged" || s === "replied";
}

export const campaignsOf = (h: FinderHit) => h.warmbly?.campaigns ?? [];
export const contactOf = (h: FinderHit) => h.warmbly?.contact_id ?? null;
export const companyContacts = (h: FinderHit) => h.warmbly?.company_contacts ?? 0;

export function reachScore(h: FinderHit): number {
    return h.reach?.score ?? -1;
}

// ---------------------------------------------------------------- companies

export interface CompanyRef {
    key: string;
    name: string;
}

/** The person's current company: LinkedIn's id, else the name. */
export function companyOf(h: FinderHit): CompanyRef | null {
    const id = h.insights?.current_company_id?.trim();
    const name = h.insights?.current_company?.trim() ?? "";
    if (id) return { key: `id:${id}`, name };
    if (name) return { key: `name:${name.toLowerCase()}`, name };
    return null;
}

/** "3 contacts in Warmbly" */
export function coverageLine(n: number): string {
    if (n <= 0) return "Nobody at this company is a contact yet";
    return `${n.toLocaleString("en-US")} contact${n === 1 ? "" : "s"} in Warmbly`;
}

// ---------------------------------------------------------------- view filters

/** Filters on what Warmbly knows. They hide loaded rows and limit selections; LinkedIn's count is unchanged. */
export interface ViewFilters {
    hideContacts: boolean;
    hideContacted: boolean;
    hideInCampaign: boolean;
    newCompanies: boolean;
    /** 0: no limit. */
    maxPerCompany: number;
}

export const NO_VIEW_FILTERS: ViewFilters = { hideContacts: false, hideContacted: false, hideInCampaign: false, newCompanies: false, maxPerCompany: 0 };

export function clampPerCompany(n: number): number {
    return Number.isFinite(n) ? Math.min(Math.max(Math.floor(n), 0), 100) : 0;
}

export type ViewFilterKey = keyof ViewFilters;

export function viewFilterChips(v: ViewFilters): { key: ViewFilterKey; label: string }[] {
    const out: { key: ViewFilterKey; label: string }[] = [];
    if (v.hideContacts) out.push({ key: "hideContacts", label: "Hide contacts already in Warmbly" });
    if (v.hideContacted) out.push({ key: "hideContacted", label: "Hide contacted" });
    if (v.hideInCampaign) out.push({ key: "hideInCampaign", label: "Hide people in a campaign" });
    if (v.newCompanies) out.push({ key: "newCompanies", label: "Only companies with no contacts" });
    if (v.maxPerCompany > 0) out.push({ key: "maxPerCompany", label: `At most ${v.maxPerCompany} per company` });
    return out;
}

export function withoutViewFilter(v: ViewFilters, key: ViewFilterKey): ViewFilters {
    return { ...v, [key]: NO_VIEW_FILTERS[key] };
}

export function readViewFilters(raw: unknown): ViewFilters {
    if (!raw || typeof raw !== "object") return NO_VIEW_FILTERS;
    const r = raw as Partial<ViewFilters>;
    return {
        hideContacts: !!r.hideContacts,
        hideContacted: !!r.hideContacted,
        hideInCampaign: !!r.hideInCampaign,
        newCompanies: !!r.newCompanies,
        maxPerCompany: clampPerCompany(Number(r.maxPerCompany ?? 0)),
    };
}

/** Loaded rows the view filters keep, in LinkedIn's order. Company rows are never hidden. */
export function applyView(hits: FinderHit[], v: ViewFilters): FinderHit[] {
    const perCompany = new Map<string, number>();
    const out: FinderHit[] = [];
    for (const h of hits) {
        if (h.company) {
            out.push(h);
            continue;
        }
        if (v.hideContacts && contactOf(h)) continue;
        if (v.hideContacted && isContacted(h)) continue;
        if (v.hideInCampaign && campaignsOf(h).length > 0) continue;
        if (v.newCompanies && companyContacts(h) > 0) continue;
        if (v.maxPerCompany > 0) {
            const c = companyOf(h);
            if (c) {
                const n = perCompany.get(c.key) ?? 0;
                if (n >= v.maxPerCompany) continue;
                perCompany.set(c.key, n + 1);
            }
        }
        out.push(h);
    }
    return out;
}

// ---------------------------------------------------------------- grouping and list items

export interface CompanyGroup {
    key: string;
    name: string;
    hits: FinderHit[];
    contacts: number;
}

export const NO_COMPANY = "__none__";

/** Rows under their current company, in order of first appearance; people without one last. */
export function groupByCompany(hits: FinderHit[]): CompanyGroup[] {
    const groups = new Map<string, CompanyGroup>();
    let none: CompanyGroup | null = null;
    for (const h of hits) {
        const c = companyOf(h);
        if (!c) {
            none ??= { key: NO_COMPANY, name: "No current company", hits: [], contacts: 0 };
            none.hits.push(h);
            continue;
        }
        let g = groups.get(c.key);
        if (!g) {
            g = { key: c.key, name: c.name || "Unnamed company", hits: [], contacts: 0 };
            groups.set(c.key, g);
        }
        g.hits.push(h);
        g.contacts = Math.max(g.contacts, companyContacts(h));
    }
    const out = [...groups.values()];
    if (none) out.push(none);
    return out;
}

export type ListItem = { kind: "row"; key: string; hit: FinderHit } | { kind: "group"; key: string; group: CompanyGroup };

export function listItems(hits: FinderHit[], view: ResultView): ListItem[] {
    if (view !== "companies") return hits.map((hit) => ({ kind: "row", key: `${view}:${hit.key}`, hit }));
    const out: ListItem[] = [];
    for (const group of groupByCompany(hits)) {
        out.push({ kind: "group", key: `g:${group.key}`, group });
        for (const hit of group.hits) out.push({ kind: "row", key: `companies:${hit.key}`, hit });
    }
    return out;
}

// ---------------------------------------------------------------- selection helpers

/** Selectable rows nobody has contacted and no campaign has, in list order. */
export function notContactedKeys(hits: FinderHit[]): string[] {
    return hits.filter((h) => isSelectable(h) && !isContacted(h) && campaignsOf(h).length === 0).map((h) => h.key);
}

/** The best n selectable people per company by reach, ties in LinkedIn's order. */
export function topPerCompanyKeys(hits: FinderHit[], n: number): string[] {
    if (n < 1) return [];
    const out: string[] = [];
    for (const g of groupByCompany(hits.filter(isSelectable))) {
        if (g.key === NO_COMPANY) {
            out.push(...g.hits.map((h) => h.key));
            continue;
        }
        const ranked = g.hits.map((h, i) => ({ h, i })).sort((a, b) => reachScore(b.h) - reachScore(a.h) || a.i - b.i);
        out.push(...ranked.slice(0, n).map((x) => x.h.key));
    }
    return out;
}

/** Keeps only selected keys still on screen; null when nothing changed. */
export function pruneSelection(keys: Set<string>, visible: Set<string>): Set<string> | null {
    let changed = false;
    const next = new Set<string>();
    for (const k of keys) {
        if (visible.has(k)) next.add(k);
        else changed = true;
    }
    return changed ? next : null;
}
