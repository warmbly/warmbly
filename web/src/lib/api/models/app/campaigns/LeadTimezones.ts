import type { TimezoneMode } from "./Campaign";

/** One timezone and how many of the campaign's leads resolve to it. */
export interface LeadZone {
    timezone: string;
    /** ISO 3166-1 alpha-2, "" for UTC and fixed offsets. */
    country: string;
    leads: number;
}

/** GET /campaigns/:id/lead-timezones */
export default interface LeadTimezones {
    campaign_id: string;
    timezone_mode: TimezoneMode;
    /** The campaign's own clock: what a lead nothing places is sent on. */
    timezone: string;
    total: number;
    unknown: number;
    /** Leads whose record changed and is still being read. */
    resolving: number;
    zones: LeadZone[];
}
