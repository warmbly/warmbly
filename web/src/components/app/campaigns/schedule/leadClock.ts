import type { Interval } from "./WeekScheduleGrid";

// Clock maths for showing a campaign's windows on its leads' own clocks.
// Windows are display-indexed, Monday first, minutes since local midnight.

const WEEKDAYS: Record<string, number> = { Mon: 0, Tue: 1, Wed: 2, Thu: 3, Fri: 4, Sat: 5, Sun: 6 };

/** The weekday (Monday = 0) and minute of the day that `at` is in `zone`. */
export function zoneClock(zone: string, at: Date): { day: number; minutes: number } {
    const parts = new Intl.DateTimeFormat("en-US", {
        timeZone: zone,
        weekday: "short",
        hour: "2-digit",
        minute: "2-digit",
        hourCycle: "h23",
    }).formatToParts(at);
    const get = (t: string) => parts.find((p) => p.type === t)?.value ?? "";
    return { day: WEEKDAYS[get("weekday")] ?? 0, minutes: Number(get("hour")) * 60 + Number(get("minute")) };
}

/** Whether `at` falls inside one of the windows on `zone`'s clock. */
export function inWindow(windows: Interval[][], zone: string, at: Date): boolean {
    const { day, minutes } = zoneClock(zone, at);
    return (windows[day] ?? []).some((iv) => minutes >= iv.start && minutes < iv.end);
}

/** Minutes `zone` is ahead of UTC at `at`. */
export function zoneOffset(zone: string, at: Date): number {
    const name =
        new Intl.DateTimeFormat("en-US", { timeZone: zone, timeZoneName: "longOffset" })
            .formatToParts(at)
            .find((p) => p.type === "timeZoneName")?.value ?? "GMT";
    const m = /GMT([+-])(\d{2}):?(\d{2})?/.exec(name);
    if (!m) return 0;
    const total = Number(m[2]) * 60 + Number(m[3] ?? 0);
    return m[1] === "-" ? -total : total;
}

const hhmm = (minutes: number) => {
    const m = ((minutes % 1440) + 1440) % 1440;
    return `${String(Math.floor(m / 60)).padStart(2, "0")}:${String(m % 60).padStart(2, "0")}`;
};

/** The instant `minutes` past midnight falls on `days` after today's date in `zone`. */
function instantIn(zone: string, at: Date, days: number, minutes: number): Date {
    const parts = new Intl.DateTimeFormat("en-US", { timeZone: zone, year: "numeric", month: "numeric", day: "numeric" }).formatToParts(at);
    const get = (t: string) => Number(parts.find((p) => p.type === t)?.value);
    const wall = Date.UTC(get("year"), get("month") - 1, get("day") + days, 0, minutes);
    // Read the offset again at the first answer: on a daylight-saving day the
    // offset at the wall time differs from the one at UTC midnight.
    const first = wall - zoneOffset(zone, new Date(wall)) * 60_000;
    return new Date(wall - zoneOffset(zone, new Date(first)) * 60_000);
}

const dayAndTime = (zone: string, at: Date) =>
    `${new Intl.DateTimeFormat("en-US", { timeZone: zone, weekday: "short" }).format(at)} ${localTime(zone, at)}`;

/**
 * The campaign's next window on `from`'s clock, from today on, as `to`'s clock
 * reads it: a weekday 09:00–17:00 New York window is "Mon 23:00–Tue 07:00" in
 * Tokyo. Null when the schedule has no window at all.
 */
export function windowOnTheirClock(windows: Interval[][], from: string, to: string, at: Date): string | null {
    const next = nextWindow(windows, from, at);
    if (!next) return null;
    const start = dayAndTime(to, next.start);
    const end = dayAndTime(to, next.end);
    // "Mon 23:00–Tue 07:00", or "Mon 09:00–17:00" when it stays on one day.
    return start.slice(0, 3) === end.slice(0, 3) ? `${start}–${end.slice(4)}` : `${start}–${end}`;
}

/** When the window next opens on `zone`'s own clock, "Mon 09:00"; null when it never does. */
export function opensOnTheirClock(windows: Interval[][], zone: string, at: Date): string | null {
    const next = nextWindow(windows, zone, at);
    return next ? dayAndTime(zone, next.start) : null;
}

/** The first window on `zone`'s clock that has not ended by `at`. */
function nextWindow(windows: Interval[][], zone: string, at: Date): { start: Date; end: Date } | null {
    const { day } = zoneClock(zone, at);
    for (let d = 0; d < 8; d++) {
        for (const iv of windows[(day + d) % 7] ?? []) {
            const end = instantIn(zone, at, d, iv.end);
            if (end > at) return { start: instantIn(zone, at, d, iv.start), end };
        }
    }
    return null;
}

/** The time it is now in `zone`, "14:05". */
export function localTime(zone: string, at: Date): string {
    return hhmm(zoneClock(zone, at).minutes);
}

/** "Tokyo", "New York", "UTC+2" for a fixed offset. */
export function zoneLabel(zone: string): string {
    const fixed = /^Etc\/GMT([+-])(\d+)$/.exec(zone);
    if (fixed) return `UTC${fixed[1] === "-" ? "+" : "-"}${fixed[2]}`;
    return (zone.split("/").pop() ?? zone).replace(/_/g, " ");
}
