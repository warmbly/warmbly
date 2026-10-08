import { z } from "zod";
import type { MailHost } from "@/lib/api/models/app/emails/MailboxImport";
import { MAIL_HOST_LABELS } from "@/lib/mailHost";

export const browseText = z.string();
export const browseBoolean = z.boolean();
export const browseIds = z.array(z.string());
const unique = <T>(values: T[]) => new Set(values).size === values.length;
export const campaignStatusSchema = z.enum(["all", "active", "paused", "draft", "completed"]);
export const campaignSortSchema = z.enum(["newest", "oldest", "name"]);
const isoDate = z.union([z.date(), z.iso.datetime({ offset: true }).transform((value) => new Date(value))]);
const count = z.number().finite().int().nonnegative();
export const contactFiltersSchema = z.object({
    query: browseText,
    custom_field_filters: z.array(z.object({
        name: z.string(), value: z.string(), type: z.enum(["equal", "starts_with", "ends_with", "contains"]),
    })),
    campaign_ids: browseIds,
    segment_ids: browseIds.optional(),
    subscribed: z.boolean().optional(),
    verification_status: z.enum(["valid", "risky", "invalid", "unknown"]).optional(),
    mail_hosts: z.array(z.enum(["", ...Object.keys(MAIL_HOST_LABELS)] as [MailHost, ...MailHost[]])).optional(),
    min_campaigns: count.optional(), max_campaigns: count.optional(),
    created_after: isoDate.optional(), created_before: isoDate.optional(),
    updated_after: isoDate.optional(), updated_before: isoDate.optional(),
    lead_status: z.enum(["pending", "active", "completed", "replied", "bounced", "failed", "unsubscribed", "paused", "undeliverable"]).optional(),
    engagement: z.enum(["opened", "not_opened", "clicked", "not_clicked", "replied", "not_replied", "bounced"]).optional(),
});
export type ContactBrowseFilters = z.infer<typeof contactFiltersSchema>;
export const contactTabSchema = z.enum(["overview", "activity", "deals", "notes", "details", "research"]);
export const contactExtrasSchema = z.array(z.enum(["created", "updated", "campaign_count", "lead_status", "engagement", "verification", "provider"])).refine(unique);
export const contactGeneralExtrasSchema = z.array(z.enum(["created", "updated", "campaign_count", "verification", "provider"])).refine(unique);
export const daySchema = z.iso.date();
const optionalDay = z.union([z.literal(""), daySchema]);
export const activityFiltersSchema = z.object({
    type: z.enum(["all", "emails", "replies", "deliv", "notes", "meetings", "campaigns", "lifecycle", "website"]),
    query: browseText, from: optionalDay, to: optionalDay,
}).refine(({ from, to }) => !from || !to || from <= to);
export const campaignPeriodSchema = z.union([
    z.object({ key: z.enum(["7d", "30d", "90d", "all"]) }),
    z.object({ key: z.literal("custom"), from: daySchema, to: daySchema }).refine(({ from, to }) => from <= to),
]);
export const hiddenMetricsSchema = z.array(z.enum(["sent", "opens", "clicks", "replies"])).max(3).refine(unique);
export const importViewSchema = z.enum(["columns", "preview"]);
