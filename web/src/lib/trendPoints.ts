// Points for a TrendStat sparkline, zero-filled so quiet days keep their place on the axis.

export interface TrendPoint {
    /** YYYY-MM-DD (UTC) for daily points, an ISO timestamp for finer ones. */
    date: string;
    value: number | null;
}

/** Every UTC day from `from` to `to`, inclusive, as YYYY-MM-DD. */
export function utcDayRange(from: string, to: string): string[] {
    const start = Date.parse(`${from}T00:00:00Z`);
    const end = Date.parse(`${to}T00:00:00Z`);
    if (Number.isNaN(start) || Number.isNaN(end) || end < start) return [];
    return Array.from({ length: Math.round((end - start) / 86_400_000) + 1 }, (_, i) => new Date(start + i * 86_400_000).toISOString().slice(0, 10));
}

/** One point per day of `days`; a day with no row is `missing` (0 for counts, null for rates). */
export function dailyPoints<R extends { date: string }>(days: string[], rows: R[] | undefined, value: (row: R) => number | null, missing: number | null = 0): TrendPoint[] {
    const byDate = new Map((rows ?? []).map((r) => [r.date.slice(0, 10), r]));
    return days.map((date) => {
        const row = byDate.get(date);
        return { date, value: row ? value(row) : missing };
    });
}
