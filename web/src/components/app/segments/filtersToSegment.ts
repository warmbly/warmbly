// Turns the contact filter panel's state into segment conditions so a filter
// can be saved as a segment. Returns what could not be carried over.

import type SearchContacts from "@/lib/api/models/app/contacts/SearchContacts";
import type { SegmentCondition } from "@/lib/api/models/app/segments/Segment";

export interface SegmentPreset {
    conditions: SegmentCondition[];
    dropped: string[];
}

const TEXT_OPS: Record<string, string> = {
    equal: "equals",
    contains: "contains",
    starts_with: "starts_with",
    ends_with: "ends_with",
};

function isoDate(d: Date): string {
    return new Date(d).toISOString().slice(0, 10);
}

export function filtersToSegment(f: SearchContacts, campaignID?: string): SegmentPreset {
    const conditions: SegmentCondition[] = [];
    const dropped: string[] = [];

    for (const cf of f.custom_field_filters) {
        const name = cf.name.trim();
        const op = TEXT_OPS[cf.type];
        // A half-filled pill never reaches the search, so it must not reach
        // the segment either: the two would describe different audiences.
        if (!name || !cf.value.trim() || !op) continue;
        conditions.push({ field: `custom.${name}`, operator: op, value: cf.value });
    }
    if (f.category_ids && f.category_ids.length > 0) {
        // The filter panel requires every category; one condition per id keeps that.
        for (const id of f.category_ids) conditions.push({ field: "category", operator: "in", values: [id] });
    }
    if (f.segment_ids && f.segment_ids.length > 0) {
        for (const id of f.segment_ids) conditions.push({ field: "segment", operator: "in", values: [id] });
    }
    if (f.subscribed !== undefined) {
        conditions.push({ field: "subscribed", operator: f.subscribed ? "is_true" : "is_false" });
    }
    if (f.verification_status) conditions.push({ field: "verification_status", operator: "in", values: [f.verification_status] });
    // A segment enum has no empty value, so "unknown" is reported rather than lost.
    const hosts = (f.mail_hosts ?? []).filter(Boolean);
    if (hosts.length > 0) conditions.push({ field: "mail_host", operator: "in", values: hosts });
    if (f.mail_hosts?.includes("")) dropped.push("unknown email provider");
    if (f.min_campaigns !== undefined) conditions.push({ field: "campaign_count", operator: "gte", value: String(f.min_campaigns) });
    if (f.max_campaigns !== undefined) conditions.push({ field: "campaign_count", operator: "lte", value: String(f.max_campaigns) });
    if (f.created_after) conditions.push({ field: "created_at", operator: "after", value: isoDate(f.created_after) });
    if (f.created_before) conditions.push({ field: "created_at", operator: "before", value: isoDate(f.created_before) });
    if (f.updated_after) conditions.push({ field: "updated_at", operator: "after", value: isoDate(f.updated_after) });
    if (f.updated_before) conditions.push({ field: "updated_at", operator: "before", value: isoDate(f.updated_before) });
    if (campaignID) conditions.push({ field: "campaign", operator: "in", values: [campaignID] });

    // Engagement buckets become lifetime counters; the not_* forms only cover
    // contacts that were sent something, like the filter does.
    if (f.engagement) {
        const positive = f.engagement.startsWith("not_") ? f.engagement.slice(4) : f.engagement;
        const field = `emails_${positive}`;
        if (f.engagement.startsWith("not_")) {
            conditions.push({ field: "emails_sent", operator: "gte", value: "1" });
            conditions.push({ field, operator: "equals", value: "0" });
        } else {
            conditions.push({ field, operator: "gte", value: "1" });
        }
    }

    if (f.query.trim()) dropped.push("search text");
    if (f.lead_status) dropped.push("lead status");

    return { conditions, dropped };
}
