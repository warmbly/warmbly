/**
 * "14:05", "tomorrow 09:12", "Thu 10:30" or "Mar 4 08:00" for a queued slot.
 * With `timeZone` the slot is read on that clock (a mailbox's own hours) and
 * named when it is not the browser's, so "09:12 GMT" matches hours set in GMT.
 */
export function nextSlotLabel(at: Date | string, now: Date = new Date(), timeZone?: string): string {
    const d = new Date(at);
    const zone = validZone(timeZone);
    const browserZone = Intl.DateTimeFormat().resolvedOptions().timeZone;
    const foreign = !!zone && !sameClock(zone, browserZone, d);
    const time = d.toLocaleTimeString([], { hour: "2-digit", minute: "2-digit", timeZone: zone, timeZoneName: foreign ? "short" : undefined });
    const days = Math.round((localDay(d, zone) - localDay(now, zone)) / 86_400_000);
    if (days === 0) return time;
    if (days === 1) return `tomorrow ${time}`;
    if (days > 1 && days < 7) return `${d.toLocaleDateString([], { weekday: "short", timeZone: zone })} ${time}`;
    return `${d.toLocaleDateString([], { month: "short", day: "numeric", timeZone: zone })} ${time}`;
}

/** A zone the browser cannot read falls back to UTC, as the scheduler does for one it cannot load. */
function validZone(zone?: string): string | undefined {
    if (!zone) return undefined;
    try {
        new Intl.DateTimeFormat("en-US", { timeZone: zone });
        return zone;
    } catch {
        return "UTC";
    }
}

/** Midnight UTC of the calendar day `d` falls on in `zone`, for whole-day differences. */
function localDay(d: Date, zone?: string): number {
    const parts = new Intl.DateTimeFormat("en-US", { timeZone: zone, year: "numeric", month: "numeric", day: "numeric" }).formatToParts(d);
    const get = (type: string) => Number(parts.find((p) => p.type === type)?.value);
    return Date.UTC(get("year"), get("month") - 1, get("day"));
}

function sameClock(a: string, b: string, at: Date): boolean {
    const fmt = (zone: string) => at.toLocaleString("en-US", { timeZone: zone });
    return a === b || fmt(a) === fmt(b);
}
