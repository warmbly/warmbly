// The loaded submissions as one window, newest down to the oldest row loaded;
// a refresh re-reads that whole range so loaded rows never shift out of view.

import type { FormSubmissionsPage, FormSubmissionsPageParam } from "@/lib/api/client/app/forms";
import type { FormSubmission } from "@/lib/api/models/app/forms/Form";

export interface SubmissionsWindow {
    rows: FormSubmission[];
    next: FormSubmissionsPageParam | null;
    hasMore: boolean;
}

export type FetchSubmissionsPage = (page: FormSubmissionsPageParam | undefined, limit: number) => Promise<FormSubmissionsPage>;

export const SUBMISSIONS_PAGE_SIZE = 50;
const MAX_PAGE_SIZE = 100;
// Pages a refresh may read before it settles for a shorter window.
const MAX_REFRESH_PAGES = 20;

const STAMP = /^(.+T\d{2}:\d{2}:\d{2})(?:\.(\d+))?(Z|[+-]\d{2}:\d{2})$/i;

// Seconds since the epoch and the nanoseconds past them; Date alone stops at
// milliseconds, and the server orders by microseconds.
function stamp(iso: string): [number, number] {
    const m = STAMP.exec(iso);
    if (!m) return [Math.floor(Date.parse(iso) / 1000), 0];
    return [Date.parse(m[1] + m[3]) / 1000, Number((m[2] ?? "").padEnd(9, "0").slice(0, 9))];
}

/** The server's order, newest first: created_at DESC, then id DESC. Negative when a comes first. */
export function compareSubmissions(a: FormSubmission, b: FormSubmission): number {
    const [as, an] = stamp(a.created_at);
    const [bs, bn] = stamp(b.created_at);
    if (as !== bs) return bs - as;
    if (an !== bn) return bn - an;
    return a.id === b.id ? 0 : a.id < b.id ? 1 : -1;
}

function sortedUnique(rows: FormSubmission[]): FormSubmission[] {
    const byId = new Map<string, FormSubmission>();
    for (const r of rows) if (!byId.has(r.id)) byId.set(r.id, r);
    return [...byId.values()].sort(compareSubmissions);
}

function last<T>(xs: T[]): T | undefined {
    return xs[xs.length - 1];
}

/** Re-reads from the newest row down to the oldest row of `previous`, or one page when nothing was loaded. */
export async function readWindow(fetchPage: FetchSubmissionsPage, previous?: SubmissionsWindow): Promise<SubmissionsWindow> {
    const floor = previous && last(previous.rows);
    let page = await fetchPage(undefined, SUBMISSIONS_PAGE_SIZE);
    let rows = page.data;
    for (let n = 1; previous && floor && page.has_more && page.next && n < MAX_REFRESH_PAGES; n++) {
        const tail = last(page.data);
        if (!tail || compareSubmissions(tail, floor) >= 0) break;
        // Ask for exactly the loaded rows still unseen, so the window ends on the floor instead of past it.
        const unseen = previous.rows.filter((r) => compareSubmissions(r, tail) > 0).length;
        page = await fetchPage(page.next, Math.min(Math.max(unseen, 1), MAX_PAGE_SIZE));
        rows = rows.concat(page.data);
    }
    return { rows: sortedUnique(rows), next: page.has_more ? page.next : null, hasMore: page.has_more && !!page.next };
}

/** Lays a refreshed window over the cache as it is now, keeping older rows a load-more appended meanwhile. */
export function mergeRefreshed(fresh: SubmissionsWindow, current: SubmissionsWindow | undefined): SubmissionsWindow {
    const tail = last(fresh.rows);
    if (!current || !fresh.hasMore || !tail) return fresh;
    const older = current.rows.filter((r) => compareSubmissions(r, tail) > 0);
    if (older.length === 0) return fresh;
    return { rows: sortedUnique([...fresh.rows, ...older]), next: current.next, hasMore: current.hasMore };
}

/** Appends the page that continues the window. */
export function appendPage(window: SubmissionsWindow, page: FormSubmissionsPage): SubmissionsWindow {
    const pageTail = last(page.data);
    if (!pageTail) return { ...window, next: null, hasMore: false };
    const rows = sortedUnique([...window.rows, ...page.data]);
    if (last(rows)?.id !== pageTail.id) return { ...window, rows };
    return { rows, next: page.has_more ? page.next : null, hasMore: page.has_more && !!page.next };
}

export function removeSubmissions(window: SubmissionsWindow, ids: ReadonlySet<string>): SubmissionsWindow {
    return { ...window, rows: window.rows.filter((r) => !ids.has(r.id)) };
}

/** Puts a row back where the server order has it, as when a delete is refused. */
export function restoreSubmission(window: SubmissionsWindow, row: FormSubmission): SubmissionsWindow {
    return { ...window, rows: sortedUnique([...window.rows, row]) };
}
