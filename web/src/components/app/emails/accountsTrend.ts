import { utcWindow } from "@/components/app/placement/placement";

// The Accounts trend strip's window; the route loader prefetches the same placement key.
export const ACCOUNTS_TREND_DAYS = 14;

export const accountsTrendWindow = () => utcWindow(ACCOUNTS_TREND_DAYS);
