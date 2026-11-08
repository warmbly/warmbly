import { utcToday } from "@/lib/campaignPeriod";

// The Accounts trend strip's window; the route loader prefetches the same placement key.
export const ACCOUNTS_TREND_DAYS = 14;

export function accountsTrendWindow(today: string = utcToday()): { from: string; to: string } {
    const start = new Date(Date.parse(`${today}T00:00:00Z`) - (ACCOUNTS_TREND_DAYS - 1) * 86_400_000);
    return { from: start.toISOString().slice(0, 10), to: today };
}
