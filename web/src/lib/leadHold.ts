import type ContactCampaignState from "@/lib/api/models/app/contacts/ContactCampaignState";

// Pausable only while the scheduler reports a next step; a failed preview counts as no.
export function leadCanBePaused(state: ContactCampaignState): boolean {
    return !state.hold && !state.ended_reason && !!state.next && state.campaign_status !== "completed";
}

// "yyyy-MM-dd" for the member's local day `days` after `from`.
export function localDayISO(days: number, from: Date = new Date()): string {
    const d = new Date(from);
    d.setDate(d.getDate() + days);
    return `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, "0")}-${String(d.getDate()).padStart(2, "0")}`;
}

// End of that local day, so "pause until the 8th" still covers the 8th.
export function endOfLocalDay(iso: string): string | null {
    const [y, m, d] = iso.split("-").map(Number);
    if (!y || !m || !d) return null;
    return new Date(y, m - 1, d, 23, 59, 59, 0).toISOString();
}

// A reply's follow-up pause; `days` null has no end and only a resume lifts it.
export interface FollowUpPause {
    label: string;
    short: string;
    days: number | null;
}

export const FOLLOW_UP_PAUSES: FollowUpPause[] = [
    { label: "For 3 days", short: "3 days", days: 3 },
    { label: "For 1 week", short: "1 week", days: 7 },
    { label: "For 2 weeks", short: "2 weeks", days: 14 },
    { label: "For 30 days", short: "30 days", days: 30 },
    { label: "Until I resume them", short: "until resumed", days: null },
];

// Counted from when the reply goes out, so a scheduled reply is covered too.
export function followUpPauseUntil(p: FollowUpPause, from: Date = new Date()): string | null {
    return p.days == null ? null : endOfLocalDay(localDayISO(p.days, from));
}
