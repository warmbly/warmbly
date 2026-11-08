// The ramp-up a campaign uses, over the two server fields it maps to: kept free
// of React so the wizard's draft model can import it without a cycle.

export type RampUp = {
    warmup_graduation: boolean;
    ramp_enabled: boolean;
    ramp_start: number;
    ramp_increment: number;
    ramp_ceiling: number;
};

export type RampUpMode = "replies" | "schedule" | "full";

export function rampUpMode(r: Pick<RampUp, "warmup_graduation" | "ramp_enabled">): RampUpMode {
    if (r.ramp_enabled) return "schedule";
    return r.warmup_graduation ? "replies" : "full";
}

export function rampUpInvalid(r: RampUp): string | null {
    if (r.ramp_enabled && r.ramp_start > r.ramp_ceiling) return "Start must be less than or equal to the ceiling.";
    return null;
}

// The fields a save or an estimate sends: the numbers only while the schedule is on, so stale ones are never refused.
export function rampUpPayload(r: RampUp): Partial<RampUp> {
    const { warmup_graduation, ramp_enabled } = r;
    if (!ramp_enabled) return { warmup_graduation, ramp_enabled };
    return { ...r };
}

// One phrase for summaries: the review step and the campaign overview.
export function rampUpSummary(r: RampUp): string {
    switch (rampUpMode(r)) {
        case "schedule":
            return `${r.ramp_start}/day, +${r.ramp_increment} a day up to ${r.ramp_ceiling}`;
        case "full":
            return "Full volume from day one";
        default:
            return "As replies come in";
    }
}
