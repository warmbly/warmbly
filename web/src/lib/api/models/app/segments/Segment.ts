export type SegmentMatch = "all" | "any";

export type SegmentMemberMode = "include" | "exclude" | "auto";

export type SegmentFieldKind =
    | "text"
    | "enum"
    | "bool"
    | "date"
    | "number"
    | "category"
    | "campaign"
    | "segment";

export interface SegmentCondition {
    field: string;
    operator: string;
    value?: string;
    values?: string[];
}

export interface SegmentFieldSpec {
    field: string;
    label: string;
    group: string;
    kind: SegmentFieldKind;
    options?: string[];
    // Display names for an enum's values, where the value alone is not readable.
    option_labels?: Record<string, string>;
}

export default interface Segment {
    id: string;
    organization_id: string;
    created_by?: string;
    name: string;
    description: string;
    color: string;
    match: SegmentMatch;
    conditions: SegmentCondition[];
    contact_count: number;
    included_count: number;
    excluded_count: number;
    created_at: Date;
    updated_at: Date;
}

export interface SegmentWrite {
    name?: string;
    description?: string;
    color?: string;
    match?: SegmentMatch;
    conditions?: SegmentCondition[];
}

export interface SegmentPreview {
    id?: string;
    match: SegmentMatch;
    conditions: SegmentCondition[];
}

// One segment as seen from a contact: member or not, plus any manual override.
export interface ContactSegment {
    id: string;
    name: string;
    color: string;
    mode?: "include" | "exclude";
    member: boolean;
}

// A contact pinned into or out of a segment.
export interface SegmentOverride {
    contact_id: string;
    first_name: string;
    last_name: string;
    email: string;
    company: string;
    mode: "include" | "exclude";
    created_at: Date;
}

export interface SegmentAddToCampaignResult {
    campaign_id: string;
    added: number;
    members: number;
}

// One segment linked to a campaign as a live audience source. The counts are
// live: members now, members that are leads of this campaign, and members
// held out because a lead was removed from the campaign by hand.
export interface CampaignSegmentLink {
    segment_id: string;
    name: string;
    color: string;
    description: string;
    contact_count: number;
    lead_count: number;
    held_out_count: number;
    linked_at: string;
}

// Operators per field kind, mirrored from the backend catalog.
export const SEGMENT_OPERATORS: Record<SegmentFieldKind, { id: string; label: string }[]> = {
    text: [
        { id: "equals", label: "is" },
        { id: "not_equals", label: "is not" },
        { id: "contains", label: "contains" },
        { id: "not_contains", label: "does not contain" },
        { id: "starts_with", label: "starts with" },
        { id: "ends_with", label: "ends with" },
        { id: "is_empty", label: "is empty" },
        { id: "is_not_empty", label: "is not empty" },
    ],
    enum: [
        { id: "in", label: "is any of" },
        { id: "not_in", label: "is none of" },
    ],
    bool: [
        { id: "is_true", label: "is yes" },
        { id: "is_false", label: "is no" },
    ],
    date: [
        { id: "within_days", label: "in the last" },
        { id: "not_within_days", label: "not in the last" },
        { id: "after", label: "is after" },
        { id: "before", label: "is before" },
        { id: "is_empty", label: "never" },
        { id: "is_not_empty", label: "ever" },
    ],
    number: [
        { id: "equals", label: "is" },
        { id: "not_equals", label: "is not" },
        { id: "gt", label: "is more than" },
        { id: "gte", label: "is at least" },
        { id: "lt", label: "is less than" },
        { id: "lte", label: "is at most" },
    ],
    category: [
        { id: "in", label: "has any of" },
        { id: "not_in", label: "has none of" },
        { id: "is_empty", label: "has none" },
        { id: "is_not_empty", label: "has any" },
    ],
    campaign: [
        { id: "in", label: "is in any of" },
        { id: "not_in", label: "is in none of" },
        { id: "is_empty", label: "is in no campaign" },
        { id: "is_not_empty", label: "is in a campaign" },
    ],
    segment: [
        { id: "in", label: "is in any of" },
        { id: "not_in", label: "is in none of" },
    ],
};

// Operators that take no value at all.
export const VALUELESS_OPERATORS = new Set(["is_empty", "is_not_empty", "is_true", "is_false"]);
