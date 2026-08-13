// Client-side mirror of the backend's guardrail bounds, so a bad threshold is
// rejected in the form instead of round-tripping for a 400.

import type Campaign from "@/lib/api/models/app/campaigns/Campaign";

export function guardrailValidationError(c: Campaign): string | null {
    if (!c.guardrail_enabled) return null;
    const rates: [string, number][] = [
        ["Bounce rate", c.guardrail_bounce_rate_max],
        ["Complaint rate", c.guardrail_complaint_rate_max],
        ["Reply rate", c.guardrail_reply_rate_min],
    ];
    for (const [label, value] of rates) {
        if (value < 0 || value > 100) return `${label} must be a percentage between 0 and 100.`;
    }
    if (c.guardrail_min_sample < 1 || c.guardrail_min_sample > 100000) {
        return "The sample floor must be between 1 and 100,000 sends.";
    }
    if (c.guardrail_window_days < 0 || c.guardrail_window_days > 365) {
        return "The measurement window must be between 0 and 365 days.";
    }
    if (
        c.guardrail_bounce_rate_max === 0 &&
        c.guardrail_complaint_rate_max === 0 &&
        c.guardrail_reply_rate_min === 0
    ) {
        return "Auto-pause is on but every rule is set to 0. Set at least one threshold, or switch auto-pause off.";
    }
    return null;
}
