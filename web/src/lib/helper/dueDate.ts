// Relative due-date helpers. Tasks are set as "due in N days" rather than an
// absolute calendar date; these convert between that day offset and the ISO
// timestamp the API stores.

// Convert a day offset into an ISO timestamp at 9am local, N days from today.
export function dueInDaysToISO(days: number): string {
    const d = new Date();
    d.setHours(9, 0, 0, 0);
    d.setDate(d.getDate() + days);
    return d.toISOString();
}

// Convert an existing absolute due date back into a whole-day offset from
// today (used to seed the control when editing a task). Negative = overdue.
export function isoToDueInDays(iso?: string): number | null {
    if (!iso) return null;
    const due = new Date(iso);
    if (Number.isNaN(due.getTime())) return null;
    const today = new Date();
    today.setHours(0, 0, 0, 0);
    const dueDay = new Date(due);
    dueDay.setHours(0, 0, 0, 0);
    return Math.round((dueDay.getTime() - today.getTime()) / 86_400_000);
}
