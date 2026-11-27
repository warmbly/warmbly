// Per-viewer finder preferences in localStorage; storage can be missing or throw, so every access falls back.
const KEY = "wb.linkedinFinder.prefs";

export interface FinderPrefs {
    sidebarWidth?: number;
    collapsed?: boolean;
    view?: string;
    columns?: string[];
    tab?: string;
    accountId?: string;
    viewFilters?: unknown;
}

export function readPrefs(): FinderPrefs {
    try {
        const raw = window.localStorage.getItem(KEY);
        const v = raw ? (JSON.parse(raw) as unknown) : null;
        return v && typeof v === "object" ? (v as FinderPrefs) : {};
    } catch {
        return {};
    }
}

export function writePrefs(patch: Partial<FinderPrefs>) {
    try {
        window.localStorage.setItem(KEY, JSON.stringify({ ...readPrefs(), ...patch }));
    } catch {
        // Not remembered; the finder works the same.
    }
}
