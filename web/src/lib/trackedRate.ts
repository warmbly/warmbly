// An open or click rate is null when none of the sends could register one,
// because tracking was off for all of them. A server that predates the
// tracked count sends none, and its rate stands as it is.
export function trackedRate(rate: number | undefined, trackedSent: number | undefined, sent: number | undefined): number | null {
    if (trackedSent === undefined) return rate ?? 0;
    if (trackedSent === 0 && (sent ?? 0) > 0) return null;
    return rate ?? 0;
}

// The unit under a tracked rate: what it is of, when only some sends were tracked.
export function trackedUnit(trackedSent: number | undefined, sent: number | undefined, full: string): string {
    if (trackedSent === undefined || sent === undefined || trackedSent >= sent) return full;
    if (trackedSent === 0) return sent > 0 ? "tracking off" : full;
    return `of ${trackedSent.toLocaleString()} tracked`;
}
